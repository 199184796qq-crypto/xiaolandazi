package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentstrategy"
)

func TestMainlineSideMaterialCannotReenterThroughContentStrategy(t *testing.T) {
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "0")
	s := cycleTestState(t)
	s.Phase = "body"
	s.BaselineJSON = textTestBaseline()
	s.FissionSides = []string{"利益算账"}
	s.FactSideMap = map[string][]string{"price": {"利益算账"}, "redemption": {"预约核销"}}
	price := `- [用户指定商品="1"；price/价格] 价格是已确认的337元`
	coupon := "- [用户指定公共；redemption/使用条件] 核销限定测试事实；安全改写=仅作条件约束"
	s.Facts = price + "\n" + coupon
	s.ContentStrategy = &contentstrategy.Snapshot{Enabled: true, Version: 1,
		Facts:    []contentstrategy.Fact{{ID: "price", Text: price}, {ID: "coupon", Text: coupon}},
		Purposes: []contentstrategy.Purpose{{ID: "p", Name: "比较预算"}, {ID: "c", Name: "解释核销"}},
		Rows:     []contentstrategy.Row{{PurposeID: "p", Angle: "价格角度", Task: "理解预算", FactIDs: []string{"price"}}, {PurposeID: "c", Angle: "核销角度", Task: "解释条件", FactIDs: []string{"coupon"}}, {PurposeID: "p", Angle: "混合角度", FactIDs: []string{"price", "coupon"}}},
	}
	before, _ := json.Marshal(s)
	messages, err := renderCycleTextMessages(&s)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range messages {
		if strings.Contains(m.Content, "核销限定测试事实") || strings.Contains(m.Content, "核销角度") || strings.Contains(m.Content, "混合角度") {
			t.Fatal("off-side strategy material reentered the complete generation messages")
		}
	}
	var packet map[string]any
	if err := json.Unmarshal([]byte(messages[0].Content), &packet); err != nil {
		t.Fatal(err)
	}
	// 内容策略表已移除：不再注入「内容方向与采用条件」，侧面事实只由「可说」承载。
	if _, ok := packet["内容方向与采用条件"]; ok {
		t.Fatal("内容方向与采用条件 should be removed from generation packet")
	}
	if len(packet["可说"].([]any)) != 1 {
		t.Fatal("side facts not filtered")
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("generation mutated the saved facts/strategy")
	}
	// The audit still needs the complete product-authorized facts, including
	// usage conditions; generation filtering must not erase its evidence.
	s.P0PolicyMD, _, err = loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	review, err := finalMainlineContentMessages(&s, anchorcycle.Candidate{Text: "已确认的337元"}, "测试审核母版")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(review)
	if !strings.Contains(string(raw), "核销限定测试事实") {
		t.Fatal("audit lost the complete facts")
	}
	// The same request is allowed to explain usage conditions when that side
	// is selected. No industry category is hard-coded into the renderer.
	s.FissionSides = []string{"预约核销"}
	messages, err = renderCycleTextMessages(&s)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(messages)
	if !strings.Contains(string(raw), "核销限定测试事实") {
		t.Fatal("selected-side conditions lost")
	}
}

func TestSideMaterialCacheContextTracksInjectedIndustry(t *testing.T) {
	_, state, _, _ := libraryFixture(t)
	state.FissionSides = []string{"行业侧面甲"}
	state.FactSideMap = map[string][]string{"category-a": {"行业侧面甲"}}
	original, err := libraryTextSHA(state, state.Task(), nil)
	if err != nil {
		t.Fatal(err)
	}
	state.FactSideMap["category-a"] = []string{"行业侧面乙"}
	changedMapping, err := libraryTextSHA(state, state.Task(), nil)
	if err != nil || original == changedMapping {
		t.Fatal("industry mapping change reused old material", err)
	}
	state.FactSideMap["category-a"] = []string{"行业侧面甲"}
	state.FissionSides = []string{"行业侧面乙"}
	changedPool, err := libraryTextSHA(state, state.Task(), nil)
	if err != nil || original == changedPool {
		t.Fatal("industry side pool change reused old material", err)
	}
}
