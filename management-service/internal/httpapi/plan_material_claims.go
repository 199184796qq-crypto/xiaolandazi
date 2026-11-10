package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type materialClaimAnalyzeInput struct {
	Text string `json:"text"`
}

type materialClaimAnalyzeOutput struct {
	Items            []model.PlanMaterialClaimInput `json:"items"`
	Provider         string                         `json:"provider,omitempty"`
	Model            string                         `json:"model,omitempty"`
	EmbeddingModel   string                         `json:"embedding_model,omitempty"`
	EmbeddingEnabled bool                           `json:"embedding_enabled"`
	ModeHint         string                         `json:"mode_hint"`
	Warnings         []string                       `json:"parse_warnings"`
	Conflicts        []any                          `json:"conflicts"`
	Gaps             []string                       `json:"gaps"`
}

func (s *Server) planMaterialClaims(w http.ResponseWriter, r *http.Request) {
	actor, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	_ = actor
	items, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资料语义库失败")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败")
		return
	}
	type claimView struct {
		model.PlanMaterialClaim
		Revision string `json:"revision"`
	}
	libraryRevision := db.MaterialLibraryRevision(items)
	views := make([]claimView, len(items))
	for i := range items {
		revision := db.MaterialClaimRevision(items[i])
		input := db.MaterialClaimInput(items[i])
		resolveMaterialTargets(&input, links)
		items[i].MaterialClaimDetails = input.MaterialClaimDetails
		items[i].ScopeKind = input.ScopeKind
		items[i].StoreName = input.StoreName
		items[i].ProductLinkID = input.ProductLinkID
		views[i] = claimView{items[i], revision}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "library_revision": libraryRevision})
}

func (s *Server) planMaterialClaimAnalyze(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input materialClaimAnalyzeInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.Text == "" {
		writeError(w, http.StatusBadRequest, "请先粘贴资料")
		return
	}
	if len([]rune(input.Text)) > 20000 {
		writeError(w, http.StatusBadRequest, "一次最多 20000 字")
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审核模型配置失败")
		return
	}
	reviewID := settings.Cycle.TextProfileID
	if reviewID == "" {
		reviewID = settings.SafetyProfileID
	}
	if reviewID == "" {
		reviewID = settings.SellingPointProfileID
	}
	if reviewID == "" {
		writeError(w, http.StatusBadRequest, "请先在后台配置审核模型")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案链接失败，未开始分析")
		return
	}
	catalog, err := s.store.PlanMaterialObjects(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取门店目录失败，未开始分析")
		return
	}
	contextRaw, _ := json.Marshal(map[string]any{"links": links, "stores": catalog.Stores})
	objectContext := "【当前方案已建立的对象目录】\n" + string(contextRaw) + "\n【待分析资料】\n"
	chunks := splitMaterialSections(input.Text, 600)
	type chunkOutput struct {
		mode            string
		conflicts       []any
		gaps            []string
		index           int
		items           []model.PlanMaterialClaimInput
		provider, model string
		err             error
	}
	outputs := make(chan chunkOutput, len(chunks))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for index, chunk := range chunks {
		index, chunk := index, chunk
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			result, _, err := s.completeAnchorStyleWorkbench(r.Context(), reviewID, "material_import_analyze", agentgateway.ResponseJSON, []agentgateway.Message{
				{Role: "system", Content: materialClaimAnalyzePrompt},
				{Role: "user", Content: objectContext + chunk},
			}, 12000, false)
			if err != nil {
				outputs <- chunkOutput{index: index, err: err}
				return
			}
			if strings.EqualFold(result.FinishReason, "length") {
				outputs <- chunkOutput{index: index, err: errors.New("模型结果被截断，请分批分析")}
				return
			}
			items, err := parseMaterialClaimItems(result.Text)
			part := chunkOutput{index: index, items: items, provider: result.Provider, model: result.Model, err: err}
			if root, e := parseLooseJSONValue(result.Text); e == nil {
				if data, ok := root.(map[string]any); ok {
					part.mode = materialString(data["mode_hint"])
					part.conflicts, _ = data["conflicts"].([]any)
					if gaps, ok := data["gaps"].([]any); ok {
						for _, g := range gaps {
							part.gaps = append(part.gaps, materialString(g))
						}
					}
				}
			}
			outputs <- part
		}()
	}
	wg.Wait()
	close(outputs)
	all := make([]model.PlanMaterialClaimInput, 0, len(chunks)*8)
	var lastProvider, lastModel string
	var lastErr error
	warnings := []string{}
	conflicts := []any{}
	gaps := []string{}
	modeHint := "unknown"
	ordered := make([]chunkOutput, len(chunks))
	for output := range outputs {
		ordered[output.index] = output
	}
	for _, output := range ordered {
		if output.err != nil {
			lastErr = output.err
			warnings = append(warnings, "部分资料分析失败，未替换有效草稿；请检查原文覆盖")
			continue
		}
		lastProvider, lastModel = output.provider, output.model
		conflicts = append(conflicts, output.conflicts...)
		gaps = append(gaps, output.gaps...)
		if output.mode == "overwrite" {
			modeHint = "overwrite"
		}
		for i := range output.items {
			item := normalizeMaterialClaim(output.items[i], tenantID, planID)
			resolveMaterialTargets(&item, links)
			annotateMaterialClaim(&item, input.Text)
			all = append(all, item)
		}
	}
	if len(all) == 0 {
		if lastErr != nil {
			writeError(w, http.StatusBadGateway, "资料分析失败："+lastErr.Error())
		} else {
			writeError(w, http.StatusBadGateway, "分析结果不是合法数据格式，请重试或换一段资料")
		}
		return
	}
	analyzed := dedupeMaterialClaims(all)
	resultProvider, resultModel := lastProvider, lastModel
	if missing := missingMaterialLines(input.Text, analyzed); len(missing) > 0 {
		if result, _, err := s.completeAnchorStyleWorkbench(r.Context(), reviewID, "material_import_analyze", agentgateway.ResponseJSON, []agentgateway.Message{
			{Role: "system", Content: materialClaimAnalyzePrompt},
			{Role: "user", Content: objectContext + "【补抽以下原文行，不要重复已有事实】\n" + strings.Join(missing, "\n")},
		}, 6000, false); err == nil {
			if extra, err := parseMaterialClaimItems(result.Text); err == nil {
				for i := range extra {
					item := normalizeMaterialClaim(extra[i], tenantID, planID)
					resolveMaterialTargets(&item, links)
					annotateMaterialClaim(&item, input.Text)
					all = append(all, item)
				}
				analyzed = dedupeMaterialClaims(all)
				resultProvider, resultModel = result.Provider, result.Model
			}
		}
	}
	for _, line := range missingMaterialLines(input.Text, analyzed) {
		warnings = append(warnings, "原文待核对："+line)
	}
	embeddingModel := ""
	embeddingEnabled := false
	if e, ok := s.semanticMetrics.(interface {
		Enabled() bool
		Model() string
	}); ok {
		embeddingModel = e.Model()
		embeddingEnabled = e.Enabled()
	}
	writeJSON(w, http.StatusOK, materialClaimAnalyzeOutput{
		Items: analyzed, Provider: resultProvider, Model: resultModel,
		EmbeddingModel: embeddingModel, EmbeddingEnabled: embeddingEnabled, ModeHint: modeHint, Warnings: warnings, Conflicts: conflicts, Gaps: gaps,
	})
}

func (s *Server) planMaterialClaimConfirm(w http.ResponseWriter, r *http.Request) {
	actor, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Items []model.PlanMaterialClaimInput `json:"items"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if len(input.Items) > 500 {
		writeError(w, http.StatusBadRequest, "一次最多确认500条")
		return
	}
	if len(input.Items) == 0 {
		writeError(w, http.StatusBadRequest, "没有可入库的资料")
		return
	}
	embedder, ok := s.semanticMetrics.(interface {
		Enabled() bool
		Model() string
		EmbedTexts(context.Context, []string) ([][]float32, error)
	})
	if !ok || !embedder.Enabled() {
		writeError(w, http.StatusBadRequest, "服务端向量模型未启用")
		return
	}
	existing, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取语义库失败，未开始向量化")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败，未开始向量化")
		return
	}
	accepted := materialClientIDs(input.Items)
	candidates := []model.PlanMaterialClaimInput{}
	for _, candidate := range input.Items {
		candidate = normalizeMaterialClaim(candidate, tenantID, planID)
		resolveMaterialTargets(&candidate, links)
		if err := validateMaterialClaim(candidate); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		duplicate := false
		for _, old := range existing {
			if db.MaterialClaimSame(candidate, db.MaterialClaimInput(old)) {
				duplicate = true
				break
			}
		}
		for _, old := range candidates {
			if db.MaterialClaimSame(candidate, old) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		if candidate.ReplacesID > 0 {
			found := false
			for _, old := range existing {
				if old.ID == candidate.ReplacesID {
					found = true
				}
			}
			if !found {
				writeError(w, 409, "待替换资料已变化，请刷新后确认")
				return
			}
		}
		candidates = append(candidates, candidate)
	}
	input.Items = candidates
	if len(candidates) == 0 {
		writeJSON(w, 200, map[string]any{"ok": true, "added": 0, "accepted_client_ids": accepted, "embedding_model": embedder.Model()})
		return
	}
	texts := make([]string, 0, len(input.Items))
	for i := range input.Items {
		input.Items[i] = normalizeMaterialClaim(input.Items[i], tenantID, planID)
		if input.Items[i].Content == "" {
			writeError(w, http.StatusBadRequest, "存在空事实")
			return
		}
		if err := validateMaterialClaim(input.Items[i]); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		texts = append(texts, materialDocumentText(input.Items[i]))
	}
	vectors, err := embedMaterialTexts(r.Context(), embedder, texts)
	if err != nil {
		writeError(w, http.StatusBadGateway, "向量化失败，整批未入库，请重试")
		return
	}
	if err := db.ValidateMaterialVectors(vectors, len(input.Items)); err != nil {
		writeError(w, http.StatusBadGateway, "向量响应不完整，未入库")
		return
	}
	added, err := s.store.ReplacePlanMaterialClaims(r.Context(), tenantID, planID, actor.UserID, input.Items, embedder.Model(), vectors)
	if err != nil {
		if errors.Is(err, db.ErrMaterialClaimConflict) {
			writeError(w, 409, "资料已变化，请刷新后重新确认")
		} else {
			writeError(w, 500, "资料入库失败，整批未采用")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "accepted_client_ids": accepted, "embedding_model": embedder.Model()})
}

func (s *Server) resolvePlanMaterialClaimScope(w http.ResponseWriter, r *http.Request) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return model.Actor{}, 0, 0, false
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return model.Actor{}, 0, 0, false
	}
	return actor, planID, tenantID, true
}

func normalizeMaterialClaim(item model.PlanMaterialClaimInput, tenantID, planID int64) model.PlanMaterialClaimInput {
	item.Content = strings.TrimSpace(item.Content)
	item.Value = strings.TrimSpace(item.Value)
	if item.Value == "" {
		item.Value = item.Content
	}
	item.Topic = strings.TrimSpace(item.Topic)
	item.Attribute = strings.TrimSpace(item.Attribute)
	item.ClaimKey = strings.TrimSpace(item.ClaimKey)
	if item.ClaimKey == "" {
		item.ClaimKey = "claim"
	}
	item.Polarity = strings.TrimSpace(item.Polarity)
	if item.Polarity != "negative" {
		item.Polarity = "positive"
	}
	item.FactType = strings.TrimSpace(item.FactType)
	if item.FactType == "" {
		if item.Polarity == "negative" {
			item.FactType = "negative"
		} else {
			item.FactType = "positive"
		}
	}
	item.ScopeKind = strings.TrimSpace(item.ScopeKind)
	if item.ScopeKind == "product" && item.ProductLinkID <= 0 {
		item.ScopeKind = "unresolved"
	}
	if item.ScopeKind != "product" && item.ScopeKind != "store" && item.ScopeKind != "unresolved" && item.ScopeKind != "voucher" && item.ScopeKind != "variant" && item.ScopeKind != "service" && item.ScopeKind != "item" && item.ScopeKind != "item_group" {
		item.ScopeKind = "plan"
		item.ProductLinkID = 0
	}
	if item.ScopeKind != "store" {
		item.StoreName = ""
	}
	if item.ScopeKind == "store" {
		item.ProductLinkID = 0
	}
	if len(item.Conditions) > 0 {
		var conditions []map[string]any
		if json.Unmarshal(item.Conditions, &conditions) == nil {
			kept := make([]map[string]any, 0, len(conditions))
			for _, condition := range conditions {
				field := strings.ToLower(strings.TrimSpace(materialString(condition["field"])))
				raw := materialString(condition["raw"])
				values := materialString(condition["values"])
				if field == "other" || field == "link" || field == "product" || field == "product_link" {
					if match := regexp.MustCompile(`^第?(\d+)\s*号(?:链接)?$`).FindStringSubmatch(strings.TrimSpace(raw + values)); match != nil {
						item.ScopeKind = "product"
						item.ProductLinkID = materialInt64(match[1])
						continue
					}
				}
				kept = append(kept, condition)
			}
			if raw, err := json.Marshal(kept); err == nil {
				item.Conditions = raw
			}
		}
	}
	if len(item.Conditions) == 0 {
		item.Conditions = json.RawMessage("[]")
	}
	normalizeMaterialDetails(&item)
	return item
}

func materialString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(v)
		return string(raw)
	}
}

func materialInt64(value any) int64 {
	switch v := value.(type) {
	case json.Number:
		n, _ := v.Int64()
		return n
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func materialConditions(value any) json.RawMessage {
	if value == nil {
		return json.RawMessage("[]")
	}
	if text, ok := value.(string); ok {
		text = strings.TrimSpace(text)
		if text == "" {
			return json.RawMessage("[]")
		}
		if json.Valid([]byte(text)) {
			return json.RawMessage(text)
		}
		raw, _ := json.Marshal([]any{map[string]any{"op": "raw", "raw": text, "status": "unresolved"}})
		return raw
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage("[]")
	}
	return raw
}

// parseMaterialClaimItems accepts the review model's usual object wrapper and
// tolerates a bare array, numeric values and stringified conditions. The
// analysis endpoint must not fail just because a small field is not typed
// exactly as the Go struct expects.
func parseMaterialClaimItems(raw string) ([]model.PlanMaterialClaimInput, error) {
	root, err := parseLooseJSONValue(raw)
	if err != nil {
		return nil, err
	}
	var rows []any
	switch value := root.(type) {
	case map[string]any:
		if items, ok := value["items"].([]any); ok {
			rows = items
		} else if claims, ok := value["claims"].([]any); ok {
			rows = claims
		}
	case []any:
		rows = value
	}
	if rows == nil {
		return nil, errors.New("missing items")
	}
	out := make([]model.PlanMaterialClaimInput, 0, len(rows))
	for _, row := range rows {
		item, ok := row.(map[string]any)
		if !ok {
			continue
		}
		content := strings.TrimSpace(materialString(firstMaterialValue(item, "content", "fact", "text")))
		value := strings.TrimSpace(materialString(item["value"]))
		if content == "" {
			content = materialString(firstMaterialValue(item, "answer", "question"))
		}
		if content == "" {
			content = value
		}
		if content == "" {
			continue
		}
		if value == "" {
			value = content
		}
		details := model.MaterialClaimDetails{}
		detailRaw, _ := json.Marshal(item)
		_ = json.Unmarshal(detailRaw, &details)
		scopeKind := strings.TrimSpace(materialString(firstMaterialValue(item, "scope_kind")))
		storeName := strings.TrimSpace(materialString(firstMaterialValue(item, "store_name", "store")))
		linkID := materialInt64(firstMaterialValue(item, "product_link_id", "link_id"))
		if scope, ok := item["scope"].(map[string]any); ok {
			scopeKind = materialString(scope["type"])
			if scopeKind == "offer" {
				scopeKind = "product"
			}
			linkID = materialInt64(scope["link_no"])
			storeName = materialString(scope["store"])
			details.ScopeName = strings.TrimSpace(materialString(firstMaterialValue(scope, "variant", "name", "raw")))
		}
		if details.ObjectTargets == nil && scopeKind == "" {
			scopeKind = "unresolved"
		}
		parsed := model.PlanMaterialClaimInput{
			MaterialClaimDetails: details,
			ClaimKey:             strings.TrimSpace(materialString(firstMaterialValue(item, "claim_key", "key", "fact_key"))),
			Topic:                strings.TrimSpace(materialString(firstMaterialValue(item, "topic", "category"))),
			Attribute:            strings.TrimSpace(materialString(item["attribute"])),
			Value:                value,
			Content:              content,
			Polarity:             strings.TrimSpace(materialString(item["polarity"])),
			FactType:             strings.TrimSpace(materialString(item["fact_type"])),
			Conditions:           materialConditions(item["conditions"]),
			SourceQuote:          strings.TrimSpace(materialString(firstMaterialValue(item, "source_quote", "quote"))),
			SourceSentence:       strings.TrimSpace(materialString(firstMaterialValue(item, "source_sentence", "sentence"))),
			ScopeKind:            scopeKind,
			StoreName:            storeName,
			ProductLinkID:        linkID,
		}
		out = append(out, expandMaterialClaim(parsed)...)
	}
	if len(out) == 0 {
		return nil, errors.New("no usable claims")
	}
	return out, nil
}

func splitMaterialSections(text string, maxChars int) []string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	sectionRe := regexp.MustCompile(`^\s*[\[【]([^\]】]+)[\]】]\s*(.*)$`)
	type section struct{ label, body string }
	sections := []section{}
	current := section{}
	flushSection := func() {
		if strings.TrimSpace(current.body) != "" {
			sections = append(sections, current)
		}
		current = section{}
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if match := sectionRe.FindStringSubmatch(line); match != nil {
			flushSection()
			if strings.HasPrefix(strings.TrimSpace(match[1]), "/") {
				continue
			}
			current.label = strings.TrimSpace(match[1])
			if strings.TrimSpace(match[2]) != "" {
				current.body = strings.TrimSpace(match[2]) + "\n"
			}
			continue
		}
		current.body += line + "\n"
	}
	flushSection()
	chunks := []string{}
	for _, sec := range sections {
		limit := maxChars
		if sec.label != "" {
			limit = 4000
		}
		bodyLines := strings.Split(strings.TrimSpace(sec.body), "\n")
		cur := []string{}
		length := 0
		label := ""
		if sec.label != "" {
			label = "【" + sec.label + "】\n"
		}
		flush := func() {
			if len(cur) == 0 {
				return
			}
			prefix := ""
			if len(chunks) == 0 || label != "" {
				prefix = label
			}
			chunks = append(chunks, prefix+strings.Join(cur, "\n"))
			cur = []string{}
			length = 0
		}
		for _, line := range bodyLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			startsObject := regexp.MustCompile(`^(?:第?\d+\s*号链接|[一二三四五六七八九十]+号链接)`).MatchString(line)
			if len(cur) > 0 && (length+len([]rune(line))+1 > limit || (startsObject && length > 120)) {
				flush()
			}
			cur = append(cur, line)
			length += len([]rune(line)) + 1
		}
		flush()
	}

	return chunks
}

func missingMaterialLines(text string, items []model.PlanMaterialClaimInput) []string {
	norm := func(s string) string {
		return strings.Join(strings.Fields(s), "")
	}
	missing := []string{}
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if len([]rune(line)) < 4 || strings.HasPrefix(line, "【") && strings.HasSuffix(line, "】") || regexp.MustCompile(`^第?\d+\s*号链接[:：]?$`).MatchString(line) {
			continue
		}
		needle := norm(line)
		covered := false
		for _, item := range items {
			hay := norm(item.SourceQuote + item.Content)
			if hay == "" {
				continue
			}
			if strings.Contains(hay, needle) || strings.Contains(needle, hay) {
				covered = true
				break
			}
		}
		if !covered {
			missing = append(missing, line)
		}
		if len(missing) >= 20 {
			break
		}
	}
	return missing
}

func dedupeMaterialClaims(items []model.PlanMaterialClaimInput) []model.PlanMaterialClaimInput {
	seen := map[string]bool{}
	out := make([]model.PlanMaterialClaimInput, 0, len(items))
	for _, item := range items {
		value := strings.Join(strings.Fields(item.Value), "")
		key := item.ScopeKind + ":" + strconv.FormatInt(item.ProductLinkID, 10) + ":" + item.StoreName + ":" + item.ScopeName + ":" + fmt.Sprint(item.ObjectTargets) + ":" + item.Destination + ":" + item.ClaimKey + ":" + item.Polarity + ":" + item.Question + ":" + value + ":" + string(item.Conditions) + ":" + string(item.Exceptions)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func parseLooseJSONValue(raw string) (any, error) {
	base := stripPolicyJSONFence(raw)
	balanced := extractBalancedJSON(base)
	candidates := []string{balanced, repairJSONText(balanced), repairJSONText(base)}
	var lastErr error
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(candidate))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err == nil {
			return value, nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("empty JSON")
	}
	return nil, lastErr
}

func extractBalancedJSON(raw string) string {
	start := -1
	for i, ch := range raw {
		if ch == '{' || ch == '[' {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	stack := []rune{}
	inString := false
	escaped := false
	for i, ch := range raw[start:] {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) > 0 {
				open := stack[len(stack)-1]
				if (open == '{' && ch == '}') || (open == '[' && ch == ']') {
					stack = stack[:len(stack)-1]
				}
			}
			if len(stack) == 0 {
				return raw[start : start+i+1]
			}
		}
	}
	return raw[start:]
}

func stripJSONComments(raw string) string {
	var out strings.Builder
	inString := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
			out.WriteByte(ch)
			continue
		}
		if ch == '/' && i+1 < len(raw) {
			if raw[i+1] == '/' {
				for i < len(raw) && raw[i] != '\n' {
					i++
				}
				continue
			}
			if raw[i+1] == '*' {
				i += 2
				for i+1 < len(raw) && !(raw[i] == '*' && raw[i+1] == '/') {
					i++
				}
				i++
				continue
			}
		}
		out.WriteByte(ch)
	}
	return out.String()
}

func convertSingleQuotedJSON(raw string) string {
	var out strings.Builder
	inDouble := false
	escaped := false
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inDouble {
			out.WriteByte(ch)
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inDouble = false
			}
			continue
		}
		if ch == '"' {
			inDouble = true
			out.WriteByte(ch)
			continue
		}
		if ch != '\'' {
			out.WriteByte(ch)
			continue
		}
		out.WriteByte('"')
		i++
		for i < len(raw) {
			c := raw[i]
			if c == '\\' && i+1 < len(raw) {
				out.WriteByte(c)
				out.WriteByte(raw[i+1])
				i += 2
				continue
			}
			if c == '\'' {
				out.WriteByte('"')
				break
			}
			if c == '"' {
				out.WriteString(`\"`)
			} else {
				out.WriteByte(c)
			}
			i++
		}
	}
	return out.String()
}

func closeTruncatedJSON(raw string) string {
	stack := []rune{}
	inString := false
	escaped := false
	for _, ch := range raw {
		if inString {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	var out strings.Builder
	out.WriteString(raw)
	if inString {
		out.WriteByte('"')
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			out.WriteByte('}')
		} else {
			out.WriteByte(']')
		}
	}
	return out.String()
}

func repairJSONText(raw string) string {
	text := stripJSONComments(extractBalancedJSON(raw))
	text = strings.ReplaceAll(text, "True", "true")
	text = strings.ReplaceAll(text, "False", "false")
	text = strings.ReplaceAll(text, "None", "null")
	text = convertSingleQuotedJSON(text)
	text = regexp.MustCompile(`([{,]\s*)([A-Za-z_][A-Za-z0-9_-]*)(\s*:)`).ReplaceAllString(text, `$1"$2"$3`)
	text = quoteUnquotedJSONValues(text)
	text = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(text, `$1`)
	return closeTruncatedJSON(text)
}

func quoteUnquotedJSONValues(text string) string {
	re := regexp.MustCompile(`(:\s*)([A-Za-z_][A-Za-z0-9_.-]*)(\s*[,}\]])`)
	return re.ReplaceAllStringFunc(text, func(match string) string {
		parts := re.FindStringSubmatch(match)
		if parts == nil {
			return match
		}
		value := parts[2]
		if value == "true" || value == "false" || value == "null" {
			return match
		}
		return parts[1] + `"` + value + `"` + parts[3]
	})
}

func firstMaterialValue(item map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := item[key]; ok && value != nil {
			return value
		}
	}
	return nil
}

func (s *Server) planMaterialClaimUpdate(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("claimID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "资料 ID 不正确")
		return
	}
	var input model.PlanMaterialClaimInput
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	input = normalizeMaterialClaim(input, tenantID, planID)
	links, linkErr := s.materialLinkObjects(r, tenantID, planID)
	if linkErr != nil {
		writeError(w, 500, "读取方案对象失败")
		return
	}
	resolveMaterialTargets(&input, links)
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "事实内容不能为空")
		return
	}
	embedder, ok := s.semanticMetrics.(interface {
		Enabled() bool
		Model() string
		EmbedTexts(context.Context, []string) ([][]float32, error)
	})
	if !ok || !embedder.Enabled() {
		writeError(w, http.StatusBadRequest, "服务端向量模型未启用")
		return
	}
	if err := validateMaterialClaim(input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取资料失败")
		return
	}
	found := false
	for _, old := range current {
		if old.ID == id {
			found = true
		}
	}
	if !found {
		writeError(w, 404, "资料不存在")
		return
	}
	vectors, err := embedMaterialTexts(r.Context(), embedder, []string{materialDocumentText(input)})
	if err != nil || db.ValidateMaterialVectors(vectors, 1) != nil {
		writeError(w, http.StatusBadGateway, "向量化失败")
		return
	}
	raw, _ := json.Marshal(vectors[0])
	if err := s.store.UpdatePlanMaterialClaim(r.Context(), tenantID, planID, id, input, embedder.Model(), raw); err != nil {
		if errors.Is(err, db.ErrMaterialClaimNotFound) {
			writeError(w, 404, "资料不存在或已更新")
		} else {
			writeError(w, 500, "资料更新失败，原资料保持")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "embedding_model": embedder.Model()})
}

func (s *Server) planMaterialClaimDelete(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("claimID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "资料 ID 不正确")
		return
	}
	if err := s.store.DeletePlanMaterialClaim(r.Context(), tenantID, planID, id); err != nil {
		if errors.Is(err, db.ErrMaterialClaimNotFound) {
			writeError(w, 404, "资料不存在")
		} else {
			writeError(w, 500, "资料移除失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) planMaterialClaimScope(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("claimID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "资料 ID 不正确")
		return
	}
	var input struct {
		ScopeKind     string `json:"scope_kind"`
		ProductLinkID int64  `json:"product_link_id"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	current, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取资料失败")
		return
	}
	for _, old := range current {
		if old.ID == id {
			next := db.MaterialClaimInput(old)
			next.ScopeKind = input.ScopeKind
			next.ObjectTargets = nil
			next.StoreName = ""
			next.ScopeName = ""
			next.ProductLinkID = input.ProductLinkID
			raw, _ := json.Marshal(next)
			r.Body = http.NoBody
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
			s.planMaterialClaimUpdate(w, r)
			return
		}
	}
	writeError(w, 404, "资料不存在")
	return
}
