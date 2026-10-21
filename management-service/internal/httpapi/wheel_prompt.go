package httpapi

import (
	"errors"
	"strings"

	"livecompanion/management/internal/contentguard"
)

// 卖点轮盘生成的四层提示词骨架。
//
// 轮盘生成原来只有「母版 + 一段 effective_p0 JSON」：P0 是带元信息字段的结构化数据，
// 模型读起来像配置表，实测角度里照样写出「极致性价比」。这里改成与话术同一套四层，
// 并且「不提」只给按类别的裸词表——生成时模型要的是「哪些词不能写」，
// 不是「规则适用范围/匹配正则/规则说明」这些程序才用的元信息。
func renderWheelFourLayerPrompt(mother, p0MD, industryExtra, extraBoundary string) (string, error) {
	mother = strings.TrimSpace(mother)
	if mother == "" {
		return "", errors.New("卖点轮盘母版为空")
	}
	var b strings.Builder
	b.WriteString("# 必说（本次任务）\n")
	b.WriteString(mother)
	if extra := strings.TrimSpace(extraBoundary); extra != "" {
		b.WriteString("\n\n# 必说（本行业额外要求）\n")
		b.WriteString(extra)
	}
	b.WriteString("\n\n# 可说（事实包）\n")
	b.WriteString("唯一真相源是 user 消息里的 fact_package。卖点、目标人群、痛点、角度只能使用其中已有的商品、链接、数字与条件；")
	b.WriteString("事实包没有写的，一律不写，也不要写成「未知」「待确认」。\n")

	if notSay := renderWheelNotSay(p0MD); notSay != "" {
		b.WriteString("\n# 不提（以下词句一律不许出现在卖点或角度里，任何语境都不许）\n")
		b.WriteString(notSay)
		if extra := strings.TrimSpace(industryExtra); extra != "" {
			b.WriteString("\n本行业另有专属红线，同样一律不许出现：\n")
			b.WriteString(extra)
		}
	}
	b.WriteString(`
# 改写（禁说 → 该怎么说）
1. 想夸 → 用「很 / 非常 / 特别 / 相当 / 挺」，或直接把事实讲出来，不堆最高级。
2. 想讲功效 → 只讲口味、食材、工艺、分量，不讲调理、滋补、治病。
3. 想保证效果 → 只讲事实包写明的规格、成分、产地、口感，不承诺结果。
4. 想催单 → 只如实写事实包里的活动条件与截止时间，不写「抓紧、赶紧、手慢无」这类催促。
5. 想证明实力 → 只讲事实包里写了的数字与资质；没写的排名、销量、年限、门店数一律不出现。
`)
	return b.String(), nil
}

// renderWheelNotSay 把 P0 策略渲染成按类别的裸词表。
//
// 只取类别与词条：这两个是生成模型真正用来避词的依据。规则说明、适用范围、
// 匹配表达、所需依据都是程序扫描或人工维护用的元信息，放进生成提示词只会占 token。
func renderWheelNotSay(p0MD string) string {
	p0MD = strings.TrimSpace(p0MD)
	if p0MD == "" {
		return ""
	}
	policy, err := contentguard.Compile(p0MD)
	if err != nil {
		// 编译不了就原文照用：宁可占 token，也不能让红线静默消失。
		return p0MD
	}
	var b strings.Builder
	for _, rule := range policy.Data.Rules {
		terms := make([]string, 0, len(rule.Terms))
		for _, term := range rule.Terms {
			if term = strings.TrimSpace(term); term != "" {
				terms = append(terms, term)
			}
		}
		if len(terms) == 0 {
			continue
		}
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(rule.Category))
		b.WriteString("：")
		b.WriteString(strings.Join(terms, "、"))
		b.WriteString("\n")
	}
	return b.String()
}
