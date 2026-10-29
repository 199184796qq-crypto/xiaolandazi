package decisionexecutor

import "strings"

// interactionSlotRules are attribute-level required slots, not per-question
// scripts. When the attribute's answer changes by the missing slot, ask one
// concrete question instead of answering from an assumed condition.
type interactionSlotRule struct {
	keywords []string
	question string
	present  func(string) bool
}

var interactionSlotRules = []interactionSlotRule{
	{
		keywords: []string{"抽真空", "真空包装"},
		question: "您是哪里的订单呀？本地发货不抽真空，外地才抽真空。",
		present:  hasPackagingRegion,
	},
}

func hasPackagingRegion(text string) bool {
	for _, token := range []string{"本地", "外地", "川渝", "四川", "南充", "上海", "北京", "成都", "重庆", "新疆", "海南"} {
		if strings.Contains(text, token) {
			return true
		}
	}
	return false
}

// missingInteractionSlot returns a slot question only when the question hits a
// known attribute and the slot that changes its answer is absent. It deliberately
// avoids asking when the question already contains the slot.
func missingInteractionSlot(question string) string {
	text := strings.TrimSpace(question)
	for _, rule := range interactionSlotRules {
		matched := false
		for _, keyword := range rule.keywords {
			if strings.Contains(text, keyword) {
				matched = true
				break
			}
		}
		if matched && !rule.present(text) {
			return rule.question
		}
	}
	return ""
}
