package decisionexecutor

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

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

// greetingPhraseVariants 打招呼句式方向池：程序按 seed 确定性轮换，避免相邻几轮机械重复同一称呼/欢迎句式。
// 只给"句式方向"，具体措辞由模型结合昵称/群体自然生成，不硬套模板。
var greetingPhraseVariants = []string{
	"打招呼用「昵称直呼」开场（如“小陈，……”），不要每次都同一个套路",
	"打招呼用「昵称+称谓后缀」开场（如“小陈哥”“阿芳老乡”），自然变化",
	"打招呼用「问候式」开场（如“小陈你好”），自然不重复",
	"打招呼用「代称式」开场（如“这位朋友小陈”），自然不重复",
	"打招呼用「轻感叹」开场（如“诶小陈”“哟阿芳”），自然不重复",
	"多人时用「昵称合并」开场（如“小陈和阿芳，你们……”），自然不重复",
}

func greetingPhraseVariant(seed string) string {
	if len(greetingPhraseVariants) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.TrimSpace(seed)))
	return greetingPhraseVariants[int(binary.BigEndian.Uint32(sum[:4])%uint32(len(greetingPhraseVariants)))]
}
