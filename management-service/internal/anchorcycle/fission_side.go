package anchorcycle

import (
	"encoding/json"
	"strings"
)

// 切入侧面短名（筛选用，对应 fissionAngleSides 里「：」之前的维度名）。
// 侧面是产品无关的通用维度，决定「这段话从哪个角度切入、喂哪几条事实」。
var fissionSideNames = []string{
	"使用场景", "过程细节", "利益算账", "对比参照", "人群痛点", "品质来源", "使用方式",
}

// 事实分类 → 切入侧面 映射表。
//
// 事实的 category 是「内容域」（live_agent_plan_facts.category，如 flavor/craft/price），
// 侧面是「话术切入维度」。一条事实可归属多个侧面；不在表中的分类（如 other）视为
// 通用事实，不限定侧面——任何侧面都能引用。
//
// 这是「侧面筛事实」的落地：生成话术时按当前侧面，只喂该侧面标签下的事实，
// 模型手里只有这个侧面的素材，自然只能讲这个侧面（实测：同卖点×角度 7 侧面
// 轮换可拉开 7 种话术结构，替代软约束「切入侧面」的失效）。
var factCategorySides = map[string][]string{
	"flavor":        {"对比参照", "使用场景"}, // 口味：麻辣 vs 五香怎么选、什么场景吃
	"product":       {"过程细节", "品质来源"}, // 商品信息：怎么做、用什么原料
	"craft":         {"过程细节"},         // 制作工艺
	"fulfillment":   {"对比参照", "使用方式"}, // 物流：川渝隔天、真空包装
	"ingredients":   {"过程细节", "品质来源"}, // 配料成分
	"brand":         {"品质来源"},         // 品牌/门店历史
	"origin":        {"品质来源"},         // 产地
	"price":         {"利益算账", "对比参照"}, // 价格：算账、线上线下对比
	"usage":         {"使用方式"},         // 使用/食用方式
	"after_sales":   {"人群痛点"},         // 售后保障
	"audience":      {"使用场景", "人群痛点"}, // 目标人群/送礼对象
	"functions":     {"使用场景"},         // 功能用途
	"return_policy": {"人群痛点"},         // 退换货政策
	"trade":         {"利益算账"},         // 交易条款
}

// FactSides 返回指定分类对应的切入侧面短名列表。
// 未知/空分类返回 nil，表示该事实为通用事实，不限定侧面。
func FactSides(category string) []string {
	if sides, ok := factCategorySides[strings.ToLower(strings.TrimSpace(category))]; ok {
		return append([]string(nil), sides...)
	}
	return nil
}

// FactMatchesSide 判断某分类的事实是否属于指定侧面。
// 通用事实（nil sides）任何侧面都算匹配，保证兜底事实不因侧面筛选而丢失。
func FactMatchesSide(category, side string) bool {
	side = FissionSideName(side)
	sides := FactSides(category)
	if len(sides) == 0 {
		return true // 通用事实，不限侧面
	}
	for _, s := range sides {
		if s == side {
			return true
		}
	}
	return false
}

// FissionSideName 从侧面描述（如「使用场景：什么场合、谁用得上」）提取短名「使用场景」。
func FissionSideName(side string) string {
	s := strings.TrimSpace(side)
	for _, sep := range []string{"：", ":"} {
		if i := strings.Index(s, sep); i >= 0 {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// CategoryFromFactText 从事实文本提取分类。
// 事实文本形如「- [product/常温保存期限] 常温保存期限为…；禁说=…；安全改写=…」，
// 取第一个含「/」的方括号标记「[category/fact_key]」中的 category 部分。
// 人工归属是程序元数据，先剥离后再读分类，不改变原始事实及其商品归属。
// 无标记或归属前缀损坏时返回空串。
func CategoryFromFactText(text string) string {
	rest := strings.TrimSpace(text)
	for {
		start := strings.IndexByte(rest, '[')
		if start < 0 {
			return ""
		}
		header, ok := stripFactScopePrefix(rest[start+1:])
		if !ok {
			return ""
		}
		end := strings.IndexByte(header, ']')
		if end < 0 {
			return ""
		}
		inner := header[:end]
		if i := strings.IndexByte(inner, '/'); i > 0 {
			return strings.TrimSpace(inner[:i])
		}
		rest = header[end+1:]
	}
}

// Decode the whole JSON link before looking for header delimiters: a merchant
// can put '/', '；', ']' or escaped quotes in the link label. Keep this small
// parser in anchorcycle, which must not depend on its httpapi caller.
func stripFactScopePrefix(header string) (string, bool) {
	if rest, ok := strings.CutPrefix(header, "用户指定公共；"); ok {
		return rest, true
	}
	raw, explicit := strings.CutPrefix(header, "用户指定商品=")
	if !explicit {
		return header, true
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	var link string
	if decoder.Decode(&link) != nil || strings.TrimSpace(link) == "" {
		return "", false
	}
	rest, ok := strings.CutPrefix(raw[decoder.InputOffset():], "；")
	return rest, ok
}

// factFamilyBranding 是「品牌背书」类公共资料（identity_location 族：品牌/资质/产地/门店/历史），
// 只进公共段、不进商品段。factFamilyTransactional 是「交易/履约条件」类（trade/fulfillment 族），
// 商品段仅在结论依赖时按需引用。其余 family 为商品本体或其他，不受公共段/商品段区分约束。
const (
	factFamilyBranding       = "identity_location"
	factFamilyTransactional  = "trade"
	factFamilyTransactional2 = "fulfillment"
)

// factFamilyOf 从事实文本反查 family：先取 category，再查 State.FactFamilyMap（category→family，
// 直播启动时由行业分类目录注入）。查不到返回空串（视为商品本体/其他，不受公共/商品区分约束）。
func (s *State) factFamilyOf(text string) string {
	category := strings.ToLower(strings.TrimSpace(CategoryFromFactText(text)))
	if category == "" || s.FactFamilyMap == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(s.FactFamilyMap[category]))
}

// isBrandingFact 判断事实是否「品牌背书」类公共资料（B 类），商品段应挡掉。
func (s *State) isBrandingFact(text string) bool {
	return s.factFamilyOf(text) == factFamilyBranding
}

// isTransactionalFact 判断事实是否「交易/履约条件」类公共资料（A 类），商品段按需引用。
func (s *State) isTransactionalFact(text string) bool {
	switch s.factFamilyOf(text) {
	case factFamilyTransactional, factFamilyTransactional2:
		return true
	}
	return false
}

// SideHasDedicatedFact 判断某侧面在当前任务事实里是否有「专属事实」支撑。
// 通用事实（other/未知分类）任何侧面都能引用，不能作为「某侧面有支撑」的依据，
// 否则会退化成「所有侧面都有支撑」，失去筛事实的意义。
func (s *State) SideHasDedicatedFact(side string) bool {
	sideName := FissionSideName(side)
	mapping := s.FactSideMap
	if len(mapping) == 0 {
		mapping = factCategorySides
	}
	for _, f := range s.HookFacts() {
		category := strings.ToLower(strings.TrimSpace(CategoryFromFactText(f.Data)))
		sides, ok := mapping[category]
		if !ok || len(sides) == 0 {
			continue // 通用事实，不作为专属支撑
		}
		for _, sn := range sides {
			if sn == sideName {
				return true
			}
		}
	}
	return false
}

// HookFactsForSide 返回「当前切入侧面」下的专属事实（含通用事实）。
//
// 这是「侧面筛事实」的落地：每条事实从 Text 提取 category，category 经映射表
// 得到侧面，只保留属于当前侧面的 + 通用事实（other/未知分类）。正文生成只拿到
// 当前侧面的素材，信息结构随之拉开（实测：同卖点×角度 7 侧面 = 7 种话术结构）。
// 映射表优先用行业配置（State.FactSideMap），未配置时回退默认 factCategorySides。
//
// 兜底：无侧面（旧状态）或过滤后一条不剩（该侧面无任何事实）时回退全量，避免空素材。
func (s *State) HookFactsForSide() []HookFact {
	side := s.CurrentFissionSide()
	all := s.HookFacts()
	if side == "" {
		return all
	}
	sideName := FissionSideName(side)
	mapping := s.FactSideMap
	if len(mapping) == 0 {
		mapping = factCategorySides
	}
	out := make([]HookFact, 0, len(all))
	for _, f := range all {
		sides, ok := mapping[strings.ToLower(strings.TrimSpace(CategoryFromFactText(f.Data)))]
		if !ok || len(sides) == 0 {
			out = append(out, f) // 通用事实，不限侧面
			continue
		}
		for _, sn := range sides {
			if sn == sideName {
				out = append(out, f)
				break
			}
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}
