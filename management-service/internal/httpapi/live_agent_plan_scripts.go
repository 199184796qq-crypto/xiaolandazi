package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

var livePlanProductLinkPattern = regexp.MustCompile(`([0-9]{1,2}|[一二三四五六七八九十]{1,3})[[:space:]]*号([[:space:]]*(链接|商品))?`)
var livePlanStyleSpecificFactPattern = regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?[[:space:]]*(?:元|块|斤|公斤|克|升|毫升|桶|件|盒|袋))|([0-9一二三四五六七八九十]{1,3}[[:space:]]*号[[:space:]]*(?:链接|商品))`)

type livePlanAnchorStyleDimensionDefinition struct {
	Key   string
	Group string
	Label string
}

var livePlanAnchorStyleDimensionDefinitions = []livePlanAnchorStyleDimensionDefinition{
	{Key: "opening_pattern", Group: "structure", Label: "开场习惯"},
	{Key: "sentence_rhythm", Group: "language", Label: "句子节奏"},
	{Key: "connectors", Group: "language", Label: "口头连接词"},
	{Key: "audience_address", Group: "language", Label: "称呼习惯"},
	{Key: "repetition_strategy", Group: "structure", Label: "重复方式"},
	{Key: "emphasis_style", Group: "language", Label: "强调方式"},
	{Key: "product_explanation_path", Group: "structure", Label: "产品讲解路径"},
	{Key: "storytelling", Group: "structure", Label: "故事化程度"},
	{Key: "parameter_expression", Group: "structure", Label: "参数表达方式"},
	{Key: "price_expression", Group: "structure", Label: "价格表达习惯"},
	{Key: "link_handoff", Group: "structure", Label: "链接承接方式"},
	{Key: "cta_style", Group: "interaction", Label: "CTA / 逼单风格"},
	{Key: "interaction_style", Group: "interaction", Label: "互动方式"},
	{Key: "qa_structure", Group: "interaction", Label: "回答问题结构"},
	{Key: "mainline_resume", Group: "interaction", Label: "答疑后回主线"},
	{Key: "transition_style", Group: "interaction", Label: "转场方式"},
	{Key: "emotion_curve", Group: "emotion", Label: "情绪强度曲线"},
	{Key: "pause_chunking", Group: "emotion", Label: "自然停顿与断句"},
	{Key: "humanization", Group: "emotion", Label: "真人化表达"},
	{Key: "information_density", Group: "structure", Label: "语言信息密度"},
	{Key: "vocabulary_complexity", Group: "language", Label: "用词复杂度"},
	{Key: "tone_tendency", Group: "language", Label: "正负表达倾向"},
	{Key: "closing_style", Group: "structure", Label: "收尾习惯"},
	{Key: "variation_freedom", Group: "structure", Label: "表达变化幅度"},
}

func flattenAnalysisStringValues(value any, output *[]string) {
	switch item := value.(type) {
	case string:
		item = strings.TrimSpace(item)
		if item != "" {
			*output = append(*output, item)
		}
	case []any:
		for _, nested := range item {
			flattenAnalysisStringValues(nested, output)
		}
	}
}

func normalizePlanScriptAnalysisJSON(raw string) string {
	raw = stripPolicyJSONFence(raw)
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return raw
	}
	normalizeListField := func(object map[string]any, key string) {
		value, ok := object[key]
		if !ok {
			return
		}
		items := []string{}
		flattenAnalysisStringValues(value, &items)
		deduped := make([]string, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			if _, exists := seen[item]; exists {
				continue
			}
			seen[item] = struct{}{}
			deduped = append(deduped, item)
		}
		object[key] = deduped
	}
	if links, ok := root["product_links"].([]any); ok {
		for _, item := range links {
			if object, ok := item.(map[string]any); ok {
				normalizeListField(object, "source_quotes")
			}
		}
	}
	if anchorStyle, ok := root["anchor_style"].(map[string]any); ok {
		if dimensions, ok := anchorStyle["dimensions"].([]any); ok {
			for _, item := range dimensions {
				if object, ok := item.(map[string]any); ok {
					normalizeListField(object, "evidence_quotes")
				}
			}
		}
		normalizeListField(anchorStyle, "reusable_rules")
		normalizeListField(anchorStyle, "candidate_patterns")
		normalizeListField(anchorStyle, "excluded_from_style")
	}
	normalized, err := json.Marshal(root)
	if err != nil {
		return raw
	}
	return string(normalized)
}

func liveAgentPlanScriptPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("scriptID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "直播话术 ID 无效")
		return 0, false
	}
	return value, true
}

func normalizeReadableLiveScript(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	lines := strings.Split(raw, "\n")
	cleaned := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if !blank && len(cleaned) > 0 {
				cleaned = append(cleaned, "")
			}
			blank = true
			continue
		}
		blank = false
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func validateLiveAgentPlanScriptInput(input *model.SaveLiveAgentPlanScriptInput, creating bool) error {
	input.Title = strings.TrimSpace(input.Title)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.OriginalName = strings.TrimSpace(input.OriginalName)
	if input.SourceType == "" {
		input.SourceType = "paste"
	}
	if input.SourceType != "paste" && input.SourceType != "upload" {
		return errors.New("话术来源只能是粘贴或上传")
	}
	if utf8.RuneCountInString(input.Title) > 180 {
		return errors.New("话术标题不能超过 180 字")
	}
	if utf8.RuneCountInString(input.OriginalName) > 255 {
		return errors.New("原始文件名过长")
	}
	if creating {
		input.RawText = strings.TrimSpace(input.RawText)
		if input.RawText == "" {
			return errors.New("请先粘贴或上传直播话术")
		}
		if utf8.RuneCountInString(input.RawText) > 120000 {
			return errors.New("单份直播话术暂不能超过 12 万字")
		}
		if strings.TrimSpace(input.ReadableText) == "" {
			input.ReadableText = normalizeReadableLiveScript(input.RawText)
		}
	}
	input.ReadableText = strings.TrimSpace(input.ReadableText)
	if input.ReadableText == "" {
		return errors.New("整理后的话术不能为空")
	}
	if utf8.RuneCountInString(input.ReadableText) > 120000 {
		return errors.New("整理后的直播话术暂不能超过 12 万字")
	}
	return nil
}

func (s *Server) liveAgentPlanScriptList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	items, err := s.store.ListLiveAgentPlanScripts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播话术失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) liveAgentPlanScriptCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.SaveLiveAgentPlanScriptInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播话术格式错误")
		return
	}
	if err := validateLiveAgentPlanScriptInput(&input, true); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Title == "" {
		input.Title = "直播话术 " + time.Now().Format("01-02 15:04")
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.SaveLiveAgentPlanScript(r.Context(), tenantID, planID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "当前直播方案不存在或已归档")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存直播话术失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) liveAgentPlanScriptUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	scriptID, ok := liveAgentPlanScriptPathID(w, r)
	if !ok {
		return
	}
	var input model.SaveLiveAgentPlanScriptInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播话术格式错误")
		return
	}
	if err := validateLiveAgentPlanScriptInput(&input, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.UpdateLiveAgentPlanScript(r.Context(), tenantID, planID, scriptID, actor.UserID, input)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptNotFound) {
		writeError(w, http.StatusNotFound, "直播话术不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "修改直播话术失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func chinesePlanNumber(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	digits := map[rune]int{
		'一': 1, '二': 2, '三': 3, '四': 4, '五': 5,
		'六': 6, '七': 7, '八': 8, '九': 9,
	}
	runes := []rune(raw)
	if len(runes) == 1 {
		if runes[0] == '十' {
			return 10, true
		}
		value, ok := digits[runes[0]]
		return value, ok
	}
	if len(runes) == 2 && runes[0] == '十' {
		value, ok := digits[runes[1]]
		return 10 + value, ok
	}
	if len(runes) == 2 && runes[1] == '十' {
		value, ok := digits[runes[0]]
		return value * 10, ok
	}
	if len(runes) == 3 && runes[1] == '十' {
		tens, okTens := digits[runes[0]]
		ones, okOnes := digits[runes[2]]
		if okTens && okOnes {
			return tens*10 + ones, true
		}
	}
	return 0, false
}

func canonicalPlanProductLinkKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	match := livePlanProductLinkPattern.FindStringSubmatch(raw)
	if len(match) < 2 {
		return raw
	}
	token := strings.TrimSpace(match[1])
	if value, err := strconv.Atoi(token); err == nil && value > 0 {
		return fmt.Sprintf("%d号链接", value)
	}
	if value, ok := chinesePlanNumber(token); ok && value > 0 {
		return fmt.Sprintf("%d号链接", value)
	}
	return raw
}

func detectPlanProductLinkKeys(text string) []string {
	matches := livePlanProductLinkPattern.FindAllStringSubmatch(text, -1)
	seen := map[string]struct{}{}
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		key := canonicalPlanProductLinkKey(match[0])
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, key)
	}
	return items
}

func productLinkEvidenceContext(text, key string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	selected := make([]string, 0, 10)
	for index, line := range lines {
		matches := livePlanProductLinkPattern.FindAllString(line, -1)
		matched := false
		for _, item := range matches {
			if canonicalPlanProductLinkKey(item) == key {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		start := index - 2
		if start < 0 {
			start = 0
		}
		end := index + 3
		if end > len(lines) {
			end = len(lines)
		}
		block := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
		if block != "" {
			selected = append(selected, block)
		}
		if len(selected) >= 4 {
			break
		}
	}
	joined := strings.Join(selected, "\n---\n")
	if utf8.RuneCountInString(joined) > 5000 {
		joined = string([]rune(joined)[:5000])
	}
	return joined
}

func normalizeProductLinkCandidate(item model.LiveAgentPlanProductLinkCandidate) model.LiveAgentPlanProductLinkCandidate {
	item.LinkKey = canonicalPlanProductLinkKey(item.LinkKey)
	item.ProductName = strings.TrimSpace(item.ProductName)
	item.Spec = strings.TrimSpace(item.Spec)
	item.DailyPrice = strings.TrimSpace(item.DailyPrice)
	item.ActivityPrice = strings.TrimSpace(item.ActivityPrice)
	item.Quantity = strings.TrimSpace(item.Quantity)
	item.Gift = strings.TrimSpace(item.Gift)
	item.Activity = strings.TrimSpace(item.Activity)
	item.Audience = strings.TrimSpace(item.Audience)
	item.ReviewBucket = strings.ToLower(strings.TrimSpace(item.ReviewBucket))
	item.ReviewReason = strings.TrimSpace(item.ReviewReason)
	item.Confidence = strings.ToLower(strings.TrimSpace(item.Confidence))
	switch item.ReviewBucket {
	case "adoptable", "conflict", "discuss", "violation":
	default:
		item.ReviewBucket = "discuss"
	}
	quotes := make([]string, 0, len(item.SourceQuotes))
	for _, quote := range item.SourceQuotes {
		quote = strings.TrimSpace(quote)
		if quote == "" {
			continue
		}
		if utf8.RuneCountInString(quote) > 320 {
			quote = string([]rune(quote)[:320])
		}
		quotes = append(quotes, quote)
		if len(quotes) >= 6 {
			break
		}
	}
	item.SourceQuotes = quotes
	return item
}

func normalizeStyleStringList(values []string, maxItems, maxRunes int) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if maxRunes > 0 && utf8.RuneCountInString(value) > maxRunes {
			value = string([]rune(value)[:maxRunes])
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
		if maxItems > 0 && len(result) >= maxItems {
			break
		}
	}
	return result
}

func normalizeAnchorStyleProfile(profile model.LiveAgentPlanAnchorStyleProfile) model.LiveAgentPlanAnchorStyleProfile {
	profile.Summary = strings.TrimSpace(profile.Summary)
	if utf8.RuneCountInString(profile.Summary) > 600 {
		profile.Summary = string([]rune(profile.Summary)[:600])
	}
	byKey := make(map[string]model.LiveAgentPlanAnchorStyleDimension, len(profile.Dimensions))
	for _, item := range profile.Dimensions {
		item.Key = strings.ToLower(strings.TrimSpace(item.Key))
		if item.Key == "" {
			continue
		}
		item.Level = strings.TrimSpace(item.Level)
		item.Rule = strings.TrimSpace(item.Rule)
		item.Confidence = strings.ToLower(strings.TrimSpace(item.Confidence))
		switch item.Confidence {
		case "high", "medium", "low":
		default:
			item.Confidence = "low"
		}
		item.PromotionLevel = "candidate"
		item.EvidenceQuotes = normalizeStyleStringList(item.EvidenceQuotes, 2, 180)
		byKey[item.Key] = item
	}
	dimensions := make([]model.LiveAgentPlanAnchorStyleDimension, 0, len(livePlanAnchorStyleDimensionDefinitions))
	for _, definition := range livePlanAnchorStyleDimensionDefinitions {
		item, ok := byKey[definition.Key]
		if !ok {
			item = model.LiveAgentPlanAnchorStyleDimension{
				Key:            definition.Key,
				Group:          definition.Group,
				Label:          definition.Label,
				Level:          "样本不足",
				Rule:           "当前样本证据不足，暂不形成稳定风格判断。",
				Confidence:     "low",
				PromotionLevel: "candidate",
			}
		} else {
			item.Group = definition.Group
			item.Label = definition.Label
			if item.Level == "" {
				item.Level = "样本不足"
			}
			if item.Rule == "" {
				item.Rule = "当前样本证据不足，暂不形成稳定风格判断。"
				item.Confidence = "low"
			}
			if livePlanStyleSpecificFactPattern.MatchString(item.Rule) {
				item.Rule = "该维度的原始归纳夹带具体商品数值或链接信息，已从风格规则中排除，待更多样本确认。"
				item.Level = "样本不足"
				item.Confidence = "low"
			}
		}
		dimensions = append(dimensions, item)
	}
	profile.Dimensions = dimensions
	profile.ReusableRules = normalizeStyleStringList(profile.ReusableRules, 12, 160)
	filteredRules := make([]string, 0, len(profile.ReusableRules))
	for _, rule := range profile.ReusableRules {
		if livePlanStyleSpecificFactPattern.MatchString(rule) {
			continue
		}
		filteredRules = append(filteredRules, rule)
	}
	profile.ReusableRules = filteredRules
	profile.CandidatePatterns = normalizeStyleStringList(profile.CandidatePatterns, 12, 160)
	profile.ExcludedFromStyle = normalizeStyleStringList(profile.ExcludedFromStyle, 12, 160)
	return profile
}

func normalizePlanScriptAnalysis(output model.LiveAgentPlanScriptAnalysis, text string) model.LiveAgentPlanScriptAnalysis {
	output.Summary = strings.TrimSpace(output.Summary)
	output.AnchorStyle = normalizeAnchorStyleProfile(output.AnchorStyle)
	productLinks := make([]model.LiveAgentPlanProductLinkCandidate, 0, len(output.ProductLinks))
	seenLinks := map[string]struct{}{}
	for _, item := range output.ProductLinks {
		item = normalizeProductLinkCandidate(item)
		if item.LinkKey == "" {
			continue
		}
		if _, exists := seenLinks[item.LinkKey]; exists {
			continue
		}
		seenLinks[item.LinkKey] = struct{}{}
		productLinks = append(productLinks, item)
		if len(productLinks) >= 30 {
			break
		}
	}
	output.ProductLinks = productLinks
	facts := make([]model.LiveAgentPlanFactCandidate, 0, len(output.Facts))
	for _, fact := range output.Facts {
		fact.Category = strings.TrimSpace(fact.Category)
		fact.Key = strings.TrimSpace(fact.Key)
		fact.Value = strings.TrimSpace(fact.Value)
		fact.SourceQuote = strings.TrimSpace(fact.SourceQuote)
		fact.ReviewBucket = strings.ToLower(strings.TrimSpace(fact.ReviewBucket))
		fact.ReviewReason = strings.TrimSpace(fact.ReviewReason)
		fact.Confidence = strings.ToLower(strings.TrimSpace(fact.Confidence))
		fact.Note = strings.TrimSpace(fact.Note)
		if fact.Key == "" || fact.Value == "" {
			continue
		}
		fact.Status = "pending"
		switch fact.ReviewBucket {
		case "adoptable", "conflict", "discuss", "violation":
		default:
			fact.ReviewBucket = "discuss"
		}
		if utf8.RuneCountInString(fact.SourceQuote) > 240 {
			fact.SourceQuote = string([]rune(fact.SourceQuote)[:240])
		}
		facts = append(facts, fact)
		if len(facts) >= 80 {
			break
		}
	}
	output.Facts = facts
	nodes := make([]model.LiveAgentPlanRhythmNode, 0, len(output.RhythmNodes))
	for index, node := range output.RhythmNodes {
		node.Title = strings.TrimSpace(node.Title)
		node.Goal = strings.TrimSpace(node.Goal)
		node.FixedText = strings.TrimSpace(node.FixedText)
		node.Transition = strings.TrimSpace(node.Transition)
		if node.Title == "" {
			continue
		}
		if node.ExecutionMode != "verbatim" {
			node.ExecutionMode = "intent"
		}
		if node.ExecutionMode != "verbatim" {
			node.FixedText = ""
		}
		node.Order = index + 1
		nodes = append(nodes, node)
		if len(nodes) >= 30 {
			break
		}
	}
	output.RhythmNodes = nodes
	detectedLinks := detectPlanProductLinkKeys(text)
	coveredLinks := make([]string, 0, len(output.ProductLinks))
	coveredSet := map[string]struct{}{}
	for _, item := range output.ProductLinks {
		if _, ok := coveredSet[item.LinkKey]; ok {
			continue
		}
		coveredSet[item.LinkKey] = struct{}{}
		coveredLinks = append(coveredLinks, item.LinkKey)
	}
	missingLinks := make([]string, 0)
	for _, key := range detectedLinks {
		if _, ok := coveredSet[key]; !ok {
			missingLinks = append(missingLinks, key)
		}
	}
	coverage := 100
	if len(detectedLinks) > 0 {
		coverage = (len(detectedLinks) - len(missingLinks)) * 100 / len(detectedLinks)
	}
	output.Completeness = model.LiveAgentPlanAnalysisCompleteness{
		DetectedLinkKeys: detectedLinks,
		CoveredLinkKeys:  coveredLinks,
		MissingLinkKeys:  missingLinks,
		LinkCoveragePct:  coverage,
	}
	return output
}

func repairMissingProductLinks(
	ctx context.Context,
	text string,
	missing []string,
) ([]model.LiveAgentPlanProductLinkCandidate, string, string, int64, error) {
	if len(missing) == 0 {
		return nil, "", "", 0, nil
	}
	contexts := make([]string, 0, len(missing))
	for _, key := range missing {
		contextText := productLinkEvidenceContext(text, key)
		if contextText == "" {
			continue
		}
		contexts = append(contexts, fmt.Sprintf("【%s】\n%s", key, contextText))
	}
	if len(contexts) == 0 {
		return nil, "", "", 0, nil
	}
	prompt := fmt.Sprintf(`你正在补齐直播话术中漏掉的商品链接信息。
只分析下面列出的链接，不分析其它内容，不新增原文没有的信息。
每个链接必须返回一个对象；没有明确字段就留空字符串。
review_bucket 只能是 adoptable、conflict、discuss、violation。

必须补齐的链接：%s

严格返回 JSON：
{
  "product_links": [
    {
      "link_key": "1号链接",
      "product_name": "商品名称",
      "spec": "规格",
      "daily_price": "日常价",
      "activity_price": "活动价",
      "quantity": "数量/组合",
      "gift": "赠品",
      "activity": "活动口径",
      "audience": "适用人群/场景",
      "review_bucket": "adoptable|conflict|discuss|violation",
      "review_reason": "判断原因",
      "source_quotes": ["原文证据"],
      "confidence": "high|medium|low"
    }
  ]
}

【原文上下文】
%s`, strings.Join(missing, "、"), strings.Join(contexts, "\n\n"))
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只补齐直播话术中已被程序发现但第一次分析遗漏的商品链接，不编造。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      2600,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        32 * time.Second,
	})
	if err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, err
	}
	var output struct {
		ProductLinks []model.LiveAgentPlanProductLinkCandidate `json:"product_links"`
	}
	if err := json.Unmarshal([]byte(normalizePlanScriptAnalysisJSON(result.Text)), &output); err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode product link repair: %w", err)
	}
	items := make([]model.LiveAgentPlanProductLinkCandidate, 0, len(output.ProductLinks))
	for _, item := range output.ProductLinks {
		item = normalizeProductLinkCandidate(item)
		if item.LinkKey != "" {
			items = append(items, item)
		}
	}
	return items, result.Provider, result.Model, result.LatencyMS, nil
}

type livePlanAnalysisRawHook func(raw, provider, modelName string, latencyMS int64) error

func analyzeLiveAgentPlanScript(
	ctx context.Context,
	text string,
) (model.LiveAgentPlanScriptAnalysis, string, string, int64, error) {
	return analyzeLiveAgentPlanScriptWithRawHook(ctx, text, nil)
}

func analyzeLiveAgentPlanScriptWithRawHook(
	ctx context.Context,
	text string,
	onRaw livePlanAnalysisRawHook,
) (model.LiveAgentPlanScriptAnalysis, string, string, int64, error) {
	detectedLinks := detectPlanProductLinkKeys(text)
	detectedLinksJSON, _ := json.Marshal(detectedLinks)
	styleDimensionSpec := make([]string, 0, len(livePlanAnchorStyleDimensionDefinitions))
	for _, definition := range livePlanAnchorStyleDimensionDefinitions {
		styleDimensionSpec = append(styleDimensionSpec, definition.Key+"|"+definition.Group+"|"+definition.Label)
	}
	prompt := fmt.Sprintf(`你是直播电商方案分析器。你的任务是把用户提供的直播话术转换为“商品链接 + 事实依据 + 可复用直播节奏 + 主播风格画像”，供后续人工审核和直播方案生成使用。

重要规则：
1. 程序已经扫描出原文中出现的商品链接编号：%s。product_links 必须逐个覆盖这些链接；每个链接只返回一个对象，不能因为原文很长而省略。
2. 商品链接单独结构化，不要只把“1号链接商品”“2号链接商品”混进普通 facts。对每个链接尽量提取：商品名称、规格、日常价、活动价、数量/组合、赠品、活动口径、适用人群/场景、原文证据。
3. 同一链接如果出现互相冲突的商品、规格、价格或权益，review_bucket 必须标 conflict，并在 review_reason 里指出冲突。
4. 事实与风格严格分离。本次只提取话术中明确出现或可直接支持的事实，不要把表达风格当事实。
5. 原稿里说过不等于已经验证为真，所以所有 facts.status 必须返回 pending；不得自动确认。
6. 每条事实必须进一步归到 review_bucket，且只能是：
   - adoptable：可采纳事实。原文明确、语义完整、内部没有冲突，且没有明显高风险。
   - conflict：矛盾事实。同一个事实名称在原稿不同位置出现互相冲突的值或承诺。
   - discuss：待商量事实。原文模糊、不完整、带主观宣传性质、缺少关键条件，或需要人工确认后才能采用。
   - violation：严重违规事实。存在明显违法违规、平台高风险、虚假/绝对化/医疗功效/无法兑现承诺等严重风险，不能直接作为直播依据。
7. 只判断原稿自身及明显风险，不要把“无法从外部核验”本身当成矛盾；没有外部证据时优先放 discuss，而不是武断判定真假。
8. review_reason 用一句短话说明为什么归入该组。
9. 不推测地址、快递、价格、库存、承诺、规格等没有明确写出的内容。
10. category 仅从 product、link、trade、fulfillment、identity_location、other 中选择。
11. key 用稳定、简短、可复用的中文名称，例如“具体产地”“默认快递”“净含量”。
12. source_quote 只摘取支持该事实的最短原句。
13. rhythm_nodes 是“如何把一场直播讲下去”的框架，不是简单按段落摘要。每个节点要给目标、必讲点、可引用 fact_keys、建议时长和转场。
14. execution_mode 只允许 intent 或 verbatim。只有原稿明显要求逐字固定的话才用 verbatim，否则用 intent。
15. 不要生成新的销售事实，不要补写原稿没有的信息。
16. anchor_style 只学习“怎么说”，绝对不能把商品名、具体价格、规格数值、链接号、产地、快递公司、库存、功效、活动承诺等样本专属事实写进风格 rule 或 reusable_rules。
17. evidence_quotes 可以引用原文作为证据，即使原句里恰好包含商品事实；但 rule 必须抽象成换商品后仍成立的表达规律。
18. 主播风格必须逐项分析下面 24 个固定维度，每个 key 恰好返回一次，顺序保持一致。维度格式为 key|group|中文名称：
%s
19. 每个风格维度：
   - level 用“高 / 中 / 低 / 混合 / 样本不足”等简短倾向；
   - rule 用一句可以复用到其它商品的规则描述，尽量不超过 80 个中文字符；
   - evidence_quotes 最多 2 条，选最能证明说话方式的短句；
   - confidence 只能 high、medium、low；只有一处弱证据时不要给 high；
   - promotion_level 本次统一返回 candidate，因为单篇话术只能形成候选风格，不能自动升级成稳定规则。
20. reusable_rules 只放跨商品可复用、证据较充分的候选规则；candidate_patterns 放值得继续观察但证据不足的模式；excluded_from_style 说明哪些内容明确被排除在风格学习之外。

严格返回 JSON：
{
  "summary": "这份话术的整体直播逻辑",
  "anchor_style": {
    "summary": "主播整体表达画像，只描述说话方式",
    "dimensions": [
      {
        "key": "opening_pattern",
        "group": "structure",
        "label": "开场习惯",
        "level": "高|中|低|混合|样本不足",
        "rule": "抽象后的可复用表达规律，不得包含具体商品事实",
        "evidence_quotes": ["原文风格证据1", "原文风格证据2"],
        "confidence": "high|medium|low",
        "promotion_level": "candidate"
      }
    ],
    "reusable_rules": ["证据较充分的跨商品候选风格规则"],
    "candidate_patterns": ["仍需更多样本验证的风格模式"],
    "excluded_from_style": ["具体商品事实、价格、规格、链接、产地、履约等不进入主播风格"]
  },
  "product_links": [
    {
      "link_key": "1号链接",
      "product_name": "商品名称",
      "spec": "规格",
      "daily_price": "日常价",
      "activity_price": "活动价",
      "quantity": "数量/组合",
      "gift": "赠品",
      "activity": "活动口径",
      "audience": "适用人群/使用场景",
      "review_bucket": "adoptable|conflict|discuss|violation",
      "review_reason": "判断原因",
      "source_quotes": ["支持该链接信息的原文证据"],
      "confidence": "high|medium|low"
    }
  ],
  "facts": [
    {
      "category": "product|link|trade|fulfillment|identity_location|other",
      "key": "事实名称",
      "value": "事实值",
      "status": "pending",
      "review_bucket": "adoptable|conflict|discuss|violation",
      "review_reason": "为什么归到这一组",
      "source_quote": "原文最短证据",
      "confidence": "high|medium|low",
      "note": "必要时说明"
    }
  ],
  "rhythm_nodes": [
    {
      "order": 1,
      "title": "节点名称",
      "goal": "这个阶段的目的",
      "fact_keys": ["可引用的事实 key"],
      "must_cover": ["必须讲到的意思"],
      "avoid": ["不应该讲的内容或风险"],
      "execution_mode": "intent|verbatim",
      "fixed_text": "仅 verbatim 时填写",
      "duration_seconds": 60,
      "transition": "建议转场方式"
    }
  ]
}

【直播话术】
%s
`, string(detectedLinksJSON), strings.Join(styleDimensionSpec, "\n"), text)
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只做直播话术结构化分析，不执行真实业务动作，不把未核实内容自动确认成事实。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      9000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        110 * time.Second,
	})
	if err != nil {
		return model.LiveAgentPlanScriptAnalysis{}, result.Provider, result.Model, result.LatencyMS, err
	}
	if onRaw != nil {
		if err := onRaw(result.Text, result.Provider, result.Model, result.LatencyMS); err != nil {
			return model.LiveAgentPlanScriptAnalysis{}, result.Provider, result.Model, result.LatencyMS, err
		}
	}
	var output model.LiveAgentPlanScriptAnalysis
	if err := json.Unmarshal([]byte(normalizePlanScriptAnalysisJSON(result.Text)), &output); err != nil {
		return model.LiveAgentPlanScriptAnalysis{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode plan script analysis: %w", err)
	}
	normalized := normalizePlanScriptAnalysis(output, text)
	totalLatency := result.LatencyMS
	provider := result.Provider
	modelName := result.Model
	if len(normalized.Completeness.MissingLinkKeys) > 0 {
		repaired, repairProvider, repairModel, repairLatency, repairErr := repairMissingProductLinks(
			ctx,
			text,
			normalized.Completeness.MissingLinkKeys,
		)
		totalLatency += repairLatency
		if provider == "" {
			provider = repairProvider
		}
		if modelName == "" {
			modelName = repairModel
		}
		if repairErr != nil {
			log.Printf(
				"live plan product link repair failed missing=%v latency_ms=%d err=%v",
				normalized.Completeness.MissingLinkKeys, repairLatency, repairErr,
			)
		} else if len(repaired) > 0 {
			normalized.ProductLinks = append(normalized.ProductLinks, repaired...)
			normalized = normalizePlanScriptAnalysis(normalized, text)
		}
	}
	if len(normalized.Completeness.MissingLinkKeys) > 0 {
		missing := append([]string(nil), normalized.Completeness.MissingLinkKeys...)
		for _, key := range missing {
			evidence := strings.TrimSpace(productLinkEvidenceContext(text, key))
			quotes := []string{}
			if evidence != "" {
				quotes = append(quotes, evidence)
			}
			normalized.ProductLinks = append(normalized.ProductLinks, model.LiveAgentPlanProductLinkCandidate{
				LinkKey:      key,
				ReviewBucket: "discuss",
				ReviewReason: "程序已在原文检测到该链接，但智能分析仍未完整提取，请人工确认。",
				SourceQuotes: quotes,
				Confidence:   "low",
			})
		}
	}
	return normalized, provider, modelName, totalLatency, nil
}

func (s *Server) liveAgentPlanScriptAnalyzePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播话术格式错误")
		return
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "请先输入或上传直播话术")
		return
	}
	if utf8.RuneCountInString(text) > 60000 {
		writeError(w, http.StatusBadRequest, "当前智能分析单次最多处理 6 万字，请先精简或拆分话术")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 165*time.Second)
	defer cancel()
	analysis, provider, modelName, latencyMS, err := analyzeLiveAgentPlanScript(ctx, text)
	if err != nil {
		log.Printf(
			"live plan script analysis preview failed tenant=%d plan=%d chars=%d provider=%s model=%s latency_ms=%d err=%v",
			tenantID, planID, utf8.RuneCountInString(text), provider, modelName, latencyMS, err,
		)
		writeError(w, http.StatusBadGateway, "智能体暂时无法分析这份直播话术")
		return
	}
	if len(analysis.ProductLinks) == 0 && len(analysis.Facts) == 0 && len(analysis.RhythmNodes) == 0 {
		log.Printf(
			"live plan script analysis preview empty tenant=%d plan=%d chars=%d provider=%s model=%s latency_ms=%d",
			tenantID, planID, utf8.RuneCountInString(text), provider, modelName, latencyMS,
		)
		writeError(w, http.StatusBadGateway, "智能分析返回了空结果，请重新分析")
		return
	}
	log.Printf(
		"live plan script analysis preview ok tenant=%d plan=%d chars=%d links=%d facts=%d rhythm=%d style_dims=%d link_coverage=%d provider=%s model=%s latency_ms=%d",
		tenantID, planID, utf8.RuneCountInString(text), len(analysis.ProductLinks), len(analysis.Facts), len(analysis.RhythmNodes),
		len(analysis.AnchorStyle.Dimensions), analysis.Completeness.LinkCoveragePct, provider, modelName, latencyMS,
	)
	writeJSON(w, http.StatusOK, map[string]any{
		"analysis":   analysis,
		"provider":   provider,
		"model":      modelName,
		"latency_ms": latencyMS,
		"persisted":  false,
	})
}

func (s *Server) liveAgentPlanScriptAnalyze(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	scriptID, ok := liveAgentPlanScriptPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	item, err := s.store.GetLiveAgentPlanScript(r.Context(), tenantID, planID, scriptID)
	if errors.Is(err, appdb.ErrLiveAgentPlanScriptNotFound) {
		writeError(w, http.StatusNotFound, "直播话术不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播话术失败")
		return
	}
	text := strings.TrimSpace(item.ReadableText)
	if text == "" {
		text = strings.TrimSpace(item.RawText)
	}
	if utf8.RuneCountInString(text) > 60000 {
		writeError(w, http.StatusBadRequest, "当前智能分析单次最多处理 6 万字，请先精简或拆分话术")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 165*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "live_plan_script_analysis", map[string]any{
		"plan_id": planID, "script_id": scriptID,
	})
	analysis, provider, modelName, latencyMS, err := analyzeLiveAgentPlanScript(ctx, text)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", provider, modelName, latencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "智能体暂时无法分析这份直播话术")
		return
	}
	updated, err := s.store.SetLiveAgentPlanScriptAnalysis(r.Context(), tenantID, planID, scriptID, analysis, provider, modelName, latencyMS)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", provider, modelName, latencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusInternalServerError, "保存话术分析结果失败")
		return
	}
	if len(analysis.AnchorStyle.Dimensions) > 0 || len(analysis.AnchorStyle.ReusableRules) > 0 {
		if err := s.hotReloadLiveAgentPlanRooms(r.Context(), tenantID, planID, "style"); err != nil {
			s.finishAISingleUse(r.Context(), invocationID, "failed", provider, modelName, latencyMS, map[string]any{"error": err.Error()})
			writeError(w, http.StatusBadGateway, "主播风格已保存，但热同步到直播间失败："+err.Error())
			return
		}
	}
	s.finishAISingleUse(r.Context(), invocationID, "succeeded", provider, modelName, latencyMS, map[string]any{
		"fact_count": len(analysis.Facts), "rhythm_node_count": len(analysis.RhythmNodes),
	})
	writeJSON(w, http.StatusOK, updated)
}
