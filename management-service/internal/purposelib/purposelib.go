package purposelib

import (
	"sort"
	"strings"
)

// Spec is one auto-generated purpose derived from a plan's facts, links and
// benefits. Keywords feed the program-side matcher before any model call.
type Spec struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	Keywords []string `json:"keywords"`
	InOut    bool     `json:"in_out"`
	Sources  []string `json:"sources"`
}

type categorySpec struct {
	key      string
	name     string
	keywords []string
	inOut    bool
}

// categoryMap turns a fact category into a purpose. Custom categories fall back
// to the fact key so new products still get a purpose without configuration.
var categoryMap = map[string]categorySpec{
	"price":         {"price", "价格与优惠", []string{"多少钱", "价格", "贵不贵", "便宜", "售价", "优惠"}, false},
	"promotion":     {"promotion", "价格与优惠", []string{"优惠", "活动", "划算", "折扣"}, false},
	"product":       {"spec", "规格与切块", []string{"规格", "多重", "几只", "分量", "切块", "大小"}, false},
	"flavor":        {"flavor", "口味与口感", []string{"好吃", "口味", "味道", "辣不辣", "口感", "腥"}, false},
	"ingredients":   {"ingredients", "原料与营养", []string{"什么鸭", "原料", "配料", "营养", "土麻鸭"}, false},
	"functions":     {"ingredients", "原料与营养", []string{"营养", "蛋白质", "脂肪"}, false},
	"craft":         {"craft", "工艺与新鲜", []string{"怎么做的", "工艺", "卤", "炸", "新鲜", "隔夜"}, false},
	"origin":        {"origin", "产地与品牌", []string{"哪里产", "产地", "正宗", "南充"}, false},
	"brand":         {"origin", "产地与品牌", []string{"品牌", "老店", "官方", "招牌"}, false},
	"business_history": {"origin", "产地与品牌", []string{"历史", "老店", "开了多久"}, false},
	"storage":       {"storage", "保存与保质期", []string{"能放几天", "保存", "保质期", "常温", "冷藏"}, false},
	"shelf_life":    {"storage", "保存与保质期", []string{"保质期", "放多久", "开袋"}, false},
	"usage":         {"usage", "复热与吃法", []string{"怎么吃", "怎么热", "微波炉", "空气炸锅", "开袋"}, false},
	"shipping_time": {"shipping_time", "发货时效", []string{"多久发", "什么时候发", "几天到"}, false},
	"delivery":      {"delivery", "快递", []string{"什么快递", "京东", "物流", "能指定"}, false},
	"shipping_fee":  {"shipping_fee", "运费", []string{"包邮", "运费", "邮费", "偏远", "空运"}, true},
	"pickup":        {"pickup", "自提", []string{"自提", "到店拿", "线下拿"}, false},
	"return_policy": {"return", "退换与售后", []string{"能退吗", "退货", "售后", "坏了"}, false},
	"other":         {"gift", "礼盒与包装", []string{"礼盒", "包装", "送礼", "送人"}, false},
	"audience":      {"audience", "适合人群与场景", []string{"适合送人", "送亲友", "适合谁"}, false},
	"scenario":      {"audience", "适合人群与场景", []string{"怎么吃", "下酒", "零食"}, false},
}

// Build merges fact categories, links and benefits into a deterministic list.
func Build(factCategories []string, factsKeys []string, hasLinks, hasBenefits bool) []Spec {
	byKey := map[string]*Spec{}
	add := func(spec categorySpec) {
		item, ok := byKey[spec.key]
		if !ok {
			item = &Spec{Key: spec.key, Name: spec.name, InOut: spec.inOut}
			byKey[spec.key] = item
		}
		for _, keyword := range spec.keywords {
			item.Keywords = appendUnique(item.Keywords, keyword)
		}
	}
	for i, category := range factCategories {
		spec, ok := categoryMap[strings.ToLower(strings.TrimSpace(category))]
		if !ok {
			key := factsKeys[i]
			spec = categorySpec{key: "fact_" + normalizeKey(category), name: strings.TrimSpace(key)}
			if spec.name == "" {
				spec.name = category
			}
		}
		add(spec)
		if item := byKey[spec.key]; item != nil {
			item.Sources = appendUnique(item.Sources, "fact")
		}
	}
	if hasLinks {
		add(categorySpec{key: "link", name: "链接与购买", keywords: []string{"链接", "小黄车", "怎么买", "几号链接", "在哪买", "购买"}})
		byKey["link"].Sources = appendUnique(byKey["link"].Sources, "link")
	}
	if hasBenefits {
		add(categorySpec{key: "benefit", name: "福利与优惠", keywords: []string{"包邮", "免邮", "福利", "活动", "领券", "优惠券"}})
		byKey["benefit"].Sources = appendUnique(byKey["benefit"].Sources, "benefit")
	}
	out := make([]Spec, 0, len(byKey))
	for _, item := range byKey {
		sort.Strings(item.Keywords)
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func appendUnique(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func normalizeKey(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	key := b.String()
	if key == "" {
		return "other"
	}
	return key
}
