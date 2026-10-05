package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
)

func fullShowPlanForKey(generation model.LiveAgentFullShowGenerationContext, key string, targetChars int) model.LiveSpeechExpansionPlan {
	for _, plan := range generation.ExpansionPlans {
		if strings.EqualFold(strings.TrimSpace(plan.VariantKey), strings.TrimSpace(key)) {
			return plan
		}
	}
	plans := speechexpander.BuildFixedPlans(speechexpander.Input{
		DurationMinutes: generation.RoundMinutes,
		TargetChars:     targetChars,
		VariantCount:    max(1, generation.VariantCount),
		FactKeys:        fullShowFactKeys(generation),
		BenefitKeys:     fullShowBenefitKeys(generation),
		LinkKeys:        fullShowLinkKeys(generation),
	})
	for _, plan := range plans {
		if strings.EqualFold(plan.VariantKey, key) {
			return plan
		}
	}
	if len(plans) > 0 {
		return plans[0]
	}
	return model.LiveSpeechExpansionPlan{Version: model.LiveSpeechExpansionVersion, Mode: "fixed_simulation", VariantKey: key, TargetChars: targetChars}
}

func fullShowRecentGuidance(recentTexts []string, current []model.LiveAgentFullShowVariant) string {
	items := make([]string, 0, len(recentTexts)+len(current))
	appendTail := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		runes := []rune(value)
		if len(runes) > 240 {
			value = string(runes[len(runes)-240:])
		}
		items = append(items, value)
	}
	for _, value := range recentTexts {
		appendTail(value)
	}
	for _, value := range current {
		appendTail(value.Text)
	}
	if len(items) == 0 {
		return ""
	}
	return "近期已经播过或正在对照的结尾如下。它们只用于避免连续复用相同开头和句式；重要事实仍可换角度回环：\n- " + strings.Join(items, "\n- ")
}

func coveredFullShowKeys(generation model.LiveAgentFullShowGenerationContext, text string) ([]string, []string) {
	facts := make([]string, 0)
	links := make([]string, 0)
	for _, fact := range generation.FormalFacts {
		if strings.TrimSpace(fact.Value) != "" && strings.Contains(text, fact.Value) {
			facts = append(facts, fact.Key)
		}
	}
	for _, link := range generation.ProductLinks {
		key := canonicalPlanProductLinkKey(link.LinkKey)
		if key != "" && strings.Contains(text, key) {
			links = append(links, link.LinkKey)
		}
	}
	return facts, links
}

func segmentedTTSHints(plan model.LiveSpeechExpansionPlan, enabled bool) []model.LiveAgentFullShowTTSHint {
	if !enabled || len(plan.Steps) == 0 {
		return nil
	}
	result := make([]model.LiveAgentFullShowTTSHint, 0, min(6, len(plan.Steps)))
	stride := max(1, len(plan.Steps)/6)
	for index := 0; index < len(plan.Steps) && len(result) < 6; index += stride {
		step := plan.Steps[index]
		instruction := "自然聊天感，清楚但不要持续高亢"
		if step.InteractionOpportunity {
			instruction = "语气放松，像自然抛出一个无需等待回答的问题"
		}
		result = append(result, model.LiveAgentFullShowTTSHint{Segment: step.Stage, Instruction: instruction, Rate: 1.06})
	}
	return result
}

func generateOneSegmentedFullShowVariant(
	ctx context.Context,
	generation model.LiveAgentFullShowGenerationContext,
	policyPrompt, key string,
	recentTexts []string,
	current []model.LiveAgentFullShowVariant,
	gateway anchorStyleCompleter,
) (model.LiveAgentFullShowVariant, string, string, int64, error) {
	targetChars := generation.RoundMinutes * 250
	if targetChars <= 0 {
		targetChars = 1750
	}
	plan := fullShowPlanForKey(generation, key, targetChars)
	plan.TargetChars = targetChars
	variantContext := generation
	variantContext.VariantCount = 1
	variantContext.ExpansionPlans = []model.LiveSpeechExpansionPlan{plan}
	topic := strings.TrimSpace(generation.PlanName + "。" + generation.PlanDescription)
	if guidance := fullShowRecentGuidance(recentTexts, current); guidance != "" {
		topic += "\n" + guidance
	}
	text, response, _, audit, repaired, err := generateAnchorStyleTest(ctx, gateway, variantContext, policyPrompt, topic, targetChars)
	if err != nil {
		return model.LiveAgentFullShowVariant{}, response.Provider, response.Model, response.LatencyMS, err
	}
	factKeys, linkKeys := coveredFullShowKeys(variantContext, text)
	opening := "按时间自然推进"
	if len(plan.Steps) > 0 && strings.TrimSpace(plan.Steps[0].Goal) != "" {
		opening = strings.TrimSpace(plan.Steps[0].Goal)
	}
	title := key + "稿 · 时间驱动"
	if repaired {
		title += "（局部补正）"
	}
	return model.LiveAgentFullShowVariant{
		VariantKey: key, Title: title, OpeningAngle: opening, Text: text,
		EstimatedMinutes: (utf8.RuneCountInString(text) + 249) / 250,
		CoveredFactKeys:  factKeys, CoveredLinkKeys: linkKeys, TTSHints: segmentedTTSHints(plan, generation.GenerateTTSHints), Audit: audit,
	}, response.Provider, response.Model, response.LatencyMS, nil
}

// generateFullShowVariants generates each variation as a sequence of small
// virtual-time units. Variants may run concurrently, but units inside one
// variant remain strictly ordered so length debt and spoken context carry over.
func generateFullShowVariants(
	ctx context.Context,
	generation model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	recentTexts []string,
	gateways ...anchorStyleCompleter,
) ([]model.LiveAgentFullShowVariant, string, string, int64, error) {
	count := generation.VariantCount
	if count < 1 {
		count = 1
	}
	if count > 5 {
		count = 5
	}
	keys := []string{"A", "B", "C", "D", "E"}
	results := make([]model.LiveAgentFullShowVariant, count)
	providers := make([]string, count)
	models := make([]string, count)
	latencies := make([]int64, count)
	errs := make([]error, count)
	semaphore := make(chan struct{}, min(3, count))
	var wait sync.WaitGroup
	gateway := speechGenerationGateway(gateways)
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				errs[index] = ctx.Err()
				return
			}
			variant, provider, modelName, latency, err := generateOneSegmentedFullShowVariant(ctx, generation, policyPrompt, keys[index], recentTexts, nil, gateway)
			variant.Index = index + 1
			results[index], providers[index], models[index], latencies[index], errs[index] = variant, provider, modelName, latency, err
		}()
	}
	wait.Wait()
	var provider, modelName string
	var latency int64
	for index := range results {
		latency += latencies[index]
		if provider == "" {
			provider = providers[index]
		}
		if modelName == "" {
			modelName = models[index]
		}
		if errs[index] != nil {
			return nil, provider, modelName, latency, fmt.Errorf("%s稿时间单元生成失败: %w", keys[index], errs[index])
		}
	}
	return results, provider, modelName, latency, nil
}

func generateSingleFullShowVariant(
	ctx context.Context,
	generation model.LiveAgentFullShowGenerationContext,
	policyPrompt string,
	targetKey string,
	currentVariants []model.LiveAgentFullShowVariant,
	recentTexts []string,
	gateways ...anchorStyleCompleter,
) (model.LiveAgentFullShowVariant, string, string, int64, error) {
	return generateOneSegmentedFullShowVariant(ctx, generation, policyPrompt, targetKey, recentTexts, currentVariants, speechGenerationGateway(gateways))
}
