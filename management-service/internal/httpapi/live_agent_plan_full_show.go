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
	"livecompanion/management/internal/factexpansion"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
	"livecompanion/management/internal/speechexpander"
	"livecompanion/management/internal/stylecontract"
)

var (
	fullShowCommercialNumberPattern  = regexp.MustCompile(`(?i)[0-9]+(?:\.[0-9]+)?\s*(?:元|块|斤|两|公斤|千克|克|升|毫升|桶|件|盒|袋|折|%|年|万|分|kg|g|ml|l)`)
	fullShowLinkPattern              = regexp.MustCompile(`([0-9]{1,2})\s*号\s*链接`)
	fullShowInventoryPattern         = regexp.MustCompile(`(?:库存|还剩|剩下|最后)[^。！？\n]{0,24}[0-9]+(?:\.[0-9]+)?\s*(?:桶|件|盒|袋|单|份)`)
	fullShowSyntheticAudiencePattern = regexp.MustCompile(`(?:当前在线|在线|直播间(?:现在|当前)?|刚进来|新进来)[^。！？\n]{0,18}[0-9]+(?:\.[0-9]+)?\s*(?:个)?(?:人|位)`)
	fullShowCompositionLeakPattern   = regexp.MustCompile(`(?:品牌背书|事实点|信息点|写作|编稿|扩写|回环策略|结构安排|表达动作|换(?:个|一种)说法|再重复一遍|(?:这个|这条|这段|品牌背书).{0,10}收一下)`)
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
	if input.ExpansionFreedom == nil {
		value := factexpansion.DefaultFreedom
		input.ExpansionFreedom = &value
	}
	if input.StyleMatchIntensity == nil {
		value := anchorStyleDefaultMatchIntensity
		input.StyleMatchIntensity = &value
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
	if *input.ExpansionFreedom < 0 || *input.ExpansionFreedom > 100 {
		return errors.New("内容扩展授权只允许 0 到 100")
	}
	if *input.StyleMatchIntensity < 1 || *input.StyleMatchIntensity > 100 {
		return errors.New("主播风格还原强度只允许 1 到 100")
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

func fullShowExpansionFreedom(input model.LiveAgentFullShowPreviewInput) int {
	if input.ExpansionFreedom == nil {
		return factexpansion.DefaultFreedom
	}
	return *input.ExpansionFreedom
}

func fullShowStyleMatchIntensity(input model.LiveAgentFullShowPreviewInput) int {
	if input.StyleMatchIntensity == nil {
		return anchorStyleDefaultMatchIntensity
	}
	return *input.StyleMatchIntensity
}

func generationProductLinks(items []model.LiveAgentPlanProductLink) []model.LiveAgentPlanProductLink {
	result := make([]model.LiveAgentPlanProductLink, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
			continue
		}
		key := canonicalPlanProductLinkKey(item.LinkKey)
		if key == "" {
			continue
		}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		item.LinkKey = key
		result = append(result, item)
	}
	return result
}

func generationBenefits(items []model.LiveAgentPlanBenefit, productLinks []model.LiveAgentPlanProductLink, now time.Time) []model.LiveAgentPlanBenefit {
	activeLinks := make(map[string]struct{}, len(productLinks))
	for _, item := range productLinks {
		activeLinks[canonicalPlanProductLinkKey(item.LinkKey)] = struct{}{}
	}
	result := make([]model.LiveAgentPlanBenefit, 0, len(items))
	for _, item := range items {
		if !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
			continue
		}
		if item.StartsAt != nil && item.StartsAt.After(now) {
			continue
		}
		if item.EndsAt != nil && item.EndsAt.Before(now) {
			continue
		}
		item.LinkKey = canonicalPlanProductLinkKey(item.LinkKey)
		// Empty link_key is a plan-wide benefit. A link-scoped benefit must still
		// have a live product card, otherwise it is a ghost and cannot be spoken.
		if item.LinkKey != "" {
			if _, ok := activeLinks[item.LinkKey]; !ok {
				continue
			}
		}
		result = append(result, item)
	}
	return result
}

func compileAuthorizedGenerationFacts(
	formalFacts []model.LiveAgentFullShowContextFact,
	benefits []model.LiveAgentPlanBenefit,
	productLinks []model.LiveAgentPlanProductLink,
) []model.LiveAgentGenerationFact {
	attributeCount := 0
	for _, link := range productLinks {
		attributeCount += len(link.Attributes)
	}
	result := make([]model.LiveAgentGenerationFact, 0, len(formalFacts)+len(benefits)*3+len(productLinks)*5+attributeCount)
	appendFact := func(item model.LiveAgentGenerationFact) {
		item.Value = strings.TrimSpace(item.Value)
		if item.Value == "" {
			return
		}
		item.Status = "active"
		item.CanGenerate = true
		result = append(result, item)
	}
	for _, fact := range formalFacts {
		appendFact(model.LiveAgentGenerationFact{
			FactID:     fmt.Sprintf("supplemental_fact:%d:%s:v%d", fact.SourceID, strings.TrimSpace(fact.Key), fact.Version),
			SourceKind: "supplemental_fact", SourceID: fact.SourceID, SourceKey: strings.TrimSpace(fact.Key), ScopeKind: "plan",
			Predicate: strings.TrimSpace(fact.Key), Label: strings.TrimSpace(fact.Key), Value: fact.Value,
			ForbiddenWording: fact.ForbiddenWording, SafeRewrite: fact.SafeRewrite, Version: fact.Version,
		})
	}
	productFields := []struct {
		predicate string
		label     string
		value     func(model.LiveAgentPlanProductLink) string
	}{
		{"product_name", "商品名称", func(item model.LiveAgentPlanProductLink) string { return item.ProductName }},
		{"spec", "规格", func(item model.LiveAgentPlanProductLink) string { return item.Spec }},
		{"daily_price", "日常价", func(item model.LiveAgentPlanProductLink) string { return item.DailyPrice }},
		{"quantity", "数量/组合", func(item model.LiveAgentPlanProductLink) string { return item.Quantity }},
		{"audience", "适用信息", func(item model.LiveAgentPlanProductLink) string { return item.Audience }},
	}
	for _, link := range productLinks {
		for _, field := range productFields {
			appendFact(model.LiveAgentGenerationFact{
				FactID:     fmt.Sprintf("product_link:%d:%s:v%d", link.ID, field.predicate, link.VersionNo),
				SourceKind: "product", SourceID: link.ID, SourceKey: link.LinkKey, ScopeKind: "product_link",
				LinkKey: link.LinkKey, ProductName: strings.TrimSpace(link.ProductName), Predicate: field.predicate,
				Label: field.label, Value: field.value(link), Version: link.VersionNo,
			})
		}
		for _, attribute := range link.Attributes {
			if strings.ToLower(strings.TrimSpace(attribute.Status)) != "active" {
				continue
			}
			value := strings.TrimSpace(attribute.Value)
			if unit := strings.TrimSpace(attribute.Unit); value != "" && unit != "" {
				value += unit
			}
			appendFact(model.LiveAgentGenerationFact{
				FactID:     fmt.Sprintf("product_attribute:%d:%s:v%d", attribute.ID, strings.TrimSpace(attribute.Code), attribute.VersionNo),
				SourceKind: "product", SourceID: attribute.ID, SourceKey: link.LinkKey, ScopeKind: "product_link",
				LinkKey: link.LinkKey, ProductName: strings.TrimSpace(link.ProductName), Predicate: "attribute." + strings.TrimSpace(attribute.Code),
				Label: strings.TrimSpace(attribute.Label), Value: value, Version: attribute.VersionNo,
			})
		}
	}
	benefitFields := []struct {
		predicate string
		label     string
		value     func(model.LiveAgentPlanBenefit) string
	}{
		{"activity_price", "活动价", func(item model.LiveAgentPlanBenefit) string { return item.ActivityPrice }},
		{"gift", "赠品/权益", func(item model.LiveAgentPlanBenefit) string { return item.Gift }},
		{"activity", "活动规则", func(item model.LiveAgentPlanBenefit) string { return item.Activity }},
	}
	for _, benefit := range benefits {
		scopeKind := "product_link"
		if strings.TrimSpace(benefit.LinkKey) == "" {
			scopeKind = "plan"
		}
		for _, field := range benefitFields {
			appendFact(model.LiveAgentGenerationFact{
				FactID:     fmt.Sprintf("benefit:%d:%s:v%d", benefit.ID, field.predicate, benefit.VersionNo),
				SourceKind: "benefit", SourceID: benefit.ID, SourceKey: benefit.Key, ScopeKind: scopeKind,
				LinkKey: benefit.LinkKey, ProductName: strings.TrimSpace(benefit.ProductName), Predicate: field.predicate,
				Label: field.label, Value: field.value(benefit), ValidFrom: benefit.StartsAt, ValidUntil: benefit.EndsAt,
				Version: benefit.VersionNo,
			})
		}
	}
	return result
}

func compileFullShowContext(
	plan model.LiveAgentPlan,
	facts []model.LiveAgentPlanFact,
	benefits []model.LiveAgentPlanBenefit,
	productLinks []model.LiveAgentPlanProductLink,
	_ []model.LiveAgentPlanScriptReference,
	input model.LiveAgentFullShowPreviewInput,
) model.LiveAgentFullShowGenerationContext {
	return compileFullShowContextAt(plan, facts, benefits, productLinks, input, time.Now())
}

func compileFullShowContextAt(
	plan model.LiveAgentPlan,
	facts []model.LiveAgentPlanFact,
	benefits []model.LiveAgentPlanBenefit,
	productLinks []model.LiveAgentPlanProductLink,
	input model.LiveAgentFullShowPreviewInput,
	now time.Time,
) model.LiveAgentFullShowGenerationContext {
	productLinks = generationProductLinks(productLinks)
	benefits = generationBenefits(benefits, productLinks, now)
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
			SourceID:         fact.ID,
			Category:         strings.TrimSpace(fact.Category),
			Key:              key,
			Value:            value,
			ForbiddenWording: strings.TrimSpace(fact.ForbiddenWording),
			SafeRewrite:      strings.TrimSpace(fact.SafeRewrite),
			Version:          fact.VersionNo,
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
		style.Delivery = input.AnchorStyle.Delivery
		if style.Delivery != nil {
			copy := *style.Delivery
			style.Delivery = &copy
			style.Delivery.Rulebook = stylecontract.Render(style)
		}
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
	factKeys := make([]string, 0, len(formalFacts))
	for _, item := range formalFacts {
		factKeys = append(factKeys, item.Key)
	}
	benefitKeys := make([]string, 0, len(benefits))
	for _, item := range benefits {
		benefitKeys = append(benefitKeys, item.Key)
	}
	linkKeys := make([]string, 0, len(productLinks))
	for _, item := range productLinks {
		if strings.EqualFold(strings.TrimSpace(item.Status), "active") {
			linkKeys = append(linkKeys, item.LinkKey)
		}
	}
	expansionPlans := speechexpander.BuildFixedPlans(speechexpander.Input{
		DurationMinutes: input.RoundMinutes,
		TargetChars:     input.RoundMinutes * 250,
		VariantCount:    input.VariantCount,
		FactKeys:        factKeys,
		BenefitKeys:     benefitKeys,
		LinkKeys:        linkKeys,
	})
	return model.LiveAgentFullShowGenerationContext{
		PlanID:              plan.ID,
		PlanName:            strings.TrimSpace(plan.Name),
		PlanDescription:     strings.TrimSpace(plan.Description),
		RoomID:              input.RoomID,
		FormalFacts:         formalFacts,
		Benefits:            benefits,
		ProductLinks:        productLinks,
		FactManifestVersion: model.LiveGenerationFactManifestVersion,
		AuthorizedFacts:     compileAuthorizedGenerationFacts(formalFacts, benefits, productLinks),
		ScriptReferences:    nil,
		RhythmNodes:         rhythmNodes,
		AnchorStyle:         style,
		StyleMatchIntensity: fullShowStyleMatchIntensity(input),
		FactExpansion:       factexpansion.Compile(fullShowExpansionFreedom(input)),
		ExpansionMode:       "fixed_simulation",
		ExpansionPlans:      expansionPlans,
		DurationMinutes:     input.DurationMinutes,
		RoundMinutes:        input.RoundMinutes,
		RoundCount:          roundCount,
		VariantCount:        input.VariantCount,
		UseAnchorStyle:      input.UseAnchorStyle,
		UseDynamicFacts:     input.UseDynamicFacts,
		GenerateTTSHints:    input.GenerateTTSHints,
		AvoidRecent:         input.AvoidRecent,
		DraftProductSource:  false,
		DraftRhythmSource:   len(rhythmNodes) > 0,
		DraftStyleSource:    len(style.Dimensions) > 0 || len(style.ReusableRules) > 0,
	}
}

func fullShowFactKeys(generation model.LiveAgentFullShowGenerationContext) []string {
	keys := make([]string, 0, len(generation.FormalFacts))
	for _, item := range generation.FormalFacts {
		keys = append(keys, item.Key)
	}
	return keys
}

func fullShowBenefitKeys(generation model.LiveAgentFullShowGenerationContext) []string {
	keys := make([]string, 0, len(generation.Benefits))
	for _, item := range generation.Benefits {
		keys = append(keys, item.Key)
	}
	return keys
}

func fullShowLinkKeys(generation model.LiveAgentFullShowGenerationContext) []string {
	keys := make([]string, 0, len(generation.ProductLinks))
	for _, item := range generation.ProductLinks {
		if strings.EqualFold(strings.TrimSpace(item.Status), "active") {
			keys = append(keys, item.LinkKey)
		}
	}
	return keys
}

func generateFullShowVariantsOneShotLegacy(
	ctx context.Context,
	generationContext model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	recentTexts []string,
	gateways ...anchorStyleCompleter,
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

【最重要的事实与授权边界】
1. authorized_facts 是商品卡、当前有效福利和补充事实编译后的统一可生成事实清单，只允许使用 can_generate=true 的条目。product、benefit、supplemental_fact 都是事实来源，不得擅自补全或猜测。
2. formal_facts、benefits、product_links 是为兼容审计保留的原始分组视图，必须与 authorized_facts 一致；如有冲突，以 authorized_facts 为准。value 是可引用事实；forbidden_wording 绝对不能原样说出；safe_rewrite 是相同沟通意图的优先替代表达。
3. benefits 只包含编译时仍有效且仍关联现存商品卡的活动事实。活动价、赠品、满减、限时权益只能使用其中已有内容，不得把日常价说成活动价，也不得把一个链接的福利挪给另一个链接。
4. product_links.room_roles 是商品在直播间里的长期经营定位，只用于安排主次、返场和商品承接。主推、引流、福利、利润、搭配、普通都不是可朗读事实；不得直接播报这些标签，也不得由“福利/利润”推导免费、亏本、优惠或利润承诺。具体讲解方案可以变化，但不能反向篡改商品定位。
5. rhythm_nodes 和 anchor_style 只控制“怎么组织、怎么说”，不能覆盖事实依据。style_match_intensity只控制主播表达还原度；调高它不得扩大事实、数字或策略权限。
6. fact_expansion 是另一项独立的用户内容扩展授权。它只决定围绕正式事实可展开多少场景、类比、故事框架与常识性解释，不得改变主播风格还原强度。除法律、平台/L1/L2绝对禁区、formal_facts.forbidden_wording 和 always_locked 外，可以按照 freedom、level、allowed 扩展；不得把假设、泛化或故事冒充成已经发生的真实用户事件。
7. 如果 use_dynamic_facts=true，库存、实时在线、当前剩余量等必须保留成自然的运行时插槽，例如“库存我看一下后台实时数量再告诉大家”，绝对不能编具体数字。
8. 所有具体数字、价格、规格、数量、年限、评分、功效结论、资质、社会证明和真实人物证言必须有正式来源；数字保持来源写法，不自行换算。
9. 碰到审核边缘时执行 boundary_rewrite_first：保留原来的沟通目的，优先采用 safe_rewrite 或换成合法合规的说法，不要因为存在边界风险就整段沉默、只念事实或拒绝扩展。
10. “换个说法、再重复一遍、品牌背书、信息点、收一下、扩写、回环策略”等是内部编稿动作，绝对不能念给观众；直接说改写后的内容。

【生成目标】
一次生成 %d 套完整轮次口播，每套目标约 %d 分钟、约 %d 个中文字符。每套都必须是可以直接连续播的完整口语，不是提纲，不要输出舞台说明或内部系统术语。
A/B/C/D/E 的事实锚点完全一致，但可在 fact_expansion 授权内使用不同场景、类比、故事框架、情绪路径以及不同的开场语言动作、事实顺序、句式、转场、强调位置、回环位置和 CTA 位置，不能只是逐句替换同义词。
这不是摘要任务。单品事实有限时，允许把同一正式事实分散到多个自然口播回合并做授权范围内的有限扩展；每次回环至少改变一种表达动作，不能改动锁定事实或伪造具体数据、背书、实时状态和已发生事件。
同一套稿内部要自然连贯，不要每几十秒机械切阶段；把留人、讲品、已有价值事实、链接和 CTA 融成连续口播。允许合理重复，避免紧邻两句只换词不换表达动作。
主播风格只控制表达方式；不要把 anchor_style 里的证据句原样复制。
如果 anchor_style.delivery_spec 存在，必须执行其中的 rulebook，这是与测试和实时互动相同的主播口播规范；不能用整体画像替代原词、称呼位置及句式规则。事实边界优先于风格。
如果 style_overlay_prompt 非空，它是用户确认过的方案级叠加风格，只能在指定场景和频率内叠加到基础风格；不能把它改造成商品内容或销售策略。严肃场景规则优先于一般叠加效果。
expansion_plans 是按虚拟时间推进的固定片段计划。每套稿只执行与自身 variant_key 相同的计划，按 steps 顺序覆盖整段时长，并尽量接近每步 target_chars；不能跳过整轮，也不能把全部正式事实挤在开头。每步只执行该步 style_capabilities 列出的偶发表达能力；为空时不要强行加入偶尔口结、叠词、改口或慢思考。
steps.room 是内部模拟的房间状态，只用于决定新人承接、解释密度、回环和互动机会，绝不是可播事实；严禁念出模拟在线人数、进房量、评论量或声称看到真实观众行为。
interaction_opportunity=true 只允许提出一个无需假装已有回应、即使无人回答也能自然继续的开放式问题。它不是打断指令；直播时是否形成互动任务、何时打断和如何回归仍由 Core 决定。

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
      "opening_angle": "直接点题/先问后答/先讲一条正式事实/先做简短复述等",
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

	result, err := speechGenerationGateway(gateways).Complete(ctx, agentgateway.Request{
		Stage: "speech_generation",
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只生成直播整场主线预览稿。保持锁定事实，严格按用户的 fact_expansion 授权扩展，不执行真实业务动作，不把内部编稿过程写进正文。"},
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

func generateSingleFullShowVariantOneShotLegacy(
	ctx context.Context,
	generationContext model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	targetKey string,
	currentVariants []model.LiveAgentFullShowVariant,
	recentTexts []string,
	gateways ...anchorStyleCompleter,
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
	需要沿用当前方案的事实依据、活动、商品链接、节奏和主播风格，锁定事实不得改变；formal_facts.forbidden_wording 不能出现，表达相同意图时优先采用 safe_rewrite；在 fact_expansion 用户授权范围内，可重新组织表达、场景、类比、故事框架、开场语言动作、事实顺序、转场、回环位置和 CTA 位置。
这不是摘要任务。事实有限时允许同一事实非连续回环和有限扩展；可使用直述、拆句、问后自答、换序重述、短确认、回顾承接等动作，但不能伪造具体数字、真实顾客事件、背书、实时状态或受限结论，也不能把“换个说法、重复一遍、品牌背书、信息点、收一下”等内部动作念给观众。
必须明显避开当前其它稿件的高相似表达，也不要只是对旧 %s 稿做同义词替换。
只执行 expansion_plans 中 variant_key=%s 的虚拟时间计划，按 steps 顺序推进并尽量接近每步 target_chars。每步只执行该步 style_capabilities 列出的偶发表达能力；为空时不要强行加入偶尔口结、叠词、改口或慢思考。steps.room 是内部模拟参数，严禁作为在线人数、进房量、评论量或真实观众行为播出。interaction_opportunity 不是打断命令，只能形成无人回应也可自然继续的开放式问题；打断与回归仍归 Core。

【事实边界】
1. authorized_facts 是商品、福利和补充事实的统一锁定清单；formal_facts、benefits、product_links 是兼容视图，不能被反转、篡改或跨链接错配。
2. rhythm_nodes、anchor_style、style_overlay_prompt 只控制表达方式；fact_expansion 是用户对其余内容空间的扩展授权，叠加风格仍须遵守场景和次数预算。
3. 扩展必须服从政策规则和 fact_expansion.always_locked；不得伪造具体数字、库存、销量、评价、资质、检测、真实人物证言或实时房间状态。
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
		targetKey,
		string(currentJSON),
		string(contextJSON),
		string(recentJSON),
		targetKey,
	)
	result, err := speechGenerationGateway(gateways).Complete(ctx, agentgateway.Request{
		Stage: "speech_generation",
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

func fullShowForbiddenFactPhrases(context model.LiveAgentFullShowGenerationContext) []string {
	seen := map[string]struct{}{}
	phrases := make([]string, 0)
	add := func(value string) {
		for _, phrase := range strings.FieldsFunc(value, func(r rune) bool {
			return r == '\n' || r == '\r' || r == ';' || r == '；'
		}) {
			phrase = strings.Trim(strings.TrimSpace(phrase), "\"'“”‘’")
			if utf8.RuneCountInString(phrase) < 2 {
				continue
			}
			if _, ok := seen[phrase]; ok {
				continue
			}
			seen[phrase] = struct{}{}
			phrases = append(phrases, phrase)
		}
	}
	for _, fact := range context.FormalFacts {
		add(fact.ForbiddenWording)
	}
	// Exact segment scoping may hide compatibility views from the renderer, so
	// the hard audit also reads the unified manifest. This keeps user-authored
	// "不要这样说" boundaries active in mainline and interaction paths.
	for _, fact := range context.AuthorizedFacts {
		add(fact.ForbiddenWording)
	}
	return phrases
}

func auditFullShowVariants(
	generationContext model.LiveAgentFullShowGenerationContext,
	variants []model.LiveAgentFullShowVariant,
	recentTexts []string,
) []model.LiveAgentFullShowVariant {
	allowedCommercial := fullShowAllowedCommercialTokens(generationContext)
	forbiddenFactPhrases := fullShowForbiddenFactPhrases(generationContext)
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
		if fullShowSyntheticAudiencePattern.MatchString(item.Text) {
			addIssue("error", "synthetic_room_signal", "内部模拟的在线或进房参数不能作为真实直播间事实播出")
		}
		if fullShowCompositionLeakPattern.MatchString(item.Text) {
			addIssue("error", "composition_meta_leak", "检测到把换说法、重复、背书或收束等内部编稿动作直接念给观众")
		}
		for _, phrase := range forbiddenFactPhrases {
			if strings.Contains(item.Text, phrase) {
				addIssue("error", "forbidden_fact_wording", "检测到事实依据中明确禁止直接说出的表达："+phrase)
			}
		}
		if generationContext.UseAnchorStyle && stylecontract.Valid(generationContext.AnchorStyle) {
			check := stylecontract.CheckLongText(generationContext.AnchorStyle, item.Text)
			if !check.Passed {
				addIssue("warning", "style_literal_missing", "主播风格软指标偏差，遗漏充分取证的高频原词："+strings.Join(check.Missing, "、"))
			}
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
			addIssue("warning", "too_similar", fmt.Sprintf("与其它变化稿或近期文本相似度 %d%%；直播允许事实回环，但后续调度应优先换角度", maxSimilarity))
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
	gateways ...anchorStyleCompleter,
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
1. authorized_facts 是商品、福利和补充事实的统一锁定依据；formal_facts、benefits、product_links 是兼容视图。不得反转、篡改、跨链接错配或伪造具体值；forbidden_wording 不得出现在正文，表达相同意图时优先使用 safe_rewrite。
1.1 product_links.room_roles 只用于后台安排商品主次、返场和承接，不得把主推、引流、福利、利润、搭配、普通等内部标签写入观众口播，也不得从标签推导优惠或经营承诺。
2. 每一条 error 审计问题都必须在新稿中消除，尤其是：禁止说法、新增数字、未知链接、写死库存、高相似和内部编稿动作泄漏。
3. 价格、规格、数量、年限、评分等数字保持来源里的阿拉伯数字写法，不改成中文数字、不自行换算。
4. use_dynamic_facts=true 时，库存只能说“我看一下后台实时数量”等自然占位表达，绝不写具体剩余数。
5. 如果anchor_style.delivery_spec存在，沿用同一份rulebook；修复遗漏的高频原词时保持原有事实和适用场景，不另写风格、不机械堆叠称呼。
6. 如果style_overlay_prompt非空，继续执行其中已确认的叠加风格、次数预算和严肃场景限制。
7. 如果问题是 too_similar，必须在 fact_expansion 用户授权范围内重做场景、类比、故事框架、开场语言动作、事实顺序、句式、回环位置、转场和 CTA 位置，不能只换同义词。
8. 修复后仍须执行该 variant_key 对应的 expansion_plans 虚拟时间计划；每步只执行该步 style_capabilities 列出的偶发表达能力；steps.room 仅为内部模拟参数，不能被播出，互动机会也不能伪装成已有观众回应。
9. 如果触及审核边缘，按 fact_expansion.boundary_rewrite_first 保留沟通目的并优先采用 safe_rewrite，不要整段删除或退化成只念事实。
10. 删除“换个说法、重复一遍、品牌背书、信息点、收一下、扩写、回环策略”等内部编稿术语，直接输出改写后的观众口语。
11. 每个修复稿仍应是完整可连续播放的口语，目标约 %d 分钟、约 %d 个中文字符。
12. 只返回下方失败稿对应的 variant_key，不得新增其它变化稿。
13. 如果 generate_tts_hints=true，保留 3 到 6 条仅描述声音表现的 TTS 建议；rate 范围 0.95 到 1.15。

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

	result, err := speechGenerationGateway(gateways).Complete(ctx, agentgateway.Request{
		Stage: "speech_generation",
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你只修复程序审计失败的直播主线稿。不得改动锁定事实；可以在用户 fact_expansion 授权内扩展；不得执行真实业务动作或暴露内部编稿过程。"},
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
	TenantID            int64                                     `json:"tenant_id,omitempty"`
	RoomID              int64                                     `json:"room_id,omitempty"`
	DurationMinutes     int                                       `json:"duration_minutes"`
	RoundMinutes        int                                       `json:"round_minutes"`
	ExpansionFreedom    *int                                      `json:"expansion_freedom,omitempty"`
	StyleMatchIntensity *int                                      `json:"style_match_intensity,omitempty"`
	UseAnchorStyle      bool                                      `json:"use_anchor_style"`
	UseDynamicFacts     bool                                      `json:"use_dynamic_facts"`
	GenerateTTSHints    bool                                      `json:"generate_tts_hints"`
	AvoidRecent         bool                                      `json:"avoid_recent"`
	ProductLinks        []model.LiveAgentPlanProductLinkCandidate `json:"product_links"`
	RhythmNodes         []model.LiveAgentPlanRhythmNode           `json:"rhythm_nodes"`
	AnchorStyle         model.LiveAgentPlanAnchorStyleProfile     `json:"anchor_style"`
	Variants            []model.LiveAgentFullShowVariant          `json:"variants"`
	RecentTexts         []string                                  `json:"recent_texts,omitempty"`
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
		TenantID:            input.TenantID,
		RoomID:              input.RoomID,
		DurationMinutes:     input.DurationMinutes,
		RoundMinutes:        input.RoundMinutes,
		VariantCount:        len(input.Variants),
		StyleMatchIntensity: input.StyleMatchIntensity,
		ExpansionFreedom:    input.ExpansionFreedom,
		UseAnchorStyle:      input.UseAnchorStyle,
		UseDynamicFacts:     input.UseDynamicFacts,
		GenerateTTSHints:    input.GenerateTTSHints,
		AvoidRecent:         input.AvoidRecent,
		ProductLinks:        input.ProductLinks,
		RhythmNodes:         input.RhythmNodes,
		AnchorStyle:         input.AnchorStyle,
		RecentTexts:         input.RecentTexts,
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
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
	industryCode, l1, l2, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, input.RoomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取规则层与行业层失败")
		return
	}
	effectivePolicy := policy.BuildEffective(industryCode, l1, l2, nil)
	generationContext := compileFullShowContext(plan, facts, benefits, productLinks, nil, previewInput)
	generationContext.IndustryCode = strings.TrimSpace(industryCode)
	generationContext.PolicyRuleCount = len(effectivePolicy.Rules)
	if err := s.attachPlanStyleOverlay(r.Context(), tenantID, planID, &generationContext); err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案叠加风格失败")
		return
	}
	if len(generationContext.FormalFacts) == 0 && len(generationContext.Benefits) == 0 && len(generationContext.ProductLinks) == 0 {
		writeError(w, http.StatusBadRequest, "当前方案还没有可用于生成的正式事实、有效活动福利或正式商品链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 240*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "live_full_show_single_regenerate", map[string]any{
		"plan_id": planID, "variant_key": targetKey, "round_minutes": input.RoundMinutes,
	})
	generated, provider, modelName, latencyMS, err := generateSingleFullShowVariant(
		ctx, generationContext, effectivePolicy.PromptText, targetKey, input.Variants, input.RecentTexts, s.speechGateway(),
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
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
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, input.RoomID) {
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
	industryCode, l1, l2, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, input.RoomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取规则层与行业层失败")
		return
	}
	effectivePolicy := policy.BuildEffective(industryCode, l1, l2, nil)
	generationContext := compileFullShowContext(plan, facts, benefits, productLinks, nil, input)
	generationContext.IndustryCode = strings.TrimSpace(industryCode)
	generationContext.PolicyRuleCount = len(effectivePolicy.Rules)
	if err := s.attachPlanStyleOverlay(r.Context(), tenantID, planID, &generationContext); err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案叠加风格失败")
		return
	}
	if len(generationContext.FormalFacts) == 0 && len(generationContext.Benefits) == 0 && len(generationContext.ProductLinks) == 0 {
		writeError(w, http.StatusBadRequest, "当前方案还没有可用于生成的正式事实、有效活动福利或正式商品链接")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 285*time.Second)
	defer cancel()
	invocationID := s.beginAISingleUse(r.Context(), actor, nil, "live_full_show_preview", map[string]any{
		"plan_id": planID, "variant_count": input.VariantCount, "round_minutes": input.RoundMinutes,
	})
	variants, provider, modelName, latencyMS, err := generateFullShowVariants(ctx, generationContext, effectivePolicy.PromptText, input.RecentTexts, s.speechGateway())
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
	// Generation is already repaired one time unit at a time. Never send the
	// complete result back for a one-shot rewrite: hard failures stay visible
	// and cannot be promoted to a formal version.
	repairAttempted := false
	repairSucceeded := false
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
