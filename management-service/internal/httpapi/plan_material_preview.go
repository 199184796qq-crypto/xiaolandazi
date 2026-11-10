package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/db"
	"net/http"
	"regexp"
	"strings"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

func materialClientIDs(items []model.PlanMaterialClaimInput) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ClientID)
	}
	return out
}
func normalizeMaterialDetails(it *model.PlanMaterialClaimInput) {
	normalizeMaterialTargets(it)
	if it.Destination == "" {
		if it.Question != "" {
			if it.Answer != "" {
				it.Destination = "qa"
			} else {
				it.Destination = "gap"
			}
		} else {
			it.Destination = "fact"
		}
	}
	it.Exceptions = normalizeMaterialPredicates(it.Exceptions)
	it.Conditions = normalizeMaterialPredicates(it.Conditions)
	if it.Question != "" && it.Answer != "" && it.Destination == "qa" {
		it.Content = it.Answer
	}
	if len(materialPredicates(it.Conditions))+len(materialPredicates(it.Exceptions)) > 0 {
		it.FactType = "conditional"
	} else if it.FactType != "conditional" {
		it.FactType = it.Polarity
	}
	if it.ClientID == "" {
		sum := sha256.Sum256([]byte(it.ScopeKind + it.StoreName + it.ScopeName + fmt.Sprint(it.ProductLinkID) + it.ClaimKey + it.Content + it.Question + fmt.Sprint(it.ObjectTargets) + string(it.Conditions) + string(it.Exceptions)))
		it.ClientID = fmt.Sprintf("claim-%x", sum[:12])
	}
}
func materialPredicates(raw json.RawMessage) []map[string]any {
	var rows []any
	_ = json.Unmarshal(raw, &rows)
	out := []map[string]any{}
	for _, row := range rows {
		switch x := row.(type) {
		case string:
			out = append(out, map[string]any{"op": "raw", "raw": x, "status": "unresolved"})
		case map[string]any:
			out = append(out, x)
		}
	}
	return out
}
func normalizeMaterialPredicates(raw json.RawMessage) json.RawMessage {
	out, _ := json.Marshal(materialPredicates(raw))
	return out
}
func annotateMaterialClaim(it *model.PlanMaterialClaimInput, source string) {
	normalizeMaterialDetails(it)
	it.PolarityIssue = ""
	it.ConditionIssue = ""
	if it.FactType == "conditional" && len(materialPredicates(it.Conditions))+len(materialPredicates(it.Exceptions)) == 0 {
		it.ConditionIssue = "条件事实缺少条件，请编辑核对"
	}
	if it.SourceQuote == "" || !strings.Contains(source, it.SourceQuote) {
		it.NeedsConfirmation = true
	}
	for _, group := range []json.RawMessage{it.Conditions, it.Exceptions} {
		for _, c := range materialPredicates(group) {
			op := materialString(c["op"])
			raw := materialString(c["raw"])
			if op == "not_in" && materialString(c["universe"]) == "" {
				it.ConditionIssue = "补集条件缺少全集"
			}
			if raw != "" && !strings.Contains(source, raw) {
				it.ConditionIssue = "条件原话不在资料中"
			}
			if op == "raw" || materialString(c["status"]) == "unresolved" {
				it.ConditionIssue = "条件尚未归一，请编辑核对"
			}
		}
	}
	if it.Destination == "fact" {
		if it.Polarity == "negative" && !regexp.MustCompile(`不|未|无|没有|禁止|不能`).MatchString(it.Content+it.SourceQuote) {
			it.PolarityIssue = "否定缺少原文依据"
		}
		if it.Polarity == "positive" && regexp.MustCompile(`^(不支持|不能|不可以|禁止|没有|无)`).MatchString(it.Content) {
			it.PolarityIssue = "正负极性与内容不一致"
		}
	}
	if it.PolarityIssue != "" || it.ConditionIssue != "" {
		it.NeedsConfirmation = true
	}
	if it.Destination == "fact" && len(regexp.MustCompile(`\d+(?:\.\d+)?\s*(?:元|块|g|克|kg|天|小时|分钟|人|位|张|道|种|折|%)`).FindAllString(it.Content, -1)) >= 2 {
		it.NeedsSplit = true
		it.AtomicityReason = "包含多个数值/单位，可能未拆分"
		it.NeedsConfirmation = true
	}
	if it.Risks == nil {
		it.Risks = []map[string]any{}
	}
	for _, flag := range it.RiskFlags {
		it.Risks = append(it.Risks, map[string]any{"type": flag, "text": it.Content, "reason": "审核模型标记的风险点", "suggestion": "核对原文依据、适用条件和有效期后修改表述"})
	}
	if hit := regexp.MustCompile(`一模一样|最好|最佳|第一|100%|绝对|保证`).FindString(it.Content); hit != "" {
		it.Risks = append(it.Risks, map[string]any{"type": "absolute_claim", "text": hit, "reason": "含绝对化表达，需核对依据", "suggestion": "去掉绝对化措辞，保留原文可核实内容"})
		it.P0Hits = append(it.P0Hits, map[string]any{"rule": "absolute_claim", "text": hit})
	}
}
func validateMaterialClaim(it model.PlanMaterialClaimInput) error {
	if len(it.ObjectTargets) > 50 {
		return errors.New("每条属性最多关联50个对象")
	}
	if strings.TrimSpace(it.Content) == "" || len([]rune(it.Content)) > 12000 {
		return errors.New("资料内容为空或过长")
	}
	switch it.Destination {
	case "fact", "qa", "material":
	default:
		return errors.New("词义和待补问题保留在草稿，不进入事实语义库")
	}
	if it.ScopeKind == "unresolved" || (it.ScopeKind == "product" && it.ProductLinkID <= 0) || (it.ScopeKind == "store" && strings.TrimSpace(it.StoreName) == "") {
		return errors.New("请先确认资料归属")
	}
	if (it.ScopeKind == "item" || it.ScopeKind == "variant" || it.ScopeKind == "voucher" || it.ScopeKind == "service" || it.ScopeKind == "item_group") && strings.TrimSpace(it.ScopeName) == "" {
		return errors.New("请先补全对象名称并确认归属")
	}
	if it.Destination == "qa" && (strings.TrimSpace(it.Question) == "" || strings.TrimSpace(it.Answer) == "") {
		return errors.New("标准问答缺少问题或答案")
	}
	check := it
	annotateMaterialClaim(&check, it.SourceQuote+"\n"+it.SourceSentence)
	if check.ConditionIssue != "" || check.PolarityIssue != "" {
		return errors.New("请先核对条件或极性：" + check.ConditionIssue + check.PolarityIssue)
	}
	if len([]rune(it.ScopeName)) > 255 || len([]rune(it.StoreName)) > 255 || len([]rune(it.ClaimKey)) > 128 || len([]rune(it.Topic)) > 255 {
		return errors.New("对象或属性名称过长")
	}
	return nil
}
func materialDocumentText(it model.PlanMaterialClaimInput) string {
	subject := materialSubject(it)
	parts := []string{subject, it.Topic}
	if it.Destination == "qa" {
		parts = append(parts, it.Question, strings.Join(it.Aliases, " / "), it.Answer)
	} else {
		parts = append(parts, it.Content)
	}
	if it.Polarity == "negative" {
		parts = append(parts, "极性：否定")
	}
	for i, group := range []json.RawMessage{it.Conditions, it.Exceptions} {
		for _, c := range materialPredicates(group) {
			prefix := "条件："
			if i == 1 {
				prefix = "例外："
			}
			raw, _ := json.Marshal(c)
			parts = append(parts, prefix+string(raw))
		}
	}
	return strings.Join(parts, "\n")
}

// Review operations share the operator-owned review role and authenticated
// tenant/plan checks. They never invoke the local preview server or its MOCK.
func (s *Server) planMaterialPreview(w http.ResponseWriter, r *http.Request) {
	_, _, _, ok := s.resolvePlanMaterialClaimScope(w, r)
	if !ok {
		return
	}
	var input struct {
		Operation string                         `json:"operation"`
		Items     []model.PlanMaterialClaimInput `json:"items"`
	}
	if err := readJSON(w, r, &input); err != nil {
		return
	}
	if len(input.Items) == 0 || len(input.Items) > 500 {
		writeError(w, 400, "请选择1至500条资料")
		return
	}
	for i := range input.Items {
		input.Items[i] = normalizeMaterialClaim(input.Items[i], 0, 0)
	}
	prompt := materialQuestionsPrompt
	maxTokens := 5000
	var payload any
	switch input.Operation {
	case "questions":
		claims := []map[string]any{}
		for _, it := range input.Items {
			if it.Destination == "gap" || it.Destination == "word" || it.Destination == "material" {
				continue
			}
			claims = append(claims, map[string]any{"id": it.ClientID, "subject": materialSubject(it), "attribute": it.Topic, "value": it.Content, "conditions": it.Conditions, "exceptions": it.Exceptions})
		}
		if len(claims) == 0 {
			writeJSON(w, 200, map[string]any{"questions": []any{}})
			return
		}
		payload = claims
	case "split", "attribute_split":
		if len(input.Items) != 1 {
			writeError(w, 400, "每次重拆一条资料")
			return
		}
		it := input.Items[0]
		prompt = materialResplitPrompt
		if input.Operation == "attribute_split" {
			prompt = materialAttributeSplitPrompt
		}
		maxTokens = 2400
		payload = map[string]any{"topic": it.Topic, "content": it.Content, "source_quote": it.SourceQuote, "conditions": it.Conditions, "exceptions": it.Exceptions}
	default:
		writeError(w, 400, "不支持的资料操作")
		return
	}
	settings, err := s.store.AnchorGenerationSettings(r.Context())
	if err != nil {
		writeError(w, 500, "读取审核模型配置失败")
		return
	}
	profile := settings.Cycle.TextProfileID
	if profile == "" {
		profile = settings.SafetyProfileID
	}
	if profile == "" {
		writeError(w, 400, "请配置审核模型")
		return
	}
	raw, _ := json.Marshal(payload)
	result, _, err := s.completeAnchorStyleWorkbench(r.Context(), profile, "material_import_analyze", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(raw)}}, maxTokens, false)
	if err != nil {
		writeError(w, 502, "审核模型处理失败")
		return
	}
	if result.FinishReason == "length" {
		writeError(w, 502, "审核模型输出截断，请减少条目后重试")
		return
	}
	root, err := parseLooseJSONValue(result.Text)
	if err != nil {
		writeError(w, 502, "审核模型返回无法解析")
		return
	}
	data, ok := root.(map[string]any)
	if !ok {
		writeError(w, 502, "审核模型返回格式不正确")
		return
	}
	if input.Operation == "questions" {
		rows, _ := data["questions"].([]any)
		questions := []any{}
		known := map[string]bool{}
		for _, it := range input.Items {
			known[it.ClientID] = true
		}
		for _, row := range rows {
			q, ok := row.(map[string]any)
			if !ok {
				continue
			}
			ids, ok := q["claim_ids"].([]any)
			if !ok || len(ids) != 1 || !known[materialString(ids[0])] {
				continue
			}
			text := strings.TrimSpace(materialString(q["q"]))
			if text == "" {
				continue
			}
			questions = append(questions, map[string]any{"q": text, "claim_ids": []string{materialString(ids[0])}})
		}
		writeJSON(w, 200, map[string]any{"questions": questions, "model": result.Model})
		return
	}
	parent := input.Items[0]
	rows, _ := data["items"].([]any)
	items := []model.PlanMaterialClaimInput{}
	for i, row := range rows {
		part, ok := row.(map[string]any)
		if !ok {
			continue
		}
		value := strings.TrimSpace(materialString(part["value"]))
		if value == "" {
			if values, ok := part["values"].([]any); ok {
				for _, v := range values {
					if value != "" {
						value += "、"
					}
					value += materialString(v)
				}
			}
		}
		if value == "" || (!strings.Contains(parent.Content, value) && !strings.Contains(parent.SourceQuote, value)) {
			continue
		}
		it := parent
		it.Content = value
		it.Value = value
		it.Topic = materialString(part["attribute"])
		it.Attribute = it.Topic
		if it.Topic != "" {
			it.ClaimKey = it.Topic
		}
		it.ClientID = fmt.Sprintf("%s-s%d", parent.ClientID, i)
		it.NeedsSplit = false
		it.AtomicityReason = ""
		it.NeedsConfirmation = true
		items = append(items, it)
	}
	writeJSON(w, 200, map[string]any{"items": items, "single": len(items) <= 1, "model": result.Model})
}
func materialSubject(it model.PlanMaterialClaimInput) string {
	targets := materialTargets(it)
	labels := []string{}
	for _, t := range targets {
		labels = append(labels, materialTargetLabel(t))
	}
	if len(labels) == 0 {
		return "待归属"
	}
	return strings.Join(labels, "、")
}

// Import uses conservative batches compatible with the existing Qwen endpoint;
// the service still owns credentials, model and request accounting.
func embedMaterialTexts(ctx context.Context, e interface {
	EmbedTexts(context.Context, []string) ([][]float32, error)
}, texts []string) ([][]float32, error) {
	out := [][]float32{}
	for start := 0; start < len(texts); start += 8 {
		end := start + 8
		if end > len(texts) {
			end = len(texts)
		}
		vectors, err := e.EmbedTexts(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		if err := db.ValidateMaterialVectors(vectors, end-start); err != nil {
			return nil, err
		}
		out = append(out, vectors...)
	}
	if err := db.ValidateMaterialVectors(out, len(texts)); err != nil {
		return nil, err
	}
	return out, nil
}

// Expand only explicit enumerations, preserving the source and predicates on
// every resulting row. Ordinary parallel phrases never become invented objects.
func expandMaterialClaim(it model.PlanMaterialClaimInput) []model.PlanMaterialClaimInput {
	if it.ObjectTargets != nil || (it.Destination != "" && it.Destination != "fact") {
		return []model.PlanMaterialClaimInput{it}
	}
	match := regexp.MustCompile(`^(.+?)(?:均为|都是|统一为|统一是)(.+)$`).FindStringSubmatch(it.Content)
	if match == nil {
		return []model.PlanMaterialClaimInput{bindMaterialScope(it)}
	}
	segments := []string{}
	start, depth := 0, 0
	chars := []rune(match[1])
	for i, ch := range chars {
		if ch == '（' || ch == '(' {
			depth++
		}
		if ch == '）' || ch == ')' {
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && (ch == '、' || ch == '，' || ch == ',') {
			segments = append(segments, strings.TrimSpace(string(chars[start:i])))
			start = i + 1
		}
	}
	segments = append(segments, strings.TrimSpace(string(chars[start:])))
	if len(segments) < 2 {
		return []model.PlanMaterialClaimInput{bindMaterialScope(it)}
	}
	common := strings.TrimSpace(match[2])
	out := []model.PlanMaterialClaimInput{}
	var shared *model.PlanMaterialClaimInput
	for _, segment := range segments {
		name, qualifier := segment, ""
		if m := regexp.MustCompile(`^(.+?)[（(]([^）)]+)[）)]\s*$`).FindStringSubmatch(segment); m != nil {
			name = strings.TrimSpace(m[1])
			qualifier = strings.TrimSpace(m[2])
		}
		part := it
		part.ObjectTargets = nil
		part.ClientID = ""
		part.Content = common
		part.Value = common
		part.NeedsConfirmation = true
		part.StoreName = ""
		part.ScopeName = ""
		part.ProductLinkID = 0
		if m := regexp.MustCompile(`^第?(\d+)\s*号`).FindStringSubmatch(name); m != nil {
			part.ScopeKind = "product"
			part.ProductLinkID = materialInt64(m[1])
		} else if regexp.MustCompile(`券|套餐|双人餐|四人餐|单人餐|次卡`).MatchString(name) {
			part.ScopeKind = "voucher"
			part.ScopeName = name
		} else if regexp.MustCompile(`门店|店$`).MatchString(name) {
			part.ScopeKind = "store"
			part.StoreName = name
		} else if regexp.MustCompile(`服务|保洁|护理|洗护`).MatchString(name) {
			part.ScopeKind = "service"
			part.ScopeName = name
		} else if regexp.MustCompile(`(?i)\d+\s*(g|克|kg|毫升|ml)`).MatchString(name) {
			part.ScopeKind = "variant"
			part.ScopeName = name
		} else {
			return []model.PlanMaterialClaimInput{bindMaterialScope(it)}
		}
		if part.SourceQuote == "" {
			part.SourceQuote = it.Content
		}
		part.ClaimKey = "product_name"
		part.Topic = "商品名称"
		if regexp.MustCompile(`^(?:¥|￥)?\s*\d+(?:\.\d+)?\s*元?$|价格|售价`).MatchString(common) {
			part.ClaimKey = "price"
			part.Topic = "价格"
		} else if regexp.MustCompile(`(?i)\d+\s*(g|克|kg|毫升|ml)`).MatchString(common) {
			part.ClaimKey = "spec"
			part.Topic = "规格"
		} else if regexp.MustCompile(`包邮|运费|邮费`).MatchString(common) {
			part.ClaimKey = "shipping_fee"
			part.Topic = "运费"
		}
		part.Attribute = part.Topic
		if part.ScopeKind == "product" || part.ScopeKind == "store" {
			if shared == nil {
				copy := part
				copy.ObjectTargets = []model.MaterialObjectTarget{}
				shared = &copy
			}
			shared.ObjectTargets = append(shared.ObjectTargets, materialTargets(part)...)
		} else {
			// An unlinked named product/card is pending, never plan-wide.
			part.ScopeKind = "unresolved"
			part.ObjectTargets = []model.MaterialObjectTarget{}
			out = append(out, part)
		}
		if qualifier != "" {
			q := part
			q.Content = qualifier
			q.Value = qualifier
			q.ClaimKey = "qualifier"
			q.Topic = "限定"
			if regexp.MustCompile(`礼品|礼盒|包装|家庭装|普通装|试用装|套装`).MatchString(qualifier) {
				q.ClaimKey = "packaging"
				q.Topic = "包装"
			} else if regexp.MustCompile(`半只|整只|单人|双人|四人|\d+`).MatchString(qualifier) {
				q.ClaimKey = "spec"
				q.Topic = "规格"
			}
			q.Attribute = q.Topic
			out = append(out, q)
		}
	}
	if shared != nil {
		out = append([]model.PlanMaterialClaimInput{*shared}, out...)
	}
	return out
}
func bindMaterialScope(it model.PlanMaterialClaimInput) model.PlanMaterialClaimInput {
	if it.ScopeKind != "" && it.ScopeKind != "plan" && it.ScopeKind != "unresolved" {
		return it
	}
	text := strings.TrimSpace(it.SourceQuote + " " + it.Content)
	if m := regexp.MustCompile(`^第?(\d+)\s*号(?:链接)?`).FindStringSubmatch(text); m != nil {
		it.ScopeKind = "product"
		it.ProductLinkID = materialInt64(m[1])
	}
	return it
}
