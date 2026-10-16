package anchorcycle

import (
	"encoding/json"
	"livecompanion/management/internal/anchorstylescore"
	"strings"
	"testing"
)

func focusFixture(t *testing.T) State {
	t.Helper()
	tasks := []Task{{SellingPoint: "工艺", Angle: "传统制作"}, {SellingPoint: "规格", Angle: "一号整只"}, {SellingPoint: "规格对比", Angle: "整只半只差异"}, {SellingPoint: "包装", Angle: "二号半只"}, {SellingPoint: "吃法", Angle: "直接食用"}}
	s, err := New(tasks, Config{Threshold: 80, MaxRetries: 1, MaxChars: 300, Rolling: true, BodyMin: 2, BodyMax: 6})
	if err != nil {
		t.Fatal(err)
	}
	s.ProductFocus = &ProductFocus{Policy: ProductPolicy{Version: "test"}, Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"traffic"}}}, Facts: []ScopedFact{{Text: "共同品牌历史", Shared: true}, {Text: "一号整只900g87.6元", Links: []string{"1"}}, {Text: "二号半只450g48.8元", Links: []string{"2"}}, {Text: "整只半只容量价格差异", Links: []string{"1", "2"}}, {Text: "归属不明的材料"}}, Tasks: map[string]ScopedTask{}}
	for i, q := range s.Queue {
		var scope ScopedTask
		switch i {
		case 1:
			scope.Links = []string{"1"}
		case 2:
			scope.Links = []string{"1", "2"}
			scope.Comparison = true
		case 3:
			scope.Links = []string{"2"}
		default:
			scope.Shared = true
		}
		s.ProductFocus.Tasks[q.Key] = scope
	}
	s.SelectProductTask()
	return s
}
func auditedFocusCandidate(s *State, text string, score int) Candidate {
	c := candidate(text, score)
	c.ProductAudit = &ProductAudit{Passed: true, TextSHA: ProductDigest(text), ScopeSHA: s.ProductScopeSHA(s.Task())}
	return c
}

// 轮换起始：主推（角色基准最高）先出，但不再独占——advanceProduct 会推进到下一个商品。
func TestProductFocusInitializesToMainThenRotates(t *testing.T) {
	s := focusFixture(t)
	// 初始 CurrentLink 应为权重最高的主推（1 号）。
	if s.Task().FocusLink != "1" {
		t.Fatal("初始应聚焦主推，得到", s.Task().FocusLink)
	}
	// 推进一次应到 2 号（traffic）。
	if !s.advanceProduct() || s.Task().FocusLink != "2" {
		t.Fatal("advanceProduct 应轮换到 2 号，得到", s.Task().FocusLink)
	}
	// 再推进一次回到 1 号（两个商品循环）。
	if !s.advanceProduct() || s.Task().FocusLink != "1" {
		t.Fatal("应循环回 1 号，得到", s.Task().FocusLink)
	}
}

// 所有商品平等：不再要求「且仅一个主推」，多商品即可；主推只是基准更高。
func TestProductFocusAllowsMultipleProductsWithoutSingleMain(t *testing.T) {
	p := &ProductFocus{Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"traffic"}}, {LinkKey: "3", Roles: []string{"profit"}}}}
	if err := p.ValidateProducts(); err != nil {
		t.Fatal("多商品不应报错", err)
	}
	// 空或重复链接仍应拒绝。
	if err := (&ProductFocus{Products: []FocusProduct{{LinkKey: "1"}, {LinkKey: "1"}}}).ValidateProducts(); err == nil {
		t.Fatal("重复链接应拒绝")
	}
	if err := (&ProductFocus{Products: []FocusProduct{{LinkKey: "1"}}}).ValidateProducts(); err == nil {
		t.Fatal("少于两个商品应拒绝")
	}
}

func TestProductFocusScopedFactsAndComparison(t *testing.T) {
	s := focusFixture(t)
	t1 := s.Task()
	facts := s.ProductFacts(t1)
	if !strings.Contains(facts, "共同品牌") || !strings.Contains(facts, "900g") || strings.Contains(facts, "450g") || strings.Contains(facts, "归属不明") {
		t.Fatal(facts)
	}
	// 对比任务（Comparison）在单商品聚焦模式下始终不可选。
	if s.productTaskEligible(s.Queue[2]) {
		t.Fatal("comparison 应在单商品模式下不可选")
	}
}

func TestProductFocusRoundsFrozenAcrossRetryAndRedis(t *testing.T) {
	s := focusFixture(t)
	before := s.Task()
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Task() != before {
		t.Fatal("redis changed frozen focus")
	}
	if err := s.CheckProductVoice(Segment{Task: before, Selected: auditedFocusCandidate(&s, "一号整只900g。", 100)}); err != nil {
		t.Fatal(err)
	}
}

func TestProductFocusReturnToOwnLinkHasNoExtraDiversionGate(t *testing.T) {
	s := focusFixture(t)
	c := candidate("2号是半只装，想整只的还是回1号链接看。", 100)
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil || s.Accepted[0].Selected.ProductAudit != nil {
		t.Fatal("independent product audit still required", err)
	}
}

func TestProductFocusAuditRequiredAndLinkAliases(t *testing.T) {
	s := focusFixture(t)
	c := candidate("一号规格介绍。", 100)
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal("extra role audit required", err)
	}
	for _, text := range []string{"2号链接", "二号链接", "链接2", "链接 2"} {
		if !MentionsLink(text, "2") {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"2026年", "价格12元", "链接20", "12号链接"} {
		if MentionsLink(text, "2") {
			t.Fatal("false link match", text)
		}
	}
	if err := (ProductPolicy{}).Validate(); err == nil {
		t.Fatal("invalid policy accepted")
	}
}
