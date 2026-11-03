package httpapi

import (
	"math/rand"
)

// openingKindCatalog 是开场方式清单，改写时随机指定一种。
//
// 目的：让每个新版本从不同角度切入，而不是靠"禁用某个词"。
var openingKindCatalog = []string{
	"时间点：从“现在几点、什么时间段”切入",
	"动作：从主播正在做的一个具体动作切入",
	"物件：从手里这只鸭子/包装的细节切入",
	"数字冲击：从价格或克重的具体数字切入",
	"对比：从线下门店价和直播间价的对比切入",
	"人群点名：从“一个人住/家里有孩子”这类具体人群切入",
	"场景还原：从“刚下班回家”“追剧到半夜”这类具体场景切入",
	"问题提问：用一个观众会有的问题开头",
	"痛点：从“买过难吃的”“怕踩坑”这类担心切入",
	"纠正误解：从“很多人以为……其实不是”切入",
	"反差：从“看着贵其实不贵”这类反差切入",
	"过程：从制作过程的一个环节切入",
	"结果：从“吃到嘴是什么感觉”切入",
	"地域：从南充本地人的说法切入",
	"季节：从当下时令切入",
	"自嘲：从主播自己的一句实话或糗事切入",
	"反向提醒：从“别急着拍”这类提醒切入",
	"吃法：从“怎么吃才最香”切入",
	"送礼：从“送人拿不拿得出手”切入",
	"数量：从“今天做了多少”这类实际数量切入",
	"对话：模拟一句观众常问的话切入",
	"回应质疑：模拟“有人说贵”然后回应",
	"直接结论：从“三个链接怎么挑”直接给结论",
	"日常：从“我自己家也这么吃”切入",
}

// pickOpeningKind 随机挑一种开场方式；已经用过的优先避开。
func pickOpeningKind(rng *rand.Rand, used []string) string {
	if len(openingKindCatalog) == 0 {
		return ""
	}
	usedSet := map[string]bool{}
	for _, u := range used {
		usedSet[u] = true
	}
	var fresh []string
	for _, kind := range openingKindCatalog {
		if !usedSet[kind] {
			fresh = append(fresh, kind)
		}
	}
	if len(fresh) == 0 {
		fresh = openingKindCatalog
	}
	return fresh[rng.Intn(len(fresh))]
}