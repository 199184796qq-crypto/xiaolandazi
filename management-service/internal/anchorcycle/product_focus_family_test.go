package anchorcycle

import "testing"

// 问题2 修复：公共资料拆「品牌背书(B类)/交易履约条件(A类)」，商品段挡掉 B 类、
// 保留 A 类（结论依赖时按需引用，由模型把关），公共段全带。
func TestProductFactsFiltersBrandingCommonFacts(t *testing.T) {
	s := &State{
		Config: Config{Rolling: true},
		ProductFocus: &ProductFocus{
			Policy:  ProductPolicy{Version: "test"},
			Products: []FocusProduct{{LinkKey: "1"}, {LinkKey: "2"}},
			Facts: []ScopedFact{
				{Text: "- [brand/品牌] 39年老油坊", Shared: true},           // B类品牌背书
				{Text: "- [origin/产地] 四川绵阳河清镇", Shared: true},      // B类
				{Text: "- [price/价格] 95.6元", Shared: true},              // A类交易
				{Text: "- [shipping_time/发货] 当天发", Shared: true},       // A类履约
				{Text: "- [flavor/口味] 麻辣五香", Links: []string{"1"}, Applicable: true}, // 商品专属
			},
			Tasks: map[string]ScopedTask{},
		},
		FactFamilyMap: map[string]string{
			"brand": "identity_location", "origin": "identity_location",
			"price": "trade", "shipping_time": "fulfillment",
			"flavor": "product",
		},
	}
	// 商品段（single）：挡 B 类，保留 A 类 + 商品专属。
	tsk := Task{Key: "K", FocusLink: "1", ProductMode: "single"}
	got := s.ProductFacts(tsk)
	if containsStr(got, "39年老油坊") || containsStr(got, "四川绵阳") {
		t.Fatalf("商品段不应含品牌背书类公共事实，got=%q", got)
	}
	if !containsStr(got, "95.6元") || !containsStr(got, "当天发") {
		t.Fatalf("商品段应保留交易/履约条件类公共事实，got=%q", got)
	}
	if !containsStr(got, "麻辣五香") {
		t.Fatalf("商品段应含商品专属事实，got=%q", got)
	}
	// 公共段（common）：全带。
	tc := Task{Key: "K", Common: true, ProductMode: "common"}
	gotC := s.ProductFacts(tc)
	if !containsStr(gotC, "39年老油坊") || !containsStr(gotC, "四川绵阳") {
		t.Fatalf("公共段应含品牌背书类公共事实，got=%q", gotC)
	}
}

// 无 FactFamilyMap 时向后兼容：不挡任何公共事实（旧状态/未选行业不改变行为）。
func TestProductFactsNoFamilyMapKeepsAll(t *testing.T) {
	s := &State{
		Config: Config{Rolling: true},
		ProductFocus: &ProductFocus{
			Policy:  ProductPolicy{Version: "test"},
			Products: []FocusProduct{{LinkKey: "1"}, {LinkKey: "2"}},
			Facts: []ScopedFact{
				{Text: "- [brand/品牌] 39年老油坊", Shared: true},
				{Text: "- [flavor/口味] 麻辣", Links: []string{"1"}, Applicable: true},
			},
			Tasks: map[string]ScopedTask{},
		},
	}
	tsk := Task{Key: "K", FocusLink: "1", ProductMode: "single"}
	got := s.ProductFacts(tsk)
	if !containsStr(got, "39年老油坊") {
		t.Fatalf("无 family 映射时不应挡掉公共事实，got=%q", got)
	}
}
