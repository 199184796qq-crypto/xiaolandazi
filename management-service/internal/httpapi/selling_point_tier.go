package httpapi

import (
	"encoding/json"
	"strings"
)

// 卖点 tier 分级：程序按可复核规则初分，用户在界面确认/调整，不靠模型拍脑袋。
// 三级：core(核心主打，最多1个) / secondary(次级) / trust(信任福利：活动/售后/物流)。
var tierTrustWords = []string{"物流", "发货", "快递", "配送", "售后", "退换", "退款", "包邮", "运费", "保障", "活动", "优惠", "赠品", "福利"}

// tierCoreGroups 按优先级从高到低：差异化亮点 > 价格算账 > 数字背书 > 品质来源。
// 命中优先级越靠前的卖点越优先当核心主打。
var tierCoreGroups = [][]string{
	{"独家", "独特", "独有", "唯一", "差异化", "非遗", "老字号", "首创", "专利", "特色"},
	{"价格", "差价", "算账", "性价比", "省钱", "优惠价", "活动价", "日常价"},
	{"门店", "年限", "销量", "复购", "好评", "证书", "认证", "检测", "背书"},
	{"品牌", "产地", "来源", "原料", "工艺", "酿造", "老卤", "手作", "制作"},
}

func sellingPointTierText(item anchorStyleSellingPointWheelItem) string {
	return item.Name + " " + strings.Join(item.Angles, " ")
}

func matchesAnyTierWord(text string, words []string) bool {
	for _, w := range words {
		if strings.Contains(text, w) {
			return true
		}
	}
	return false
}

// coreCandidatePriority 返回该卖点作为核心主打的优先级（0 最高），非候选返回 -1。
func coreCandidatePriority(text string) int {
	for p, group := range tierCoreGroups {
		if matchesAnyTierWord(text, group) {
			return p
		}
	}
	return -1
}

// assignSellingPointTiers 给轮盘每个卖点补 tier：信任福利类 → trust；其余按优先级
// 选出一个 core（差异化 > 价格 > 数字背书 > 品质来源），剩下的 → secondary。
// 只补缺或重新按规则初分，不覆盖用户已确认的 tier（已有合法 tier 时保持不变）。
func assignSellingPointTiers(wheelJSON string) (string, error) {
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(strings.TrimSpace(stripPolicyJSONFence(wheelJSON))), &wheel); err != nil {
		return "", err
	}
	coreIdx, corePriority, hasCore := -1, -1, false
	for i := range wheel.Items {
		item := &wheel.Items[i]
		// 用户已确认的 tier 保持不变；已存在 core 时不再另选核心。
		if item.Tier == "core" {
			hasCore = true
			continue
		}
		if item.Tier == "secondary" || item.Tier == "trust" {
			continue
		}
		text := sellingPointTierText(*item)
		if matchesAnyTierWord(text, tierTrustWords) {
			item.Tier = "trust"
			continue
		}
		if p := coreCandidatePriority(text); p >= 0 && !hasCore && (coreIdx < 0 || p < corePriority) {
			coreIdx, corePriority = i, p
		}
		item.Tier = "secondary"
	}
	if coreIdx >= 0 && !hasCore {
		wheel.Items[coreIdx].Tier = "core"
	}
	out, err := json.Marshal(wheel)
	return string(out), err
}
