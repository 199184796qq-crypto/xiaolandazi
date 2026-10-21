package decisionexecutor

import "strings"

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

// neutralAddressTerms 中性托底称谓词：不假设年龄/性别/地域/亲密度。
// 当主播风格主力称呼和用户 PreferredTerms 都没有时兜底，保证任何直播间都有可播称呼。
// 单用“哥/姐/叔/姨”会默认对方性别，托底池只放中性词和男女成对词。
var neutralAddressTerms = []string{
	"朋友", "这位朋友", "大家", "各位",
	"哥姐", "哥哥姐姐", "叔叔阿姨", "兄弟姐妹",
	"老乡", "老板", "家人",
}

// pairedAddressTerms 是男女同时出现的成对称呼，未知性别时优先于单性别词。
var pairedAddressTerms = map[string]string{
	"哥":  "哥姐",
	"哥哥": "哥哥姐姐",
	"姐":  "哥姐",
	"姐姐": "哥哥姐姐",
	"叔":  "叔叔阿姨",
	"叔叔": "叔叔阿姨",
	"姨":  "阿姨",
	"阿姨": "叔叔阿姨",
	"兄弟": "兄弟姐妹",
	"姐妹": "兄弟姐妹",
}

// normalizePairedAddressTerm 把单个性别称呼换成男女成对词；其它词原样返回。
func normalizePairedAddressTerm(term string) string {
	term = strings.TrimSpace(term)
	if paired, ok := pairedAddressTerms[term]; ok {
		return paired
	}
	return term
}

// normalizeAddressPool 对称呼池逐项做成对化并去重。
func normalizeAddressPool(terms []string) []string {
	out := make([]string, 0, len(terms))
	seen := map[string]struct{}{}
	for _, term := range terms {
		term = normalizePairedAddressTerm(term)
		if term == "" {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		out = append(out, term)
	}
	return out
}

// greetingPhraseByScene 打招呼句式方向池，按互动场景分池。
// 程序给"句式方向"，具体措辞由模型结合昵称/群体自然生成，不硬套模板。
var greetingPhraseByScene = map[string][]string{
	"welcome": {
		"欢迎用「欢迎+称呼」开场（如“欢迎小陈”），自然不重复",
		"欢迎用「来了啊」开场（如“小陈来了啊”），自然不重复",
		"欢迎用「问候」开场（如“小陈你好”），自然不重复",
		"欢迎用「也来啦」开场（如“小陈也来啦”），自然不重复",
	},
	"product_question": {
		"回答用「昵称直呼」开场（如“小陈，你问的……”），自然不重复",
		"回答用「昵称+中性后缀」开场（如“小陈朋友”“阿芳老乡”），自然不重复",
		"回答用「称呼后直接给内容」开场（如“小陈，你问的规格是……”），不要先评价问题",
		"回答用「代称式」开场（如“这位朋友小陈”），自然不重复",
	},
	"warmup": {
		"暖场用「群体称呼」开场（如“家人们”“哥哥姐姐们”），自然不重复",
		"暖场用「招呼全体」开场（如“大家”“各位”），自然不重复",
	},
	"like_follow": {
		"感谢用「谢谢+称呼」开场（如“谢谢小陈”），自然不重复",
		"感谢用「支持」开场（如“感谢小陈支持”），自然不重复",
	},
	"order": {
		"回应下单用「谢谢+称呼」开场（如“谢谢小陈”），自然不重复",
		"回应下单用「破费了」开场（如“小陈破费了”），自然不重复",
	},
	"smalltalk": {
		"打招呼用「昵称直呼」开场（如“小陈，……”），自然不重复",
		"打招呼用「问候式」开场（如“小陈你好”），自然不重复",
		"打招呼用「代称式」开场（如“这位朋友小陈”），自然不重复",
		"打招呼用「轻感叹」开场（如“诶小陈”），自然不重复",
	},
	"default": {
		"打招呼用「昵称直呼」开场（如“小陈，……”），自然不重复",
		"打招呼用「昵称+中性后缀」开场（如“小陈朋友”），自然不重复",
		"打招呼用「问候式」开场（如“小陈你好”），自然不重复",
		"打招呼用「代称式」开场（如“这位朋友小陈”），自然不重复",
	},
}

// greetingScenePool 返回指定场景的句式池；未知场景回退 default。
func greetingScenePool(scene string) []string {
	if pool, ok := greetingPhraseByScene[scene]; ok && len(pool) > 0 {
		return pool
	}
	return greetingPhraseByScene["default"]
}
