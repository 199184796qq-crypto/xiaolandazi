package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

func validateStrategyCenterInput(input *model.LiveStrategyCenterInput) error {
	seen := map[string]struct{}{}
	for i := range input.Rules {
		rule := &input.Rules[i]
		rule.Category = strings.ToLower(strings.TrimSpace(rule.Category))
		rule.Key = strings.TrimSpace(rule.Key)
		rule.Name = strings.TrimSpace(rule.Name)
		if rule.Category == "" || rule.Key == "" || rule.Name == "" {
			return fmt.Errorf("策略分类、编码和名称不能为空")
		}
		identity := rule.Category + ":" + rule.Key
		if _, ok := seen[identity]; ok {
			return fmt.Errorf("策略重复：%s", rule.Name)
		}
		seen[identity] = struct{}{}

		minimum := 5
		switch rule.Category {
		case "interrupt":
			minimum = 10
		case "resume":
			minimum = 20
		case "interaction":
			if rule.Key == "reply_chat" {
				minimum = 20
			}
		default:
			return fmt.Errorf("未知策略分类：%s", rule.Category)
		}
		rule.MinProbability = minimum
		if rule.BaseProbability < 0 || rule.BaseProbability > 100 {
			return fmt.Errorf("%s 的概率必须在0到100之间", rule.Name)
		}
		if !rule.Enabled {
			rule.BaseProbability = 0
		} else if rule.BaseProbability < minimum {
			rule.BaseProbability = minimum
		}
	}

	// Interrupt and resume are mutually exclusive pools. The user only supplies
	// preferred weights; effective percentages are normalized automatically.
	normalizeStrategyPool(input.Rules, "interrupt")
	normalizeStrategyPool(input.Rules, "resume")

	mode := strings.ToLower(strings.TrimSpace(input.AddressingMode))
	if mode != "system" && mode != "custom" {
		return fmt.Errorf("称呼策略必须选择系统或自定义")
	}
	input.AddressingMode = mode
	return nil
}

func normalizeStrategyPool(rules []model.LiveStrategyRule, category string) {
	indexes := make([]int, 0)
	firstCategoryIndex := -1
	for i := range rules {
		if rules[i].Category == category && firstCategoryIndex < 0 {
			firstCategoryIndex = i
		}
		if rules[i].Category == category && rules[i].Enabled {
			indexes = append(indexes, i)
		}
	}
	if len(indexes) == 0 {
		if firstCategoryIndex < 0 {
			return
		}
		rules[firstCategoryIndex].Enabled = true
		rules[firstCategoryIndex].BaseProbability = 100
		indexes = append(indexes, firstCategoryIndex)
	}

	// Minimum floors can make an oversized pool impossible (for example six
	// resume strategies at a 20% floor). Keep the strongest choices and quietly
	// disable the weakest extras instead of rejecting the save.
	for {
		minimumTotal := 0
		for _, idx := range indexes {
			minimumTotal += rules[idx].MinProbability
		}
		if minimumTotal <= 100 || len(indexes) <= 1 {
			break
		}
		sort.SliceStable(indexes, func(i, j int) bool {
			left, right := rules[indexes[i]], rules[indexes[j]]
			if left.BaseProbability == right.BaseProbability {
				return left.Key > right.Key
			}
			return left.BaseProbability > right.BaseProbability
		})
		drop := indexes[len(indexes)-1]
		rules[drop].Enabled = false
		rules[drop].BaseProbability = 0
		indexes = indexes[:len(indexes)-1]
	}

	minimumTotal := 0
	extraTotal := 0
	for _, idx := range indexes {
		minimumTotal += rules[idx].MinProbability
		extra := rules[idx].BaseProbability - rules[idx].MinProbability
		if extra > 0 {
			extraTotal += extra
		}
	}
	remaining := 100 - minimumTotal
	if remaining < 0 {
		remaining = 0
	}

	type allocation struct {
		idx       int
		base      int
		remainder float64
	}
	items := make([]allocation, 0, len(indexes))
	assigned := minimumTotal
	for _, idx := range indexes {
		base := rules[idx].MinProbability
		var exact float64
		if remaining > 0 {
			if extraTotal > 0 {
				extra := rules[idx].BaseProbability - rules[idx].MinProbability
				if extra < 0 {
					extra = 0
				}
				exact = float64(remaining) * float64(extra) / float64(extraTotal)
			} else {
				exact = float64(remaining) / float64(len(indexes))
			}
		}
		whole := int(exact)
		assigned += whole
		items = append(items, allocation{idx: idx, base: base + whole, remainder: exact - float64(whole)})
	}

	left := 100 - assigned
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].remainder == items[j].remainder {
			return rules[items[i].idx].Key < rules[items[j].idx].Key
		}
		return items[i].remainder > items[j].remainder
	})
	for i := 0; i < left && i < len(items); i++ {
		items[i].base++
	}
	for _, item := range items {
		rules[item.idx].BaseProbability = item.base
	}
}

func validateAddressingOptions(options []model.LiveAddressingOption, mode string) error {
	wantSystem := !strings.EqualFold(strings.TrimSpace(mode), "custom")
	count := 0
	seen := map[string]struct{}{}
	for i := range options {
		option := &options[i]
		text := strings.TrimSpace(option.Text)
		if text == "" || !option.Enabled {
			continue
		}
		if option.SystemDefault != wantSystem {
			continue
		}
		if utf8.RuneCountInString(text) > 12 {
			return fmt.Errorf("称呼“%s”过长，最多12个字", text)
		}
		if option.Probability < 0 || option.Probability > 100 {
			return fmt.Errorf("称呼“%s”的概率必须在0到100之间", text)
		}
		if _, ok := seen[text]; ok {
			return fmt.Errorf("称呼“%s”重复", text)
		}
		seen[text] = struct{}{}
		count++
	}
	if count == 0 {
		return fmt.Errorf("至少启用一个称呼")
	}
	normalizeAddressingOptions(options, mode)
	return nil
}

func normalizeAddressingOptions(options []model.LiveAddressingOption, mode string) {
	wantSystem := !strings.EqualFold(strings.TrimSpace(mode), "custom")
	indexes := make([]int, 0)
	total := 0
	for i := range options {
		if !options[i].Enabled || options[i].SystemDefault != wantSystem || strings.TrimSpace(options[i].Text) == "" {
			continue
		}
		indexes = append(indexes, i)
		if options[i].Probability > 0 {
			total += options[i].Probability
		}
	}
	if len(indexes) == 0 {
		return
	}

	type allocation struct {
		idx       int
		base      int
		remainder float64
	}
	items := make([]allocation, 0, len(indexes))
	assigned := 0
	for _, idx := range indexes {
		var exact float64
		if total > 0 {
			weight := options[idx].Probability
			if weight < 0 {
				weight = 0
			}
			exact = 100 * float64(weight) / float64(total)
		} else {
			exact = 100 / float64(len(indexes))
		}
		whole := int(exact)
		assigned += whole
		items = append(items, allocation{idx: idx, base: whole, remainder: exact - float64(whole)})
	}

	left := 100 - assigned
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].remainder == items[j].remainder {
			return options[items[i].idx].Key < options[items[j].idx].Key
		}
		return items[i].remainder > items[j].remainder
	})
	for i := 0; i < left && i < len(items); i++ {
		items[i].base++
	}
	for _, item := range items {
		options[item.idx].Probability = item.base
	}
}

func addressingSignature(mode string, options []model.LiveAddressingOption) string {
	wantSystem := !strings.EqualFold(strings.TrimSpace(mode), "custom")
	type signatureItem struct {
		Key         string `json:"key"`
		Text        string `json:"text"`
		Enabled     bool   `json:"enabled"`
		Probability int    `json:"probability"`
	}
	items := make([]signatureItem, 0, len(options))
	for _, option := range options {
		if option.SystemDefault != wantSystem {
			continue
		}
		items = append(items, signatureItem{
			Key: strings.TrimSpace(option.Key), Text: strings.TrimSpace(option.Text),
			Enabled: option.Enabled, Probability: option.Probability,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Key == items[j].Key {
			return items[i].Text < items[j].Text
		}
		return items[i].Key < items[j].Key
	})
	raw, _ := json.Marshal(struct {
		Mode  string          `json:"mode"`
		Items []signatureItem `json:"items"`
	}{Mode: strings.ToLower(strings.TrimSpace(mode)), Items: items})
	return string(raw)
}

func reviewAddressingWithModel(ctx context.Context, options []model.LiveAddressingOption, mode string) error {
	wantSystem := !strings.EqualFold(strings.TrimSpace(mode), "custom")
	texts := make([]string, 0, len(options))
	for _, option := range options {
		if option.Enabled && option.SystemDefault == wantSystem && strings.TrimSpace(option.Text) != "" {
			texts = append(texts, strings.TrimSpace(option.Text))
		}
	}
	if len(texts) == 0 {
		return fmt.Errorf("当前称呼模式至少需要一个可用称呼")
	}
	sort.Strings(texts)
	payload, _ := json.Marshal(texts)
	response, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你是直播称呼安全审核器。判断这些称呼是否适合公开直播使用。拒绝侮辱、歧视、性暗示、骚扰、冒充身份、政治煽动、明显低俗或容易造成身份冒犯的称呼。只返回JSON：{\"safe\":true|false,\"reason\":\"简短原因\"}。"},
			{Role: "user", Content: string(payload)},
		},
		MaxTokens:      120,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        12 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("称呼模型审核失败：%w", err)
	}
	var result struct {
		Safe   bool   `json:"safe"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(response.Text)), &result); err != nil {
		return fmt.Errorf("称呼模型审核结果无法解析")
	}
	if !result.Safe {
		reason := strings.TrimSpace(result.Reason)
		if reason == "" {
			reason = "称呼不适合公开直播使用"
		}
		return fmt.Errorf("称呼未通过审核：%s", reason)
	}
	return nil
}

func (s *Server) pushStrategyCenterToCore(ctx context.Context, tenantID int64, config model.LiveStrategyCenterConfig) error {
	if s.core == nil {
		return nil
	}
	resp, err := s.core.DoAny(ctx, http.MethodPut, fmt.Sprintf("/internal/v1/strategy-policies/%d", tenantID), nil, config)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		return fmt.Errorf("core strategy sync http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return nil
}

func (s *Server) systemLiveStrategyCenter(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	allowed := actor.IsPlatformAdmin()
	if !allowed && actor.IsInternalStaff() {
		if access, err := s.store.GetStaffAccess(r.Context(), actor.UserID); err == nil {
			allowed = access.IsSuperAdmin || staffHasPermission(access, "system.settings.liveops.manage")
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "当前账号没有策略中心管理权限")
		return
	}
	if r.Method == http.MethodGet {
		item, err := s.store.GetLiveStrategyCenter(r.Context(), 0)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取策略中心失败")
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	var input model.LiveStrategyCenterInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "策略中心格式错误")
		return
	}
	if err := validateStrategyCenterInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAddressingOptions(input.Addressing, input.AddressingMode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, currentErr := s.store.GetLiveStrategyCenter(r.Context(), 0)
	addressingChanged := currentErr != nil || addressingSignature(current.AddressingMode, current.Addressing) != addressingSignature(input.AddressingMode, input.Addressing)
	if addressingChanged {
		if err := reviewAddressingWithModel(r.Context(), input.Addressing, input.AddressingMode); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	item, err := s.store.UpsertLiveStrategyCenter(r.Context(), 0, actor.UserID, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存策略中心失败")
		return
	}
	if err := s.pushStrategyCenterToCore(r.Context(), 0, item); err != nil {
		writeError(w, http.StatusBadGateway, "策略已保存，但同步 Core 失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) roomLiveStrategyWeight(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	allowed := actor.IsPlatformAdmin()
	if !allowed && actor.IsInternalStaff() {
		if access, err := s.store.GetStaffAccess(r.Context(), actor.UserID); err == nil {
			allowed = access.IsSuperAdmin || staffHasPermission(access, "system.settings.liveops.manage")
		}
	}
	if !allowed {
		writeError(w, http.StatusForbidden, "当前账号没有策略权重调整权限")
		return
	}

	var input struct {
		Category string `json:"category"`
		Key      string `json:"key"`
		Value    int    `json:"value"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "策略权重格式错误")
		return
	}
	category := strings.ToLower(strings.TrimSpace(input.Category))
	key := strings.TrimSpace(input.Key)
	if key == "" || input.Value < 0 || input.Value > 100 {
		writeError(w, http.StatusBadRequest, "策略权重必须在0到100之间")
		return
	}

	targetTenantID := int64(0)
	if category == "addressing" {
		targetTenantID = tenantID
	}
	current, err := s.store.GetLiveStrategyCenter(r.Context(), targetTenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取策略中心失败")
		return
	}

	updated := false
	if category == "addressing" {
		for i := range current.Addressing {
			if strings.TrimSpace(current.Addressing[i].Key) == key {
				current.Addressing[i].Probability = input.Value
				updated = true
				break
			}
		}
	} else {
		if category != "interrupt" && category != "resume" && category != "interaction" {
			writeError(w, http.StatusBadRequest, "未知策略分类")
			return
		}
		for i := range current.Rules {
			if strings.EqualFold(strings.TrimSpace(current.Rules[i].Category), category) && strings.TrimSpace(current.Rules[i].Key) == key {
				current.Rules[i].BaseProbability = input.Value
				updated = true
				break
			}
		}
	}
	if !updated {
		writeError(w, http.StatusNotFound, "策略项不存在")
		return
	}

	strategyInput := model.LiveStrategyCenterInput{
		Rules: current.Rules, AddressingMode: current.AddressingMode, Addressing: current.Addressing,
	}
	if err := validateStrategyCenterInput(&strategyInput); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateAddressingOptions(strategyInput.Addressing, strategyInput.AddressingMode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	item, err := s.store.UpsertLiveStrategyCenter(r.Context(), targetTenantID, actor.UserID, strategyInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存策略权重失败")
		return
	}
	if err := s.pushStrategyCenterToCore(r.Context(), targetTenantID, item); err != nil {
		writeError(w, http.StatusBadGateway, "策略权重已保存，但同步 Core 失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"room_id":         roomID,
		"tenant_id":       targetTenantID,
		"category":        category,
		"key":             key,
		"requested_value": input.Value,
		"config":          item,
	})
}

func (s *Server) liveAddressingStrategy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	current, err := s.store.GetLiveStrategyCenter(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取称呼策略失败")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"addressing_mode": current.AddressingMode, "addressing": current.Addressing})
		return
	}
	var input struct {
		Mode       string                       `json:"addressing_mode"`
		Addressing []model.LiveAddressingOption `json:"addressing"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "称呼策略格式错误")
		return
	}
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode != "system" && mode != "custom" {
		writeError(w, http.StatusBadRequest, "请选择系统称呼或自定义称呼")
		return
	}
	if err := validateAddressingOptions(input.Addressing, mode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if mode == "custom" {
		if err := reviewAddressingWithModel(r.Context(), input.Addressing, mode); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	current.AddressingMode = mode
	current.Addressing = input.Addressing
	item, err := s.store.UpsertLiveStrategyCenter(r.Context(), tenantID, actor.UserID, model.LiveStrategyCenterInput{
		Rules: current.Rules, AddressingMode: current.AddressingMode, Addressing: current.Addressing,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存称呼策略失败")
		return
	}
	if err := s.pushStrategyCenterToCore(r.Context(), tenantID, item); err != nil {
		writeError(w, http.StatusBadGateway, "称呼策略已保存，但同步 Core 失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"addressing_mode": item.AddressingMode, "addressing": item.Addressing})
}
