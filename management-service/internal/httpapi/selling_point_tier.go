package httpapi

import (
	"encoding/json"
	"strings"
)

// 卖点 tier 分级：程序按可复核规则初分，用户在界面确认/调整，不靠模型拍脑袋。
// 三级：core(核心主打，可多个，平级) / secondary(次级，可多个) / trust(信任福利：活动/售后/物流)。
// 权重层级：核心 > 次级 > 信任福利，供节奏规划分配出镜权重；多个同级之间平级。
var tierTrustWords = []string{"物流", "发货", "快递", "配送", "售后", "退换", "退款", "包邮", "运费", "保障", "活动", "优惠", "赠品", "福利"}

// tierCoreGroups 按「核心购买理由」分组：差异化亮点 > 价格算账 > 数字背书 > 品质来源。
// 命中任一组即判 core；组序目前用于将来「同级之间谁多谁少」的细分，现在仅作判定依据。
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

// coreCandidatePriority 返回该卖点命中的核心组序号（0 最高），非候选返回 -1。
func coreCandidatePriority(text string) int {
	for p, group := range tierCoreGroups {
		if matchesAnyTierWord(text, group) {
			return p
		}
	}
	return -1
}

// assignSellingPointTiers 给轮盘每个卖点补 tier：命中核心关键词 → core（可多个）；
// 命中信任福利词（物流/售后/活动/福利）且非核心 → trust；其余 → secondary。
// 只补空 tier，不覆盖用户已确认的 tier（core/secondary/trust 均保持不变）。
func assignSellingPointTiers(wheelJSON string) (string, error) {
	var wheel anchorStyleSellingPointWheel
	if err := json.Unmarshal([]byte(strings.TrimSpace(stripPolicyJSONFence(wheelJSON))), &wheel); err != nil {
		return "", err
	}
	for i := range wheel.Items {
		item := &wheel.Items[i]
		// 用户已确认的 tier 保持不变。
		if item.Tier == "core" || item.Tier == "secondary" || item.Tier == "trust" {
			continue
		}
		text := sellingPointTierText(*item)
		// core 优先于 trust：含核心关键词（价格/工艺/品牌/原料等）先判 core，
		// 即使同时含「福利/保障」等词也不按 trust 处理，避免「价格算账与福利权益」被误判。
		if coreCandidatePriority(text) >= 0 {
			item.Tier = "core"
			continue
		}
		if matchesAnyTierWord(text, tierTrustWords) {
			item.Tier = "trust"
			continue
		}
		item.Tier = "secondary"
	}
	out, err := json.Marshal(wheel)
	return string(out), err
}
