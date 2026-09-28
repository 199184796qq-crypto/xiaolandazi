package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

var (
	fullShowCommercialNumberPattern = regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?\s*(?:元|块|斤|两|公斤|千克|克|升|毫升|桶|件|盒|袋|折|%|年|万|分|kg|g|ml|l)`)
	fullShowLinkPattern             = regexp.MustCompile(`([0-9]{1,2})\s*号\s*链接`)
	fullShowInventoryPattern        = regexp.MustCompile(`(?:库存|还剩|剩下|最后)[^。！？\n]{0,24}[0-9]+(?:\.[0-9]+)?\s*(?:桶|件|盒|袋|单|份)`)
)

func normalizeFullShowPreviewInput(input *model.LiveAgentFullShowPreviewInput) error {
	if input.DurationMinutes == 0 {
		input.DurationMinutes = 90
	}
	if input.RoundMinutes == 0 {
		input.RoundMinutes = 7
	}
	if input.VariantCount == 0 {
		input.VariantCount = 5
	}
	if input.DurationMinutes < 30 || input.DurationMinutes > 240 {
		return errors.New("直播时长只允许 30 到 240 分钟")
	}
	if input.RoundMinutes < 5 || input.RoundMinutes > 12 {
		return errors.New("单轮目标时长只允许 5 到 12 分钟")
	}
	if input.VariantCount < 3 || input.VariantCount > 5 {
		return errors.New("首批变化稿只允许 3 到 5 套")
	}
	if len(input.ProductLinks) > 20 {
		return errors.New("商品链接草稿最多 20 个")
	}
	if len(input.RhythmNodes) > 40 {
		return errors.New("直播节奏节点最多 40 个")
	}
	if len(input.AnchorStyle.Dimensions) > 40 {
		return errors.New("主播风格维度最多 40 个")
	}
	if len(input.RecentTexts) > 5 {
		input.RecentTexts = input.RecentTexts[:5]
	}
	for index, text := range input.RecentTexts {
		text = strings.TrimSpace(text)
		if utf8.RuneCountInString(text) > 1200 {
			text = string([]rune(text)[:1200])
		}
		input.RecentTexts[index] = text
	}
	return nil
}

func compileFullShowContext(
	plan model.LiveAgentPlan,
	facts []model.LiveAgentPlanFact,
	benefits []model.LiveAgentPlanBenefit,
	productLinks []model.LiveAgentPlanProductLink,
	scriptReferences []model.LiveAgentPlanScriptReference,
	input model.LiveAgentFullShowPreviewInput,
) model.LiveAgentFullShowGenerationContext {
	formalFacts := make([]model.LiveAgentFullShowContextFact, 0, len(facts))
	for _, fact := range facts {
		if strings.ToLower(strings.TrimSpace(fact.Status)) != "active" {
			continue
		}
		key := strings.TrimSpace(fact.Key)
		value := strings.TrimSpace(fact.Value)
		if key == "" || value == "" {
			continue
		}
		formalFacts = append(formalFacts, model.LiveAgentFullShowContextFact{
			Category: strings.TrimSpace(fact.Category),
			Key:      key,
			Value:    value,
			Version:  fact.VersionNo,
		})
	}

	formalScriptReferences := make([]model.LiveAgentFullShowContextScriptReference, 0, len(scriptReferences))
	for _, item := range scriptReferences {
		if strings.ToLower(strings.TrimSpace(item.Status)) != "active" {
			continue
		}
		content := strings.TrimSpace(item.ContentText)
		if content == "" {
			continue
		}
		mode := strings.ToLower(strings.TrimSpace(item.ExecutionMode))
		if mode != "verbatim" {
			mode = "intent"
		}
		formalScriptReferences = append(formalScriptReferences, model.LiveAgentFullShowContextScriptReference{
			ReferenceKey:  strings.TrimSpace(item.ReferenceKey),
			Title:         strings.TrimSpace(item.Title),
			ContentText:   content,
			Goal:          strings.TrimSpace(item.Goal),
			Transition:    strings.TrimSpace(item.Transition),
			ExecutionMode: mode,
			Version:       item.VersionNo,
		})
	}

	rhythmNodes := make([]model.LiveAgentPlanRhythmNode, 0, len(input.RhythmNodes))
	for _, item := range input.RhythmNodes {
		item.FixedText = ""
		if strings.EqualFold(strings.TrimSpace(item.ExecutionMode), "verbatim") {
			item.ExecutionMode = "intent"
		}
		rhythmNodes = append(rhythmNodes, item)
	}

	style := model.LiveAgentPlanAnchorStyleProfile{
		Dimensions:        []model.LiveAgentPlanAnchorStyleDimension{},
		ReusableRules:     []string{},
		CandidatePatterns: []string{},
		ExcludedFromStyle: []string{},
	}
	if input.UseAnchorStyle {
		style.Summary = strings.TrimSpace(input.AnchorStyle.Summary)
		for _, item := range input.AnchorStyle.Dimensions {
			if strings.TrimSpace(item.Rule) == "" || strings.TrimSpace(item.Level) == "样本不足" {
				continue
			}
			item.EvidenceQuotes = nil
			style.Dimensions = append(style.Dimensions, item)
		}
		style.ReusableRules = normalizeStyleStringList(input.AnchorStyle.ReusableRules, 12, 160)
	}

	roundCount := (input.DurationMinutes + input.RoundMinutes - 1) / input.RoundMinutes
	return model.LiveAgentFullShowGenerationContext{
		PlanID:             plan.ID,
		PlanName:           strings.TrimSpace(plan.Name),
		PlanDescription:    strings.TrimSpace(plan.Description),
		RoomID:             input.RoomID,
		FormalFacts:        formalFacts,
		Benefits:           benefits,
		ProductLinks:       productLinks,
		ScriptReferences:   formalScriptReferences,
		RhythmNodes:        rhythmNodes,
		AnchorStyle:        style,
		DurationMinutes:    input.DurationMinutes,
		RoundMinutes:       input.RoundMinutes,
		RoundCount:         roundCount,
		VariantCount:       input.VariantCount,
		UseAnchorStyle:     input.UseAnchorStyle,
		UseDynamicFacts:    input.UseDynamicFacts,
		GenerateTTSHints:   input.GenerateTTSHints,
		AvoidRecent:        input.AvoidRecent,
		DraftProductSource: false,
		DraftRhythmSource:  len(rhythmNodes) > 0,
		DraftStyleSource:   len(style.Dimensions) > 0 || len(style.ReusableRules) > 0,
	}
}

func generateFullShowVariants(
	ctx context.Context,
	generationContext model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	recentTexts []string,
) ([]model.LiveAgentFullShowVariant, string, string, int64, error) {
	contextJSON, err := json.Marshal(generationContext)
	if err != nil {
		return nil, "", "", 0, err
	}
	recentJSON, _ := json.Marshal(recentTexts)
	targetChars := generationContext.RoundMinutes * 250
	prompt := fmt.Sprintf(`你是直播电商“整场主线话术生成器”。

【规则层与行业层硬约束】
%s

【最重要的事实边界】
1. formal_facts 是唯一正式事实来源。不得修改其中的价格、规格、产地、履约、身份、承诺等事实。
2. benefits 是当前方案已经正式采纳且此刻有效的活动福利。活动价、赠品、满减、限时权益只能使用 benefits 中已有内容。
3. product_links 是当前方案已经正式采纳的商品链接，只允许使用里面已有的商品、规格、日常价、数量和适用信息，不得补全或猜测。
4. script_references 是当前方案正式话术参考，只控制“怎么组织、怎么说”，绝不是事实来源。里面出现但未被 formal_facts、benefits 或 product_links 支持的事实、数字、承诺都不能使用。
5. script_references.execution_mode=verbatim 时，如果决定使用该参考，content_text 必须逐字保持，不得改写；但若其中存在未被正式事实支持的内容，则不要使用这条参考。execution_mode=intent 时只学习表达意图、结构和转场，可以自然改写。
6. rhythm_nodes 和 anchor_style 是本次页面草稿，也只用于“怎么组织、怎么说”，不能产生新的商品事实。
7. 任何没有出现在 formal_facts、benefits 或 product_links 里的具体数字、价格、规格、功效、快递、库存、销量、粉丝量、资质、承诺，都不能生成。
8. 如果 use_dynamic_facts=true，库存、实时在线、当前剩余量等必须保留成自然的运行时插槽，例如“库存我看一下后台实时数量再告诉大家”，绝对不能编具体数字。
9. 所有带数字的价格、规格、数量、年限、评分等必须保持来源里的数字写法，不要把阿拉伯数字改写成中文数字，也不要自行换算。

【生成目标】
一次生成 %d 套完整轮次口播，每套目标约 %d 分钟、约 %d 个中文字符。每套都必须是可以直接连续播的完整口语，不是提纲，不要输出舞台说明或内部系统术语。
A/B/C/D/E 的事实完全一致，但开场角度、事实顺序、句式、生活场景、转场、强调位置、CTA 位置要明显不同，不能只是同义词替换。
同一套稿内部要自然连贯，不要每几十秒机械切阶段；把“留人、讲品、价值、链接、CTA”融成一个连续产品故事。
主播风格只控制表达方式；不要把 anchor_style 里的证据句原样复制。

【TTS】
如果 generate_tts_hints=true，为每套稿给 3 到 6 条分段语气建议。rate 建议范围 0.95 到 1.15；直播整体不要慢，不要一直高亢。instruction 只写声音表现，不写商品事实。

【近期去重】
avoid_recent=%t。若为 true，请避开近期文本里已经用过的开场和高重复表达。

严格返回 JSON：
{
  "variants": [
    {
      "index": 1,
      "variant_key": "A",
      "title": "本轮简短标题",
      "opening_angle": "新人留人/生活场景/用户疑问/产品价值/老客信任等",
      "text": "完整可播口语全文",
      "covered_fact_keys": ["实际使用到的 formal_facts.key"],
      "covered_link_keys": ["实际使用到的 link_key"],
      "tts_hints": [{"segment":"开场","instruction":"轻快自然、像和直播间聊天","rate":1.08}]
    }
  ]
}

【生成上下文】
%s

【近期文本】
%s`, strings.TrimSpace(policyPrompt), generationContext.VariantCount, generationContext.RoundMinutes, targetChars, generationContext.AvoidRecent, string(contextJSON), string(recentJSON))

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只生成直播整场主线预览稿。严格遵守已提供事实，不编造，不执行真实业务动作。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      15000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        120 * time.Second,
	})
	if err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, err
	}
	var output struct {
		Variants []model.LiveAgentFullShowVariant `json:"variants"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode full show generation: %w", err)
	}
	if len(output.Variants) < generationContext.VariantCount {
		return nil, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("full show generation returned %d variants, want %d", len(output.Variants), generationContext.VariantCount)
	}
	if len(output.Variants) > generationContext.VariantCount {
		output.Variants = output.Variants[:generationContext.VariantCount]
	}
	keys := []string{"A", "B", "C", "D", "E"}
	for index := range output.Variants {
		item := &output.Variants[index]
		item.Index = index + 1
		item.VariantKey = keys[index]
		item.Text = strings.TrimSpace(item.Text)
		item.Title = strings.TrimSpace(item.Title)
		item.OpeningAngle = strings.TrimSpace(item.OpeningAngle)
		if item.Title == "" {
			item.Title = item.VariantKey + "稿"
		}
		if item.OpeningAngle == "" {
			item.OpeningAngle = "变化开场"
		}
		if !generationContext.GenerateTTSHints {
			item.TTSHints = nil
		} else {
			for hintIndex := range item.TTSHints {
				if item.TTSHints[hintIndex].Rate < 0.95 || item.TTSHints[hintIndex].Rate > 1.15 {
					item.TTSHints[hintIndex].Rate = 1.08
				}
			}
		}
	}
	return output.Variants, result.Provider, result.Model, result.LatencyMS, nil
}

func generateSingleFullShowVariant(
	ctx context.Context,
	generationContext model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	targetKey string,
	currentVariants []model.LiveAgentFullShowVariant,
	recentTexts []string,
) (model.LiveAgentFullShowVariant, string, string, int64, error) {
	contextJSON, err := json.Marshal(generationContext)
	if err != nil {
		return model.LiveAgentFullShowVariant{}, "", "", 0, err
	}
	currentJSON, _ := json.Marshal(currentVariants)
	recentJSON, _ := json.Marshal(recentTexts)
	targetChars := generationContext.RoundMinutes * 250
	prompt := fmt.Sprintf(`你是直播电商“单稿重生成器”。

【规则层与行业层硬约束】
%s

【任务】
只重新生成 %s 稿，绝对不要生成或修改其它稿。
新稿仍然是完整可连续播放的直播主线口语，目标约 %d 分钟、约 %d 个中文字符。
需要沿用当前方案的正式事实、活动、商品链接、话术参考、节奏和主播风格，但表达、开场角度、事实顺序、生活场景、转场和 CTA 位置允许重新组织。
必须明显避开当前其它稿件的高相似表达，也不要只是对旧 %s 稿做同义词替换。

【事实边界】
1. formal_facts、benefits、product_links 是唯一可以作为事实的来源。
2. script_references、rhythm_nodes、anchor_style 只控制表达方式，不得产生新事实。
3. 不得编造价格、规格、产地、履约、库存、销量、粉丝、资质、功效或承诺。
4. use_dynamic_facts=true 时，实时库存等只能保留为运行时自然插槽，不得写死数字。
5. 所有来源中的数字写法保持原样，不自行换算。

【当前全部稿件】
%s

【生成上下文】
%s

【近期文本】
%s

严格只返回 JSON：
{
  "variant": {
    "variant_key": "%s",
    "title": "本稿简短标题",
    "opening_angle": "本稿新的切入角度",
    "text": "完整可播口语全文",
    "covered_fact_keys": ["实际使用的正式事实 key"],
    "covered_link_keys": ["实际使用的 link_key"],
    "tts_hints": [{"segment":"开场","instruction":"轻快自然、像和直播间聊天","rate":1.08}]
  }
}`,
		strings.TrimSpace(policyPrompt),
		targetKey,
		generationContext.RoundMinutes,
		targetChars,
		targetKey,
		string(currentJSON),
		string(contextJSON),
		string(recentJSON),
		targetKey,
	)
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只重新生成用户指定的一套直播主线稿。严格遵守事实边界，不修改其它稿件。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      7000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        90 * time.Second,
	})
	if err != nil {
		return model.LiveAgentFullShowVariant{}, result.Provider, result.Model, result.LatencyMS, err
	}
	var output struct {
		Variant model.LiveAgentFullShowVariant `json:"variant"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return model.LiveAgentFullShowVariant{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode single full show generation: %w", err)
	}
	item := output.Variant
	item.VariantKey = targetKey
	item.Text = strings.TrimSpace(item.Text)
	item.Title = strings.TrimSpace(item.Title)
	item.OpeningAngle = strings.TrimSpace(item.OpeningAngle)
	if item.Text == "" {
		return model.LiveAgentFullShowVariant{}, result.Provider, result.Model, result.LatencyMS, errors.New("single full show generation returned empty text")
	}
	if item.Title == "" {
		item.Title = targetKey + "稿"
	}
	if item.OpeningAngle == "" {
		item.OpeningAngle = "重新生成"
	}
	item.EstimatedMinutes = generationContext.RoundMinutes
	if !generationContext.GenerateTTSHints {
		item.TTSHints = nil
	} else {
		for index := range item.TTSHints {
			if item.TTSHints[index].Rate < 0.95 || item.TTSHints[index].Rate > 1.15 {
				item.TTSHints[index].Rate = 1.08
			}
		}
	}
	return item, result.Provider, result.Model, result.LatencyMS, nil
}

func normalizeCommercialToken(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
}

func fullShowAllowedCommercialTokens(context model.LiveAgentFullShowGenerationContext) map[string]struct{} {
	allowed := map[string]struct{}{}
	add := func(text string) {
		for _, token := range fullShowCommercialNumberPattern.FindAllString(text, -1) {
			allowed[normalizeCommercialToken(token)] = struct{}{}
		}
	}
	for _, fact := range context.FormalFacts {
		add(fact.Value)
	}
	for _, link := range context.ProductLinks {
		add(link.Spec)
		add(link.DailyPrice)
		add(link.Quantity)
	}
	for _, benefit := range context.Benefits {
		add(benefit.ActivityPrice)
		add(benefit.Gift)
		add(benefit.Activity)
	}
	return allowed
}

func fullShowRuneShingles(text string, size int) map[string]struct{} {
	runes := []rune(strings.Join(strings.Fields(strings.TrimSpace(text)), ""))
	result := map[string]struct{}{}
	if len(runes) == 0 {
		return result
	}
	if len(runes) < size {
		result[string(runes)] = struct{}{}
		return result
	}
	for index := 0; index+size <= len(runes); index++ {
		result[string(runes[index:index+size])] = struct{}{}
	}
	return result
}

func fullShowSimilarityPercent(left, right string) int {
	a := fullShowRuneShingles(left, 5)
	b := fullShowRuneShingles(right, 5)
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	union := map[string]struct{}{}
	for item := range a {
		union[item] = struct{}{}
		if _, ok := b[item]; ok {
			intersection++
		}
	}
	for item := range b {
		union[item] = struct{}{}
	}
	return intersection * 100 / len(union)
}

func auditFullShowVariants(
	generationContext model.LiveAgentFullShowGenerationContext,
	variants []model.LiveAgentFullShowVariant,
	recentTexts []string,
) []model.LiveAgentFullShowVariant {
	allowedCommercial := fullShowAllowedCommercialTokens(generationContext)
	allowedLinks := map[string]struct{}{}
	for _, link := range generationContext.ProductLinks {
		allowedLinks[canonicalPlanProductLinkKey(link.LinkKey)] = struct{}{}
	}
	for index := range variants {
		item := &variants[index]
		issues := []model.LiveAgentFullShowAuditIssue{}
		addIssue := func(severity, code, message string) {
			issues = append(issues, model.LiveAgentFullShowAuditIssue{Severity: severity, Code: code, Message: message})
		}
		if item.Text == "" {
			addIssue("error", "empty_text", "该变化稿没有生成可播全文")
		}
		for _, token := range fullShowCommercialNumberPattern.FindAllString(item.Text, -1) {
			if _, ok := allowedCommercial[normalizeCommercialToken(token)]; !ok {
				addIssue("error", "unknown_numeric_claim", "检测到未在当前事实中出现的具体商业数字："+strings.TrimSpace(token))
			}
		}
		for _, match := range fullShowLinkPattern.FindAllStringSubmatch(item.Text, -1) {
			if len(match) < 2 {
				continue
			}
			key := canonicalPlanProductLinkKey(match[1] + "号链接")
			if _, ok := allowedLinks[key]; !ok {
				addIssue("error", "unknown_link", "检测到当前商品链接草稿里不存在的链接："+key)
			}
		}
		if generationContext.UseDynamicFacts && fullShowInventoryPattern.MatchString(item.Text) {
			addIssue("error", "hardcoded_inventory", "库存属于运行时动态事实，整场主线不能写死具体剩余数量")
		}

		factCovered := 0
		for _, fact := range generationContext.FormalFacts {
			if strings.Contains(item.Text, fact.Value) {
				factCovered++
			}
		}
		linkCovered := 0
		for key := range allowedLinks {
			if key != "" && strings.Contains(item.Text, key) {
				linkCovered++
			}
		}
		factPct := 100
		if len(generationContext.FormalFacts) > 0 {
			factPct = factCovered * 100 / len(generationContext.FormalFacts)
		}
		linkPct := 100
		if len(allowedLinks) > 0 {
			linkPct = linkCovered * 100 / len(allowedLinks)
		}
		maxSimilarity := 0
		for previous := 0; previous < index; previous++ {
			if score := fullShowSimilarityPercent(item.Text, variants[previous].Text); score > maxSimilarity {
				maxSimilarity = score
			}
		}
		if generationContext.AvoidRecent {
			for _, recent := range recentTexts {
				if score := fullShowSimilarityPercent(item.Text, recent); score > maxSimilarity {
					maxSimilarity = score
				}
			}
		}
		if maxSimilarity >= 72 {
			addIssue("error", "too_similar", fmt.Sprintf("与其它变化稿或近期文本相似度 %d%%，需要重写", maxSimilarity))
		} else if maxSimilarity >= 58 {
			addIssue("warning", "similarity_warning", fmt.Sprintf("与其它变化稿或近期文本相似度 %d%%，变化幅度偏小", maxSimilarity))
		}
		runeCount := utf8.RuneCountInString(item.Text)
		item.EstimatedMinutes = (runeCount + 259) / 260
		if item.EstimatedMinutes < generationContext.RoundMinutes-2 {
			addIssue("warning", "round_too_short", fmt.Sprintf("预计只有约 %d 分钟，低于本轮 %d 分钟目标", item.EstimatedMinutes, generationContext.RoundMinutes))
		}
		passed := true
		for _, issue := range issues {
			if issue.Severity == "error" {
				passed = false
				break
			}
		}
		item.Audit = model.LiveAgentFullShowAudit{
			Passed:          passed,
			Issues:          issues,
			FactCoveragePct: factPct,
			LinkCoveragePct: linkPct,
			SimilarityPct:   maxSimilarity,
		}
	}
	return variants
}

func repairFailedFullShowVariants(
	ctx context.Context,
	generationContext model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	variants []model.LiveAgentFullShowVariant,
	recentTexts []string,
) ([]model.LiveAgentFullShowVariant, string, string, int64, error) {
	type failedVariant struct {
		VariantKey   string                              `json:"variant_key"`
		Title        string                              `json:"title"`
		OpeningAngle string                              `json:"opening_angle"`
		Text         string                              `json:"text"`
		Issues       []model.LiveAgentFullShowAuditIssue `json:"issues"`
	}
	failed := make([]failedVariant, 0)
	for _, item := range variants {
		if item.Audit.Passed {
			continue
		}
		failed = append(failed, failedVariant{
			VariantKey:   item.VariantKey,
			Title:        item.Title,
			OpeningAngle: item.OpeningAngle,
			Text:         item.Text,
			Issues:       item.Audit.Issues,
		})
	}
	if len(failed) == 0 {
		return nil, "", "", 0, nil
	}
	contextJSON, err := json.Marshal(generationContext)
	if err != nil {
		return nil, "", "", 0, err
	}
	failedJSON, err := json.Marshal(failed)
	if err != nil {
		return nil, "", "", 0, err
	}
	recentJSON, _ := json.Marshal(recentTexts)
	targetChars := generationContext.RoundMinutes * 250
	prompt := fmt.Sprintf(`你是直播整场话术的“失败稿修复器”。第一次生成已经经过程序审计，下面只修复审计失败的变化稿，不要改动没有失败的稿。

【规则层与行业层硬约束】
%s

【修复硬规则】
1. formal_facts 是正式事实来源；benefits 是当前有效活动福利；product_links 仅允许使用已提供字段，不得补全或猜测。
2. script_references 只提供表达参考，不提供新事实。execution_mode=verbatim 时如果继续使用该参考，content_text 必须逐字保持；若内容与正式事实边界冲突，就停止使用该参考，不得借它放行未核实事实。
3. 每一条 error 审计问题都必须在新稿中消除，尤其是：新增数字、未知链接、写死库存、高相似。
4. 价格、规格、数量、年限、评分等数字保持来源里的阿拉伯数字写法，不改成中文数字、不自行换算。
5. use_dynamic_facts=true 时，库存只能说“我看一下后台实时数量”等自然占位表达，绝不写具体剩余数。
6. 如果问题是 too_similar，必须重做开场角度、事实顺序、生活场景、句式、转场和 CTA 位置，不能只换同义词。
7. 每个修复稿仍应是完整可连续播放的口语，目标约 %d 分钟、约 %d 个中文字符。
8. 只返回下方失败稿对应的 variant_key，不得新增其它变化稿。
9. 如果 generate_tts_hints=true，保留 3 到 6 条仅描述声音表现的 TTS 建议；rate 范围 0.95 到 1.15。

严格返回 JSON：
{
  "variants": [
    {
      "variant_key": "A",
      "title": "修复后的标题",
      "opening_angle": "修复后的切入角度",
      "text": "完整修复稿",
      "covered_fact_keys": ["实际使用的正式事实 key"],
      "covered_link_keys": ["实际使用的 link_key"],
      "tts_hints": [{"segment":"开场","instruction":"轻快自然，不喊","rate":1.08}]
    }
  ]
}

【生成上下文】
%s

【失败稿及程序审计问题】
%s

【近期文本】
%s`, strings.TrimSpace(policyPrompt), generationContext.RoundMinutes, targetChars, string(contextJSON), string(failedJSON), string(recentJSON))

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只修复程序审计失败的直播主线稿。不得新增事实，不得执行真实业务动作。"},
			{Role: "user", Content: prompt},
		},
		MaxTokens:      10000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        60 * time.Second,
	})
	if err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, err
	}
	var output struct {
		Variants []model.LiveAgentFullShowVariant `json:"variants"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return nil, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode full show repair: %w", err)
	}
	failedKeys := make(map[string]struct{}, len(failed))
	for _, item := range failed {
		failedKeys[strings.ToUpper(strings.TrimSpace(item.VariantKey))] = struct{}{}
	}
	repaired := make([]model.LiveAgentFullShowVariant, 0, len(failed))
	seen := map[string]struct{}{}
	for _, item := range output.Variants {
		key := strings.ToUpper(strings.TrimSpace(item.VariantKey))
		if _, ok := failedKeys[key]; !ok || key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		item.VariantKey = key
		item.Text = strings.TrimSpace(item.Text)
		item.Title = strings.TrimSpace(item.Title)
		item.OpeningAngle = strings.TrimSpace(item.OpeningAngle)
		if item.Title == "" {
			item.Title = key + "稿"
		}
		if item.OpeningAngle == "" {
			item.OpeningAngle = "变化开场"
		}
		if !generationContext.GenerateTTSHints {
			item.TTSHints = nil
		} else {
			for hintIndex := range item.TTSHints {
				if item.TTSHints[hintIndex].Rate < 0.95 || item.TTSHints[hintIndex].Rate > 1.15 {
					item.TTSHints[hintIndex].Rate = 1.08
				}
			}
		}
		repaired = append(repaired, item)
	}
	if len(repaired) == 0 {
		return nil, result.Provider, result.Model, result.LatencyMS, errors.New("full show repair returned no matching variants")
	}
	return repaired, result.Provider, result.Model, result.LatencyMS, nil
}

func mergeRepairedFullShowVariants(
	original []model.LiveAgentFullShowVariant,
	repaired []model.LiveAgentFullShowVariant,
) []model.LiveAgentFullShowVariant {
	byKey := make(map[string]model.LiveAgentFullShowVariant, len(repaired))
	for _, item := range repaired {
		key := strings.ToUpper(strings.TrimSpace(item.VariantKey))
		if key != "" {
			byKey[key] = item
		}
	}
	result := append([]model.LiveAgentFullShowVariant(nil), original...)
	for index := range result {
		key := strings.ToUpper(strings.TrimSpace(result[index].VariantKey))
		item, ok := byKey[key]
		if !ok {
			continue
		}
		item.Index = result[index].Index
		item.VariantKey = result[index].VariantKey
		result[index] = item
	}
	return result
}

type liveAgentFullShowAuditPreviewInput struct {
	TenantID        int64                            `json:"tenant_id,omitempty"`
	RoomID          int64                            `json:"room_id,omitempty"`
	RoundMinutes    int                              `json:"round_minutes"`
	UseDynamicFacts bool                             `json:"use_dynamic_facts"`
	AvoidRecent     bool                             `json:"avoid_recent"`
	Variants        []model.LiveAgentFullShowVariant `json:"variants"`
	RecentTexts     []string                         `json:"recent_texts,omitempty"`
}

type liveAgentFullShowRegenerateInput struct {
	TenantID         int64                                     `json:"tenant_id,omitempty"`
	RoomID           int64                                     `json:"room_id,omitempty"`
	DurationMinutes  int                                       `json:"duration_minutes"`
	RoundMinutes     int                                       `json:"round_minutes"`
	UseAnchorStyle   bool                                      `json:"use_anchor_style"`
	UseDynamicFacts  bool                                      `json:"use_dynamic_facts"`
	GenerateTTSHints bool                                      `json:"generate_tts_hints"`
	AvoidRecent      bool                                      `json:"avoid_recent"`
	ProductLinks     []model.LiveAgentPlanProductLinkCandidate `json:"product_links"`
	RhythmNodes      []model.LiveAgentPlanRhythmNode           `json:"rhythm_nodes"`
	AnchorStyle      model.LiveAgentPlanAnchorStyleProfile     `json:"anchor_style"`
	Variants         []model.LiveAgentFullShowVariant          `json:"variants"`
	RecentTexts      []string                                  `json:"recent_texts,omitempty"`
}

func (s *Server) liveAgentPlanFullShowRegeneratePreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	targetKey := strings.ToUpper(strings.TrimSpace(r.PathValue("variantKey")))
	if targetKey != "A" && targetKey != "B" && targetKey != "C" && targetKey != "D" && targetKey != "E" {
		writeError(w, http.StatusBadRequest, "稿件编号无效")
		return
	}
	var input liveAgentFullShowRegenerateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "单稿重新生成参数格式错误")
		return
	}
	if len(input.Variants) < 3 || len(input.Variants) > 5 {
		writeError(w, http.StatusBadRequest, "当前稿件集合必须为 3 到 5 套")
		return
	}
	previewInput := model.LiveAgentFullShowPreviewInput{
		TenantID:         input.TenantID,
		RoomID:           input.RoomID,
		DurationMinutes:  input.DurationMinutes,
		RoundMinutes:     input.RoundMinutes,
		VariantCount:     len(input.Variants),
		UseAnchorStyle:   input.UseAnchorStyle,
		UseDynamicFacts:  input.UseDynamicFacts,
		GenerateTTSHints: input.GenerateTTSHints,
		AvoidRecent:      input.AvoidRecent,
		ProductLinks:     input.ProductLinks,
		RhythmNodes:      input.RhythmNodes,
		AnchorStyle:      input.AnchorStyle,
		RecentTexts:      input.RecentTexts,
	}
	if err := normalizeFullShowPreviewInput(&previewInput); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	input.RecentTexts = previewInput.RecentTexts
	targetIndex := -1
	for index := range input.Variants {
		input.Variants[index].VariantKey = strings.ToUpper(strings.TrimSpace(input.Variants[index].VariantKey))
		if input.Variants[index].VariantKey == targetKey {
			targetIndex = index
		}
	}
	if targetIndex < 0 {
		writeError(w, http.StatusBadRequest, targetKey+"稿不在当前稿件集合中")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	boundToRoom := false
	for _, roomID := range plan.RoomIDs {
		if roomID == input.RoomID {
			boundToRoom = true
			break
		}
	}
	if !boundToRoom {
		writeError(w, http.StatusBadRequest, "当前直播方案没有绑定到所选直播间")
		return
	}
	facts, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式事实依据失败")
		return
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(r.Context(), tenantID, planID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前有效活动福利失败")
		return
	}
	productLinks, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
		return
	}
	scriptReferences, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式话术参考失败")
		return
	}
	industryCode, l1, l2, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, input.RoomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取规则层与行业层失败")
		return
	}
	effectivePolicy := policy.BuildEffective(industryCode, l1, l2, nil)
	generationContext := compileFullShowContext(plan, facts, benefits, productLinks, scriptReferences, previewInput)
	generationContext.IndustryCode = strings.TrimSpace(industryCode)
	generationContext.PolicyRuleCount = len(effectivePolicy.Rules)
	if len(generationContext.FormalFacts) == 0 && len(generationContext.Benefits) == 0 && len(generationContext.ProductLinks) == 0 {
		writeError(w, http.StatusBadRequest, "当前方案还没有可用于生成的正式事实、有效活动福利或正式商品链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "live_full_show_single_regenerate", map[string]any{
		"plan_id": planID, "variant_key": targetKey, "round_minutes": input.RoundMinutes,
	})
	generated, provider, modelName, latencyMS, err := generateSingleFullShowVariant(
		ctx, generationContext, effectivePolicy.PromptText, targetKey, input.Variants, input.RecentTexts,
	)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", provider, modelName, latencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, targetKey+"稿重新生成失败，请稍后重试")
		return
	}
	generated.Index = input.Variants[targetIndex].Index
	merged := append([]model.LiveAgentFullShowVariant(nil), input.Variants...)
	merged[targetIndex] = generated
	merged = auditFullShowVariants(generationContext, merged, input.RecentTexts)
	updated := merged[targetIndex]
	s.finishAISingleUse(r.Context(), invocationID, "succeeded", provider, modelName, latencyMS, map[string]any{
		"variant_key": targetKey, "audit_passed": updated.Audit.Passed,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"variant":    updated,
		"variants":   merged,
		"provider":   provider,
		"model":      modelName,
		"latency_ms": latencyMS,
	})
}

func (s *Server) liveAgentPlanFullShowAuditPreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input liveAgentFullShowAuditPreviewInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "文稿复核参数格式错误")
		return
	}
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择当前直播间后再复核文稿")
		return
	}
	if input.RoundMinutes <= 0 {
		input.RoundMinutes = 7
	}
	if input.RoundMinutes < 5 || input.RoundMinutes > 12 {
		writeError(w, http.StatusBadRequest, "单轮目标时长只允许 5 到 12 分钟")
		return
	}
	if len(input.Variants) == 0 || len(input.Variants) > 5 {
		writeError(w, http.StatusBadRequest, "请提交 1 到 5 套待复核文稿")
		return
	}
	if len(input.RecentTexts) > 5 {
		input.RecentTexts = input.RecentTexts[:5]
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	boundToRoom := false
	for _, roomID := range plan.RoomIDs {
		if roomID == input.RoomID {
			boundToRoom = true
			break
		}
	}
	if !boundToRoom {
		writeError(w, http.StatusBadRequest, "当前直播方案没有绑定到所选直播间")
		return
	}
	facts, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式事实依据失败")
		return
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(r.Context(), tenantID, planID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前有效活动福利失败")
		return
	}
	productLinks, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
		return
	}
	context := compileFullShowContext(
		plan,
		facts,
		benefits,
		productLinks,
		nil,
		model.LiveAgentFullShowPreviewInput{
			RoomID:          input.RoomID,
			RoundMinutes:    input.RoundMinutes,
			DurationMinutes: input.RoundMinutes,
			VariantCount:    len(input.Variants),
			UseDynamicFacts: input.UseDynamicFacts,
			AvoidRecent:     input.AvoidRecent,
		},
	)
	variants := append([]model.LiveAgentFullShowVariant(nil), input.Variants...)
	for index := range variants {
		variants[index].Text = strings.TrimSpace(variants[index].Text)
	}
	variants = auditFullShowVariants(context, variants, input.RecentTexts)
	writeJSON(w, http.StatusOK, map[string]any{
		"variants": variants,
	})
}

func (s *Server) liveAgentPlanFullShowPreview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input model.LiveAgentFullShowPreviewInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "整场话术生成参数格式错误")
		return
	}
	if err := normalizeFullShowPreviewInput(&input); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	plan, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播智能体方案不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播智能体方案失败")
		return
	}
	if input.RoomID <= 0 {
		writeError(w, http.StatusBadRequest, "请选择当前直播间后再生成整场话术")
		return
	}
	boundToRoom := false
	for _, roomID := range plan.RoomIDs {
		if roomID == input.RoomID {
			boundToRoom = true
			break
		}
	}
	if !boundToRoom {
		writeError(w, http.StatusBadRequest, "当前直播方案没有绑定到所选直播间")
		return
	}
	facts, err := s.store.ListLiveAgentPlanFacts(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式事实依据失败")
		return
	}
	benefits, err := s.store.ListActiveLiveAgentPlanBenefits(r.Context(), tenantID, planID, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取当前有效活动福利失败")
		return
	}
	productLinks, err := s.store.ListLiveAgentPlanProductLinks(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式商品链接失败")
		return
	}
	scriptReferences, err := s.store.ListLiveAgentPlanScriptReferences(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取正式话术参考失败")
		return
	}
	industryCode, l1, l2, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, input.RoomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取规则层与行业层失败")
		return
	}
	effectivePolicy := policy.BuildEffective(industryCode, l1, l2, nil)
	generationContext := compileFullShowContext(plan, facts, benefits, productLinks, scriptReferences, input)
	generationContext.IndustryCode = strings.TrimSpace(industryCode)
	generationContext.PolicyRuleCount = len(effectivePolicy.Rules)
	if len(generationContext.FormalFacts) == 0 && len(generationContext.Benefits) == 0 && len(generationContext.ProductLinks) == 0 {
		writeError(w, http.StatusBadRequest, "当前方案还没有可用于生成的正式事实、有效活动福利或正式商品链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 195*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "live_full_show_preview", map[string]any{
		"plan_id": planID, "variant_count": input.VariantCount, "round_minutes": input.RoundMinutes,
	})
	variants, provider, modelName, latencyMS, err := generateFullShowVariants(ctx, generationContext, effectivePolicy.PromptText, input.RecentTexts)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", provider, modelName, latencyMS, map[string]any{"error": err.Error()})
		log.Printf("full show preview failed tenant=%d plan=%d provider=%s model=%s latency_ms=%d err=%v", tenantID, planID, provider, modelName, latencyMS, err)
		writeError(w, http.StatusBadGateway, "整场话术暂时生成失败，请稍后重试")
		return
	}
	variants = auditFullShowVariants(generationContext, variants, input.RecentTexts)
	failedBeforeRepair := 0
	for _, item := range variants {
		if !item.Audit.Passed {
			failedBeforeRepair++
		}
	}
	repairAttempted := false
	repairSucceeded := false
	if failedBeforeRepair > 0 {
		repairAttempted = true
		repaired, repairProvider, repairModel, repairLatency, repairErr := repairFailedFullShowVariants(
			ctx,
			generationContext,
			effectivePolicy.PromptText,
			variants,
			input.RecentTexts,
		)
		latencyMS += repairLatency
		if provider == "" {
			provider = repairProvider
		}
		if modelName == "" {
			modelName = repairModel
		}
		if repairErr != nil {
			log.Printf(
				"full show repair failed tenant=%d plan=%d failed=%d latency_ms=%d err=%v",
				tenantID, planID, failedBeforeRepair, repairLatency, repairErr,
			)
		} else {
			variants = mergeRepairedFullShowVariants(variants, repaired)
			variants = auditFullShowVariants(generationContext, variants, input.RecentTexts)
			repairSucceeded = true
		}
	}
	passed := 0
	for _, item := range variants {
		if item.Audit.Passed {
			passed++
		}
	}
	s.finishAISingleUse(r.Context(), invocationID, "succeeded", provider, modelName, latencyMS, map[string]any{
		"variant_count":              len(variants),
		"audit_passed":               passed,
		"audit_failed_before_repair": failedBeforeRepair,
		"repair_attempted":           repairAttempted,
		"repair_succeeded":           repairSucceeded,
	})
	writeJSON(w, http.StatusOK, model.LiveAgentFullShowPreviewResponse{
		Context:   generationContext,
		Variants:  variants,
		Provider:  provider,
		Model:     modelName,
		LatencyMS: latencyMS,
		Persisted: false,
		Preview:   true,
	})
}
