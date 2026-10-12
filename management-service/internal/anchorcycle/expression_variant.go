package anchorcycle

import "fmt"

// 表达变体池：程序按"本场已采纳段数"轮换分配"包装（开头句式）"和"长度"。
// 目的：主线每段话术即使卖点角度相同，也要靠程序指定的包装/长度差异打破模型低温复读，
// 而不是靠"换一种说法"这类模型不听的软约束。后期若要扩充，改这两个切片即可。
// 注意：示例要通用，不写死任何具体商品（食品/建材/日化都适用）。
var packagingPool = []string{
	"热情招呼式（先招呼观众注意，再进入主题）",
	"提问切入式（用观众最关心的一个问题开头，再展开解答）",
	"直接陈述式（不铺垫，直接讲核心卖点）",
	"算账式（先帮观众算一笔账，突出实惠）",
	"场景带入式（先描述一个具体使用场景，把观众带入）",
	"数字强调式（先用一个关键数字抓住注意力）",
}

var lengthPool = []string{
	"短句（约30-50字，一句话讲透）",
	"中句（约60-80字，两句展开）",
	"长句（约90-120字，三段铺陈）",
}

// ExpressionVariant 返回本次生成应采用的"包装 + 长度"要求文本。
// 用本场已采纳段数做确定性轮换，确保每一段（无论角度是否相同）都用不同的包装和长度。
func (s *State) ExpressionVariant() string {
	if s == nil {
		return ""
	}
	count := len(s.Accepted)
	p := packagingPool[count%len(packagingPool)]
	l := lengthPool[count%len(lengthPool)]
	return fmt.Sprintf("本轮开头用「%s」，全段控制在「%s」。", p, l)
}
