package stylecontract

import (
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

func habitsOfKind(spec *model.LiveAnchorDeliverySpec, kind string) []model.LiveAnchorLiteralHabit {
	result := []model.LiveAnchorLiteralHabit{}
	for _, habit := range spec.Habits {
		if habit.Kind == kind {
			result = append(result, habit)
		}
	}
	return result
}

func sourcePunctuationCount(source string, marks string) int {
	count := 0
	for _, current := range source {
		if strings.ContainsRune(marks, current) {
			count++
		}
	}
	return count
}

var executableDimensionKeys = map[string]bool{
	"sentence_rhythm":       true,
	"address_position":      true,
	"repetition_strategy":   true,
	"emphasis_style":        true,
	"qa_structure":          true,
	"transition_style":      true,
	"pause_chunking":        true,
	"humanization":          true,
	"information_density":   true,
	"vocabulary_complexity": true,
	"tone_tendency":         true,
	"closing_style":         true,
	"variation_freedom":     true,
}

// groundedDimensionInstructions admits only a small, evidence-backed subset of
// model analysis into the executable contract. The source quote proves that the
// observation belongs to this anchor; the purity gate prevents product facts,
// sales logic and interrupt strategy from entering the style layer.
func groundedDimensionInstructions(dimensions []model.LiveAgentPlanAnchorStyleDimension, source string) []string {
	rules := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, dimension := range dimensions {
		if !executableDimensionKeys[strings.TrimSpace(dimension.Key)] {
			continue
		}
		confidence := strings.ToLower(strings.TrimSpace(dimension.Confidence))
		if confidence != "high" && confidence != "medium" {
			continue
		}
		rule := trim(dimension.Rule, 100)
		if rule == "" || strings.TrimSpace(dimension.Level) == "样本不足" || !pureStyleText(rule) || seen[rule] {
			continue
		}
		grounded := false
		for _, quote := range dimension.EvidenceQuotes {
			quote = strings.TrimSpace(quote)
			if quote != "" && strings.Contains(source, quote) {
				grounded = true
				break
			}
		}
		if !grounded {
			continue
		}
		seen[rule] = true
		rules = append(rules, rule)
		if len(rules) == 4 {
			break
		}
	}
	return rules
}

// compilePureInstructions is the deterministic half of the style compiler.
// Code owns the invariant rules. A bounded set of model observations may be
// appended only after source grounding and a semantic purity check.
func compilePureInstructions(spec *model.LiveAnchorDeliverySpec, source string, dimensions []model.LiveAgentPlanAnchorStyleDimension) []string {
	if spec == nil {
		return nil
	}
	average := spec.AverageSentenceChars
	if average <= 0 {
		average = 20
	}
	low, high := max(4, average*65/100), max(8, average*140/100)
	rules := []string{
		fmt.Sprintf("主线平均分句以%d字为中心，大多数分句控制在%d到%d字；一个分句只承担一个表达重点。", average, low, high),
		"保留长短句交替：解释句之后允许接一条短确认句或转折句，不把所有内容切成等长短句。",
		"同一正式事实允许在不同口播轮次回环出现；每次至少改变一种表达动作，例如直述、拆成短句、问后自答、换序重述、短确认或回顾承接。内容能扩展到什么程度由生成上下文中的fact_expansion用户授权决定，主播风格本身不扩大也不收窄该权限。",
	}
	rules = append(rules, groundedDimensionInstructions(dimensions, source)...)

	if habits := habitsOfKind(spec, "audience_address"); len(habits) > 0 {
		positions := map[string]bool{}
		for _, habit := range habits {
			positions[habit.Position] = true
		}
		positionNames := make([]string, 0, len(positions))
		for _, value := range []string{"句首", "句中", "句尾", "混合"} {
			if positions[value] {
				positionNames = append(positionNames, value)
			}
		}
		rules = append(rules, fmt.Sprintf("观众称呼只在%s等样本位置自然出现；同类称呼是替代关系，不连续堆叠。", strings.Join(positionNames, "、")))
	} else {
		rules = append(rules, "样本没有稳定观众称呼时，不为制造热情而临时发明称呼。")
	}
	if len(habitsOfKind(spec, "self_address")) > 0 {
		rules = append(rules, "主播方自指只沿用有重复证据的第一方原词；不得把团队成员、亲属或第三人称身份改成主播自称。")
	} else {
		rules = append(rules, "样本没有稳定主播方自指时，用自然省略主语的表达，不补造主播身份。")
	}
	if len(habitsOfKind(spec, "particle")) > 0 {
		rules = append(rules, "句末语气词仅用于自然停顿和语气柔化；相邻分句不重复堆同一个语气词。")
	} else {
		rules = append(rules, "样本没有稳定句末语气词时，保持克制陈述，不添加直播行业套话。")
	}
	if len(habitsOfKind(spec, "connector")) > 0 {
		rules = append(rules, "连接词用于递进、转折或换角度；每次只选择一个合适原词，不把连接词串联堆放。")
	} else {
		rules = append(rules, "样本没有稳定连接词时，依靠自然停顿衔接，不强塞固定转场词。")
	}
	if len(habitsOfKind(spec, "catchphrase")) > 0 {
		rules = append(rules, "中性确认口头语只能确认已经有依据的上一句；不用于制造承诺、口碑、紧迫感或执行状态。")
	} else {
		rules = append(rules, "样本没有稳定中性口头禅时，不从行业惯例补充口头禅。")
	}

	questions := sourcePunctuationCount(source, "？?")
	if questions > 0 {
		rules = append(rules, fmt.Sprintf("样本每%d个分句约出现%d个问句；问句只用于表达节奏或澄清，不假装收到真实回应。", max(spec.SentenceCount, 1), questions))
	} else {
		rules = append(rules, "样本缺少稳定问句证据时，以陈述推进，不额外制造问答腔。")
	}
	rules = append(rules,
		"主线表达按本次热度预算分布原词与停顿；事实准确和自然度优先于词频达标。",
		"短互动先直接回答核心问题，再选择最多一到两个合适风格标记自然承接，不复刻整段主线节奏。",
		"严肃答复、投诉和边界说明使用克制短句，关闭亲昵称呼、促销语气和不必要的句末语气词。",
	)
	if len(rules) > 16 {
		rules = rules[:16]
	}
	return rules
}
