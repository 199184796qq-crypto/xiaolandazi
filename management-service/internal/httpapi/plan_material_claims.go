package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

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
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	list := s.store.ListPlanMaterialClaims
	if r.URL.Query().Get("order") == "display" {
		list = s.store.ListPlanMaterialClaimsForDisplay
	}
	items, err := list(r.Context(), tenantID, planID)
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

func (s *Server) planMaterialClaimOrder(w http.ResponseWriter, r *http.Request) {
	_, planID, tenantID, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var change db.MaterialFactOrderChange
	if err := readJSON(w, r, &change); err != nil {
		writeError(w, http.StatusBadRequest, "事实排序数据不完整，请重新读取")
		return
	}
	if change.ObjectKey == "" || change.ObjectKey == "all" || change.ObjectKey == "unresolved" {
		writeError(w, http.StatusBadRequest, "请选择一个对象后调整排序")
		return
	}
	links, err := s.materialProductLinks(r, tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案对象失败")
		return
	}
	catalog, err := s.store.ReorderPlanMaterialFacts(r.Context(), tenantID, planID, change,
		func(claims []model.PlanMaterialClaim, directory model.MaterialObjectDirectory) []model.PlanMaterialClaim {
			return materialFactsForObject(claims, change.ObjectKey, materialAnalysisLinks(slices.Clone(links), directory))
		},
	)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
	case errors.Is(err, db.ErrMaterialClaimConflict):
		writeError(w, http.StatusConflict, "事实或排序已更新，请重新读取后调整")
	case errors.Is(err, db.ErrMaterialFactOrderInvalid):
		writeError(w, http.StatusBadRequest, "排序只能包含当前对象的全部原子事实，每条一次")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "保存事实顺序失败，请重试")
	default:
		writeMaterialObjects(w, links, catalog)
	}
}

func materialFactsForObject(claims []model.PlanMaterialClaim, key string, links []materialLinkObject) []model.PlanMaterialClaim {
	var matches []model.PlanMaterialClaim
	for _, claim := range claims {
		input := db.MaterialClaimInput(claim)
		resolveMaterialTargets(&input, links)
		for _, target := range input.ObjectTargets {
			if key == materialFactObjectKey(target) {
				matches = append(matches, claim)
				break
			}
		}
	}
	return matches
}

func materialFactObjectKey(target model.MaterialObjectTarget) string {
	switch target.Kind {
	case "plan":
		return "plan"
	case "product":
		return "product:" + strconv.FormatInt(target.LinkID, 10)
	case "object":
		return "object:" + strconv.FormatInt(target.ObjectID, 10)
	case "store":
		return "store:" + target.StoreName
	default:
		return ""
	}
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
	if strings.TrimSpace(input.Text) == "" {
		writeError(w, http.StatusBadRequest, "请先粘贴资料")
		return
	}
	if len([]rune(input.Text)) > 20000 {
		writeError(w, http.StatusBadRequest, "一次最多 20000 字")
		return
	}
	profile, err := s.danmakuPoolProfile(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
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
	document := newMaterialImportDocument(input.Text, links, catalog.Stores)
	chunks := materialSourceChunks(input.Text, materialChunkRunes)
	if len(chunks) == 0 {
		writeError(w, 400, "没有可分析的正文")
		return
	}
	ordered := make([]materialImportChunkResult, len(chunks))
	permits := make(chan struct{}, 4)
	var workers sync.WaitGroup
	for index, chunk := range chunks {
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case permits <- struct{}{}:
				defer func() { <-permits }()
			case <-r.Context().Done():
				ordered[index].err = r.Context().Err()
				return
			}
			ordered[index] = s.analyzeMaterialSourceChunk(r.Context(), profile, objectContext, document, chunk, true)
		}()
	}
	workers.Wait()
	all := []model.PlanMaterialClaimInput{}
	warnings := []string{}
	conflicts := []any{}
	gaps := []string{}
	provider, modelName := "", ""
	failed := 0
	for index, part := range ordered {
		if part.err != nil {
			failed++
			warnings = append(warnings, fmt.Sprintf("第%d段分析失败，原文待核对：%s", index+1, strings.TrimSpace(input.Text[chunks[index].start:chunks[index].end])))
			continue
		}
		provider, modelName = part.provider, part.model
		warnings = append(warnings, part.warnings...)
		conflicts = append(conflicts, part.conflicts...)
		gaps = append(gaps, part.gaps...)
		for _, item := range part.items {
			if item.ScopeKind == "" && len(item.ObjectTargets) == 0 {
				item.ScopeKind = "unresolved"
			}
			item = normalizeMaterialClaim(item, tenantID, planID)
			resolveMaterialTargets(&item, links)
			if item.SourceRef != nil {
				if e := validateMaterialImportEvidence(&item, input.Text); e != nil {
					item.SourceIssue = e.Error()
				}
			}
			evidence := materialImportEvidenceText(item, input.Text)
			annotateMaterialClaim(&item, evidence)
			item.SourceContext = nil
			all = append(all, item)
		}
	}
	if len(all) == 0 && failed == len(chunks) {
		writeError(w, http.StatusBadGateway, "所有资料段分析失败，原草稿已保留，请重试")
		return
	}
	if err := s.store.SaveMaterialSource(r.Context(), tenantID, planID, document.id, input.Text); err != nil {
		writeError(w, http.StatusInternalServerError, "原文保存失败，原草稿已保留")
		return
	}
	writeJSON(w, http.StatusOK, materialClaimAnalyzeOutput{
		Items: dedupeMaterialClaims(all), Provider: provider, Model: modelName,
		ModeHint: "unknown", Warnings: warnings, Conflicts: conflicts, Gaps: gaps,
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
	if err := s.validateMaterialImportSources(r.Context(), tenantID, planID, input.Items); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	existing, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取资料库失败，未开始入库")
		return
	}
	links, err := s.materialLinkObjects(r, tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取方案对象失败，未开始入库")
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
		writeJSON(w, 200, map[string]any{"ok": true, "added": 0, "accepted_client_ids": accepted, "embedding_model": ""})
		return
	}
	modelName, vectors := s.materialClaimEmbedding(r.Context(), candidates)
	added, err := s.store.ReplacePlanMaterialClaims(r.Context(), tenantID, planID, actor.UserID, candidates, modelName, vectors)
	if err != nil {
		if errors.Is(err, db.ErrMaterialClaimConflict) {
			writeError(w, 409, "资料已变化，请刷新后重新确认")
		} else {
			writeError(w, 500, "资料入库失败，整批未采用")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "added": added, "accepted_client_ids": accepted, "embedding_model": ""})
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
	item.SharedSourceKey = ""
	item.SharedSourceNeedsReview = false
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
	if item.ScopeKind != "product" && item.ScopeKind != "object" && item.ScopeKind != "store" && item.ScopeKind != "unresolved" && item.ScopeKind != "voucher" && item.ScopeKind != "variant" && item.ScopeKind != "service" && item.ScopeKind != "item" && item.ScopeKind != "item_group" {
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
	items, err := materialClaimItems(root, true)
	if err == nil && len(items) == 0 {
		return nil, errors.New("no usable claims")
	}
	return items, err
}

func materialClaimItems(root any, legacy bool) ([]model.PlanMaterialClaimInput, error) {
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
		if legacy {
			out = append(out, expandMaterialClaim(parsed)...)
		} else {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 && len(rows) > 0 {
		return nil, errors.New("no usable claims")
	}
	return out, nil
}

func dedupeMaterialClaims(items []model.PlanMaterialClaimInput) []model.PlanMaterialClaimInput {
	seen := map[string]int{}
	out := make([]model.PlanMaterialClaimInput, 0, len(items))
	for _, item := range items {
		value := strings.Join(strings.Fields(item.Value), "")
		key := item.ScopeKind + ":" + strconv.FormatInt(item.ProductLinkID, 10) + ":" + item.StoreName + ":" + item.ScopeName + ":" + fmt.Sprint(item.ObjectTargets) + ":" + item.Destination + ":" + item.ClaimKey + ":" + item.Topic + ":" + item.Attribute + ":" + item.Polarity + ":" + item.Question + ":" + item.Answer + ":" + item.Content + ":" + value + ":" + string(item.Conditions) + ":" + string(item.Exceptions)
		if index, exists := seen[key]; exists {
			if out[index].SourceIssue != "" && item.SourceIssue == "" && item.SourceRef != nil {
				out[index] = item
			}
			continue
		}
		seen[key] = len(out)
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
	var body struct {
		model.PlanMaterialClaimInput
		// Older clients may include these response fields when resaving a claim.
		Status           string `json:"status"`
		CreatedByUserID  *int64 `json:"created_by_user_id"`
		ExpectedRevision string `json:"expected_revision"`
	}
	if err := readJSON(w, r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "事实修改的数据格式不正确，请刷新后重试。")
		return
	}
	input := normalizeMaterialClaim(body.PlanMaterialClaimInput, tenantID, planID)
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
	current, err := s.store.ListPlanMaterialClaims(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, 500, "读取资料失败")
		return
	}
	var previous *model.PlanMaterialClaim
	for i := range current {
		if current[i].ID == id {
			previous = &current[i]
			break
		}
	}
	if previous == nil {
		writeError(w, 404, "资料不存在")
		return
	}
	currentRevision := db.MaterialClaimRevision(*previous)
	sourceChanged := input.SourceSentenceEdited
	if (body.ExpectedRevision != "" && body.ExpectedRevision != currentRevision) ||
		(body.ExpectedRevision == "" && !db.SameMaterialSource(input.SourceRef, previous.SourceRef)) ||
		(!sourceChanged && strings.TrimSpace(input.SourceSentence) != strings.TrimSpace(previous.SourceSentence)) {
		writeError(w, http.StatusConflict, "事实或共享原句已更新，请刷新后重试")
		return
	}
	input.SharedSourceKey = previous.SharedSourceKey
	input.SharedSourceNeedsReview = previous.SharedSourceNeedsReview
	input.InitialSourceRef = previous.InitialSourceRef
	input.SourceRef = previous.SourceRef
	if sourceChanged {
		if err := bindEditedMaterialSource(&input); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		input.SourceSentence = previous.SourceSentence
		if input.SourceRef != nil {
			candidates := []model.PlanMaterialClaimInput{input}
			if err := s.validateMaterialImportSources(r.Context(), tenantID, planID, candidates); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			input = candidates[0]
		}
		db.RecheckSharedSourceReview(&input, *previous)
	}
	if err := validateMaterialClaim(input); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	// 修改时按新文本重新生成这一条事实的向量；失败则不阻断保存，
	// 由后续回填任务补上，避免把旧文本的向量留给新内容。
	modelName, vectors := s.materialClaimEmbedding(r.Context(), []model.PlanMaterialClaimInput{input})
	raw := materialVectorsJSON(vectors)
	if err := s.store.UpdatePlanMaterialClaimAtRevision(r.Context(), tenantID, planID, id, input, modelName, raw, currentRevision, sourceChanged); err != nil {
		if errors.Is(err, db.ErrMaterialClaimNotFound) {
			writeError(w, 404, "资料不存在或已更新")
		} else if errors.Is(err, db.ErrMaterialClaimConflict) {
			writeError(w, 409, "事实原句或对象目录已更新，请刷新后重试")
		} else {
			writeError(w, 500, "资料更新失败，原资料保持")
		}
		return
	}
	s.scheduleFactCardRebuild(tenantID, planID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "embedding_model": modelName})
}

func (s *Server) materialClaimEditVector(ctx context.Context, previous model.PlanMaterialClaim, input model.PlanMaterialClaimInput) (string, []byte, int, error) {
	if previous.SourceRef != nil || previous.EmbeddingModel == "" {
		return "", nil, 0, nil
	}
	embedder, ok := s.semanticMetrics.(interface {
		Enabled() bool
		Model() string
		EmbedTexts(context.Context, []string) ([][]float32, error)
	})
	if !ok || !embedder.Enabled() {
		return "", nil, http.StatusBadRequest, errors.New("服务端向量模型未启用")
	}
	vectors, err := embedMaterialTexts(ctx, embedder, []string{materialDocumentText(input)})
	if err != nil || db.ValidateMaterialVectors(vectors, 1) != nil {
		return "", nil, http.StatusBadGateway, errors.New("向量化失败")
	}
	raw, _ := json.Marshal(vectors[0])
	return embedder.Model(), raw, 0, nil
}

func (s *Server) planMaterialClaimBenefit(w http.ResponseWriter, r *http.Request) {
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
		Revision  string `json:"revision"`
		IsBenefit *bool  `json:"is_benefit"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if len(input.Revision) != 64 || input.IsBenefit == nil {
		writeError(w, http.StatusBadRequest, "请刷新资料后重试")
		return
	}
	if err := s.store.SetPlanMaterialClaimBenefit(r.Context(), tenantID, planID, id, input.Revision, *input.IsBenefit); err != nil {
		switch {
		case errors.Is(err, db.ErrMaterialClaimNotFound):
			writeError(w, http.StatusNotFound, "资料不存在")
		case errors.Is(err, db.ErrMaterialClaimConflict):
			writeError(w, http.StatusConflict, "资料已变化，请刷新后重试")
		default:
			writeError(w, http.StatusInternalServerError, "福利标记保存失败")
		}
		return
	}
	s.scheduleFactCardRebuild(tenantID, planID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
	s.scheduleFactCardRebuild(tenantID, planID)
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
