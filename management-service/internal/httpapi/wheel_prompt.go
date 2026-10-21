package httpapi

import (
	"encoding/json"
	"errors"
	"strings"
)

// 卖点轮盘生成的四层提示词骨架。
//
// 轮盘生成原来只有「母版 + 一段 effective_p0 JSON」：P0 红线散在两三个标题下，
// 也没有说明禁说该怎么改说，模型容易把极限词写进角度（实测出现过「极致性价比」）。
// 这里统一按话术同一套四层组织，让「可选事实」与「不可说」在同一个结构里对齐：
//   - 必说：本次任务（母版 + 行业维度）
//   - 可说：事实包（唯一真相源，放在 user 的 fact_package）
//   - 不提：P0 红线词表 + 行业专属红线
//   - 改写：禁说 → 改成可以说的表达
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

	p0MD = strings.TrimSpace(p0MD)
	if p0MD != "" {
		raw, err := json.Marshal(generationP0View(p0MD, "*"))
		if err != nil {
			return "", err
		}
		b.WriteString("\n# 不提（P0 红线：以下表述任何语境都不得出现；标 semantic 的只有「可说」事实里有原话时才可原样引用）\n")
		b.Write(raw)
		b.WriteString("\n")
	}
	if extra := strings.TrimSpace(industryExtra); extra != "" {
		b.WriteString("\n# 不提（P0 红线 · 行业补充）\n")
		b.WriteString(extra)
		b.WriteString("\n")
	}
	b.WriteString(`
# 改写（禁说 → 改说）
1. 绝对词与极限词 → 改用「很 / 非常 / 特别 / 相当 / 挺」，或直接陈述事实，不堆最高级。
2. 医疗与保健功效 → 只讲口味、食材、工艺，不讲任何调理、滋补或治病作用。
3. 虚假与夸大功效 → 只讲事实包写明的规格、成分、产地、口感，不承诺效果。
4. 无依据限时逼单 → 只如实转述事实包里的活动条件与截止时间，不衍生催促。
5. 站外引流、贬低同行、免检承诺、背书数据 → 只讲自己的事实；事实包没写的数字、排名、年限一律不出现。
`)
	return b.String(), nil
}
