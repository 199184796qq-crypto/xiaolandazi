package httpapi

import (
	"encoding/json"
	"strings"
	"unicode"
)

func normalizeDevMainlineUnits(values []devMainlineUnitInput, maxItems int) []devMainlineUnitInput {
	seen := map[string]struct{}{}
	out := make([]devMainlineUnitInput, 0, len(values))
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Text = strings.TrimSpace(value.Text)
		value.Topics = normalizeUniqueStrings(value.Topics, 8)
		if value.ID == "" || value.Text == "" {
			continue
		}
		key := strings.ToUpper(value.ID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
		if maxItems > 0 && len(out) >= maxItems {
			break
		}
	}
	return out
}

func canonicalDevTopic(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "delivery", "shipping", "logistics", "shipping_time", "delivery_time", "物流", "发货", "时效", "到货":
		return "delivery"
	case "cooking", "how_to_eat", "eat", "吃法", "做法", "烹饪":
		return "cooking"
	case "spec_state", "spec", "weight", "规格", "重量":
		return "spec_state"
	case "identity_source", "origin", "产地", "来源":
		return "identity_source"
	case "appearance_meat", "meat", "texture", "口感", "肉质":
		return "appearance_meat"
	case "raising_activity", "raising", "放养", "饲养":
		return "raising_activity"
	case "feeding", "喂养", "饲料":
		return "feeding"
	case "slaughter_offal", "offal", "内脏", "宰杀":
		return "slaughter_offal"
	case "original_cut", "cut", "原切", "切块":
		return "original_cut"
	case "cta", "成交", "下单":
		return "cta"
	case "general", "普通", "通用":
		return "general"
	default:
		return value
	}
}

func devTopicSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		if topic := canonicalDevTopic(value); topic != "" {
			out[topic] = struct{}{}
		}
	}
	return out
}

func inferDevPlanTopics(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	out := make([]string, 0, 4)
	add := func(topic string) {
		for _, item := range out {
			if item == topic {
				return
			}
		}
		out = append(out, topic)
	}
	if strings.Contains(text, "物流") || strings.Contains(text, "发货") || strings.Contains(text, "到货") ||
		strings.Contains(text, "隔天") || strings.Contains(text, "几天") || strings.Contains(text, "时效") ||
		strings.Contains(text, "江浙沪") || strings.Contains(text, "快递") {
		add("delivery")
	}
	if strings.Contains(text, "怎么吃") || strings.Contains(text, "做法") || strings.Contains(text, "清蒸") ||
		strings.Contains(text, "炖") || strings.Contains(text, "煮") || strings.Contains(text, "炒") {
		add("cooking")
	}
	if strings.Contains(text, "毛重") || strings.Contains(text, "净重") || strings.Contains(text, "规格") {
		add("spec_state")
	}
	return out
}

func devUnitOverlapsTopics(unit devMainlineUnitInput, topics map[string]struct{}) bool {
	for _, topic := range unit.Topics {
		if _, ok := topics[canonicalDevTopic(topic)]; ok {
			return true
		}
	}
	return false
}

func sameDevStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func resolveDevInteractionResume(plan devInteractionPlan, input devAnswerTTSInput) (devInteractionPlan, bool) {
	if len(input.NextMainlineUnits) == 0 {
		return plan, false
	}
	plan.CoveredTopics = normalizeUniqueStrings(append(plan.CoveredTopics, inferDevPlanTopics(plan.ReplyCore)...), 16)
	covered := devTopicSet(plan.CoveredTopics)
	if len(covered) == 0 {
		return plan, false
	}

	skip := make([]string, 0, len(input.NextMainlineUnits))
	skipping := false
	resumeUnit := ""
	for _, unit := range input.NextMainlineUnits {
		overlap := devUnitOverlapsTopics(unit, covered)
		generalTail := false
		if skipping && !overlap {
			for _, topic := range unit.Topics {
				if canonicalDevTopic(topic) == "general" {
					generalTail = true
					break
				}
			}
		}
		if !skipping && !overlap {
			break
		}
		if overlap || generalTail {
			skip = append(skip, unit.ID)
			skipping = true
			continue
		}
		if skipping {
			resumeUnit = unit.ID
			break
		}
	}
	if len(skip) == 0 {
		return plan, false
	}

	oldMode := plan.ResumeMode
	oldResume := plan.ResumeUnit
	oldSkip := append([]string(nil), plan.SkipUnits...)
	plan.SkipUnits = skip
	plan.ResumeUnit = resumeUnit
	if len(skip) == 1 {
		plan.ResumeMode = "FUSION_SKIP"
	} else {
		plan.ResumeMode = "CROSS_RESUME"
	}
	return plan, oldMode != plan.ResumeMode || oldResume != plan.ResumeUnit || !sameDevStrings(oldSkip, plan.SkipUnits)
}

func devResumeTarget(plan devInteractionPlan, input devAnswerTTSInput) devMainlineUnitInput {
	for _, unit := range input.NextMainlineUnits {
		if unit.ID == plan.ResumeUnit {
			return unit
		}
	}
	if len(input.NextMainlineUnits) > 0 {
		return input.NextMainlineUnits[0]
	}
	return devMainlineUnitInput{}
}

func compactDevSpeech(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func devNGramOverlap(a, b string, n int) (int, float64) {
	ra := []rune(compactDevSpeech(a))
	rb := []rune(compactDevSpeech(b))
	if n <= 0 || len(ra) < n || len(rb) < n {
		return 0, 0
	}
	setA := map[string]struct{}{}
	setB := map[string]struct{}{}
	for i := 0; i <= len(ra)-n; i++ {
		setA[string(ra[i:i+n])] = struct{}{}
	}
	for i := 0; i <= len(rb)-n; i++ {
		setB[string(rb[i:i+n])] = struct{}{}
	}
	common := 0
	for gram := range setA {
		if _, ok := setB[gram]; ok {
			common++
		}
	}
	denom := len(setA)
	if len(setB) < denom {
		denom = len(setB)
	}
	if denom == 0 {
		return common, 0
	}
	return common, float64(common) / float64(denom)
}

func devResumeTailOverlapsTarget(plan devInteractionPlan, input devAnswerTTSInput) bool {
	target := devResumeTarget(plan, input)
	tail := compactDevSpeech(plan.ResumeTail)
	targetText := compactDevSpeech(target.Text)
	if tail == "" || targetText == "" {
		return false
	}
	if len([]rune(tail)) >= 6 && (strings.Contains(targetText, tail) || strings.Contains(tail, targetText)) {
		return true
	}
	common, _ := devNGramOverlap(tail, targetText, 4)
	return common >= 2
}

func sanitizeDevResumeTail(plan devInteractionPlan, input devAnswerTTSInput) (devInteractionPlan, bool) {
	if !devResumeTailOverlapsTarget(plan, input) {
		return plan, false
	}
	plan.ResumeTail = ""
	return plan, true
}

func devResumeCorrectionPrompt(plan devInteractionPlan, input devAnswerTTSInput) string {
	target := devResumeTarget(plan, input)
	raw, _ := json.Marshal(map[string]any{
		"reply_core":       plan.ReplyCore,
		"covered_topics":   plan.CoveredTopics,
		"skip_units":       plan.SkipUnits,
		"resume_unit":      plan.ResumeUnit,
		"resume_mode":      plan.ResumeMode,
		"resume_target":    target,
		"current_mainline": input.CurrentMainline,
	})
	return "本次回归桥接动态数据：\n" + string(raw)
}
