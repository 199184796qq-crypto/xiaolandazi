package decisionexecutor

// 互动回答的表达变体种子：判断观众意图 + 完成 1-2 项最高价值目标 + 软变化。
// 程序按"近期同问题/同角度使用次数"轮换注入，让同一答案每次说法不同。
// 核心：先答准问题，再根据观众目的（解决顾虑 / 了解信息 / 比价确认 / 想下单）
// 挖掘并完成 1-2 项最高价值的事，不机械逼单、不超短。
// 互动回答不缓存、实时生成，软变化靠程序给种子 + 实时生成，不靠缓存变体池。
var interactionExpressionVariants = []string{
	"先答准问题，再判断对方是有顾虑还是在了解：有顾虑就用一句有事实依据的安心话打消，想了解就补一条最关键的卖点",
	"先答准问题，再补一条和问题直接相关的关键信息，让回答更有价值，不硬推销",
	"先答准问题，再判断对方下一步最需要什么（比价/售后/使用建议），针对性补一条",
	"先答准问题，再用一句有事实依据的话让观众更放心，完成打消顾虑这一项",
	"先答准问题，再顺势点一个最相关的卖点或引导看链接，完成带货这一项，但不过度",
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
