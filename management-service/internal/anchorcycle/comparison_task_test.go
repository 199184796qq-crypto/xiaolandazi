package anchorcycle

import (
	"strings"
	"testing"
)

// 对比任务（一条角度要同时讲两个链接）默认不排；开了 comparison_mode 才排，
// 而且只在「当前轮到的链接正好在它比较的范围里」时才排。
func TestComparisonTaskSchedulingFollowsPolicy(t *testing.T) {
	key := "1号链接×价格算账×对比两档套餐的节省金额"
	task := Task{LinkKey: "1号链接", SellingPoint: "价格算账", Angle: "对比两档套餐的节省金额", Key: key}
	build := func(mode, current string) *State {
		return &State{
			Queue:     []Task{task},
			TaskIndex: 0,
			Phase:     "body",
			Config:    Config{Rolling: true, MaxChars: 300},
			ProductFocus: &ProductFocus{
				Products:    []FocusProduct{{LinkKey: "1号链接"}, {LinkKey: "2号链接"}, {LinkKey: "3号链接"}},
				CurrentLink: current,
				Policy:      ProductPolicy{Version: "test-v1", ComparisonMode: mode},
				Tasks: map[string]ScopedTask{
					key: {Links: []string{"1号链接", "2号链接"}, Comparison: true},
				},
			},
		}
	}
	if got := build("", "1号链接").taskScopeEligible(task); got {
		t.Fatal("comparison task must stay off when policy is empty (legacy snapshots)")
	}
	if got := build("off", "1号链接").taskScopeEligible(task); got {
		t.Fatal("comparison task must stay off when policy is off")
	}
	if got := build("fact", "1号链接").taskScopeEligible(task); !got {
		t.Fatal("comparison task must be eligible once the policy allows it")
	}
	if got := build("fact", "2号链接").taskScopeEligible(task); !got {
		t.Fatal("comparison task must be eligible on any link it compares")
	}
	if got := build("fact", "3号链接").taskScopeEligible(task); got {
		t.Fatal("comparison task must not run when the current link is not part of the comparison")
	}
	// 商品级轮换：只有策略允许时，参与比较的链接才算「这轮有活可干」。
	scope := ScopedTask{Links: []string{"1号链接", "2号链接"}, Comparison: true}
	if scopeSchedulable(scope, "1号链接", ProductPolicy{Version: "v1"}) {
		t.Fatal("off policy must keep the product out of the rotation")
	}
	if !scopeSchedulable(scope, "2号链接", ProductPolicy{Version: "v1", ComparisonMode: "fact"}) {
		t.Fatal("allowed policy must keep compared products in rotation")
	}
	if scopeSchedulable(scope, "3号链接", ProductPolicy{Version: "v1", ComparisonMode: "fact"}) {
		t.Fatal("a product outside the comparison must not be kept in rotation by it")
	}
}

// 对比段必须把两个链接的事实一起给模型，否则它只能凭印象自己拼另一边。
func TestFocusComparisonTaskWidensFacts(t *testing.T) {
	key := "1号链接×价格算账×对比两档套餐的节省金额"
	task := Task{LinkKey: "1号链接", SellingPoint: "价格算账", Angle: "对比两档套餐的节省金额", Key: key}
	s := &State{
		Queue:     []Task{task},
		TaskIndex: 0,
		Phase:     "body",
		Surface:   "formal_live",
		Config:    Config{Rolling: true, MaxChars: 300},
		ProductFocus: &ProductFocus{
			Products:    []FocusProduct{{LinkKey: "1号链接"}, {LinkKey: "2号链接"}},
			CurrentLink: "1号链接",
			Policy:      ProductPolicy{Version: "test-v1", ComparisonMode: "fact"},
			Tasks: map[string]ScopedTask{
				key: {Links: []string{"1号链接", "2号链接"}, Comparison: true},
			},
			Facts: []ScopedFact{
				{Text: "[1号链接] 团购337元，原价547元", Links: []string{"1号链接"}},
				{Text: "[2号链接] 团购187元，原价245元", Links: []string{"2号链接"}},
				{Text: "[两档] 10人餐比6人餐多7道荤素菜", Links: []string{"1号链接", "2号链接"}},
			},
		},
	}
	focused := s.focusTask(task)
	if focused.ProductMode != "comparison" {
		t.Fatalf("comparison task must switch product mode, got %q", focused.ProductMode)
	}
	if got := focused.ProductLinks(); len(got) != 2 || got[0] != "1号链接" || got[1] != "2号链接" {
		t.Fatalf("comparison task must authorize both links, got %v", got)
	}
	if focused.FocusLink != "1号链接" {
		t.Fatalf("focus link must stay on the owning pool, got %q", focused.FocusLink)
	}
	facts := s.ProductFacts(focused)
	for _, want := range []string{"团购337元", "团购187元", "多7道荤素菜"} {
		if !strings.Contains(facts, want) {
			t.Fatalf("comparison facts must include %q, got:\n%s", want, facts)
		}
	}
	// 普通单链接段不能顺手拿到另一个链接的价格。
	single := s.focusTask(Task{LinkKey: "1号链接", SellingPoint: "价格算账", Angle: "核对1号链接团购价", Key: "single"})
	if single.ProductMode != "single" {
		t.Fatalf("ordinary task must stay single, got %q", single.ProductMode)
	}
	if strings.Contains(s.ProductFacts(single), "团购187元") {
		t.Fatal("single-link segment must not receive the other link's price")
	}
}
