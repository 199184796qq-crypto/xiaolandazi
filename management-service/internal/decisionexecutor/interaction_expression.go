package decisionexecutor

// 互动回答的表达变体种子：措辞层面的软变化。
// 程序按"近期同问题/同角度使用次数"轮换注入，让同一答案每次说法不同。
// 互动回答不缓存、实时生成，软变化靠程序给种子 + 实时生成，不靠缓存变体池。
var interactionExpressionVariants = []string{
	"直接给答案，简洁干脆，不加多余话",
	"先肯定一句观众，再给答案",
	"用一句简短理由引出答案",
	"先给结论，再补一句适用条件或提醒",
	"用轻松自然的语气带出答案",
}

func interactionExpressionVariant(n int) string {
	if len(interactionExpressionVariants) == 0 {
		return ""
	}
	if n < 0 {
		n = 0
	}
	return interactionExpressionVariants[n%len(interactionExpressionVariants)]
}
