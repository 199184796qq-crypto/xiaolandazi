package anchorcycle

import "fmt"

// 表达变体池：程序根据"本场该卖点角度已采纳次数"轮换分配"包装（开头句式）"和"长度"。
// 目的：轮盘循环到同一卖点角度时，靠程序指定的包装/长度差异打破模型低温复读，
// 而不是靠"换一种说法"这类模型不听软约束。后期若要扩充，改这两个切片即可。
var packagingPool = []string{
	"热情招呼式（如「家人们/哥哥姐姐们，看这里」招呼开头）",
	"提问切入式（如「买鸭子最怕啥？」「担心口感单一？」提问开头）",
	"直接陈述式（不招呼，直接讲卖点）",
	"算账对比式（如「咱们先算笔账」算账开头）",
	"场景带入式（如「想象一下深夜追剧/家里来客」场景开头）",
	"数字强调式（如「记住这几个数：4小时、两遍」数字开头）",
}

var lengthPool = []string{
	"短句（约30-50字，一句话讲透）",
	"中句（约60-80字，两句展开）",
	"长句（约90-120字，三段铺陈）",
}

// ExpressionVariant 返回本次生成应采用的"包装 + 长度"要求文本。
// 使用该卖点角度的已采纳次数做确定性轮换，确保同一角度每次复用时都换一种包装和长度。
func (s *State) ExpressionVariant() string {
	if s == nil {
		return ""
	}
	count := s.Used[s.Task().Key].Count
	p := packagingPool[count%len(packagingPool)]
	l := lengthPool[count%len(lengthPool)]
	return fmt.Sprintf("本轮开头用「%s」，全段控制在「%s」。", p, l)
}
