package anchorcycle

import (
	"encoding/json"
	"fmt"
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
	s.ProductFocus = &ProductFocus{Policy: ProductPolicy{Version: "test", MainSharePercent: 75, MinMainRounds: 1, AuxiliaryMaxChars: 160}, Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2", Roles: []string{"traffic"}}}, Facts: []ScopedFact{{Text: "共同品牌历史", Shared: true}, {Text: "一号整只900g87.6元", Links: []string{"1"}}, {Text: "二号半只450g48.8元", Links: []string{"2"}}, {Text: "整只半只容量价格差异", Links: []string{"1", "2"}}, {Text: "归属不明的材料"}}, Tasks: map[string]ScopedTask{}}
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
func TestProductFocusRoundsFrozenAcrossRetryAndRedis(t *testing.T) {
	s := focusFixture(t)
	for i, want := range []string{"1", "1", "1", "1", "1", "1", "1", "1"} {
		s.CycleNumber = i + 1
		s.EnsureProductFocus()
		if got := s.Task().FocusLink; got != want {
			t.Fatal(i, got, want)
		}
	}
	before := s.Task()
	raw, _ := json.Marshal(s)
	var restored State
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Task() != before {
		t.Fatal("redis changed frozen focus")
	}
	if restored.BodyLimit() != s.BodyTarget {
		t.Fatal("main body range changed", restored.BodyLimit())
	}
	restored.Config.BodyMin = 4
	restored.BodyTarget = 5
	if restored.BodyLimit() != 5 {
		t.Fatal("user lower bound violated")
	}
	for i := range s.ProductFocus.Products {
		s.ProductFocus.Products[i].Roles = nil
	}
	if s.ProductFocus.ValidateProducts() == nil {
		t.Fatal("no main product accepted")
	}
}
func TestProductFocusScopedFactsAndComparisonBudget(t *testing.T) {
	s := focusFixture(t)
	t1 := s.Task()
	facts := s.ProductFacts(t1)
	if !strings.Contains(facts, "共同品牌") || !strings.Contains(facts, "900g") || strings.Contains(facts, "450g") || strings.Contains(facts, "归属不明") {
		t.Fatal(facts)
	}
	if s.productTaskEligible(s.Queue[2]) {
		t.Fatal("comparison allowed in opening")
	}
	s.Phase = "body"
	if s.productTaskEligible(s.Queue[2]) {
		t.Fatal("comparison interrupted main exclusive period")
	}
	s.ProductFocus.MainChars, s.ProductFocus.CompletedMainRounds = 600, 1
	if !s.beginAuxiliary() {
		t.Fatal("completed main round did not allow auxiliary")
	}
	if s.Task().ProductMode != "auxiliary" || len(s.Task().ProductLinks()) != 2 || !strings.Contains(s.ProductFacts(s.Task()), "450g") {
		t.Fatal(s.Task())
	}
	c := auditedFocusCandidate(&s, "1号链接整只900g；想要半只的可以看看2号450g，整只还是选1号。", 100)
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if s.ProductFocus.AuxiliaryActive || s.Phase != "opening" || s.Task().FocusLink != "1" || s.productTaskEligible(s.Queue[2]) {
		t.Fatal("auxiliary did not immediately return to main")
	}
	s.Phase = "closing"
	if s.Task().ProductMode != "single" || len(s.Task().ProductLinks()) != 1 {
		t.Fatal("closing kept comparison mode")
	}
}
func TestProductFocusReturnToOwnMainLinkHasNoExtraDiversionGate(t *testing.T) {
	s := focusFixture(t)
	c := candidate("2号是半只装，想整只的还是回1号链接看。", 100)
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil || s.Accepted[0].Selected.ProductAudit != nil {
		t.Fatal("independent product audit still required", err)
	}
}

func TestProductRoleAuditCannotBlockAnOtherwiseApprovedDraft(t *testing.T) {
	s := focusFixture(t)
	c := auditedFocusCandidate(&s, "整只87.6元，半只48.8元都可以买。", 100)
	c.ProductAudit.Passed = false
	c.ProductAudit.Reason = "历史商品角色审核拒绝"
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil || s.Status == "content_blocked" {
		t.Fatal("historical role gate blocked supply", err)
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

func TestProductFocusMultipleAuxiliariesRequireCompleteMainRounds(t *testing.T) {
	s := focusFixture(t)
	s.Config.BodyMin, s.Config.BodyMax, s.BodyTarget, s.Config.MaxCycles = 2, 2, 2, 4
	s.ProductFocus.Products = append(s.ProductFocus.Products, FocusProduct{LinkKey: "3", Roles: []string{"profit"}})
	task := Task{SellingPoint: "第三件", Angle: "真实规格", Key: "第三件×真实规格"}
	s.Queue = append(s.Queue, task)
	s.ProductFocus.Tasks[task.Key] = ScopedTask{Links: []string{"3"}}
	s.ProductFocus.Facts = append(s.ProductFocus.Facts, ScopedFact{Text: "第三件资料", Links: []string{"3"}})
	auxiliaries := []string{}
	lastWasAuxiliary := false
	for i := 0; s.Phase != "ended" && i < 30; i++ {
		task := s.Task()
		if task.ProductMode == "auxiliary" {
			if s.ProductFocus.CompletedMainRounds == 0 || lastWasAuxiliary || s.BodyCount != 0 || s.PriceHookAllowed() {
				t.Fatal("auxiliary entered before complete round or extended its own cycle")
			}
			auxiliaries = append(auxiliaries, task.FocusLink)
			// Redis reload and a rejected draft must not change focus or spend quota.
			beforeChars, beforeCursor := s.ProductFocus.AuxiliaryChars, s.ProductFocus.AuxiliaryCursor
			raw, _ := json.Marshal(s)
			if err := json.Unmarshal(raw, &s); err != nil || s.Task() != task {
				t.Fatal("auxiliary changed on Redis reload", err)
			}
			bad := auditedFocusCandidate(&s, "辅推仅作本轮补充。", 10)
			bad.ProductAudit.Passed = false
			if ok, err := s.AddCandidate(bad, anchorstylescore.Baseline{}); ok || err != nil {
				t.Fatal(ok, err)
			}
			if s.Task() != task || beforeChars != s.ProductFocus.AuxiliaryChars || beforeCursor != s.ProductFocus.AuxiliaryCursor {
				t.Fatal("rejected auxiliary advanced budget")
			}
		} else if task.FocusLink != "1" || len(task.ProductLinks()) != 1 {
			t.Fatal("main round mixed auxiliary", task)
		}
		if lastWasAuxiliary && (s.Phase != "opening" || task.ProductMode != "single") {
			t.Fatal("did not return immediately to main opening")
		}
		text := strings.Repeat("这段只展开当前商品已有的真实内容。", 7)
		if task.ProductMode == "auxiliary" {
			text = "主推作为主要选择，辅推仅作已知规格的补充，随后回到主推。"
		}
		ok, err := s.AddCandidate(auditedFocusCandidate(&s, text, 100), anchorstylescore.Baseline{})
		if err != nil || !ok {
			t.Fatal(i, ok, err)
		}
		lastWasAuxiliary = task.ProductMode == "auxiliary"
		p := s.ProductFocus
		if p.AuxiliaryChars*p.Policy.MainSharePercent > p.MainChars*(100-p.Policy.MainSharePercent) {
			t.Fatal("combined auxiliaries exceeded share")
		}
		if err := s.CheckProductVoice(s.Accepted[len(s.Accepted)-1]); err != nil {
			t.Fatal("accepted voice rejected after focus changed", err)
		}
	}
	if s.Status != "completed" || fmt.Sprint(auxiliaries) != "[2 3 2]" || s.OpeningCount != 4 || s.ProductFocus.CompletedMainRounds != 4 {
		t.Fatal(s.Status, auxiliaries, s.OpeningCount, s.ProductFocus.CompletedMainRounds)
	}
}

func TestProductFocusBudgetAndUnavailableAuxiliaryDoNotBreakMain(t *testing.T) {
	s := focusFixture(t)
	p := s.ProductFocus
	p.MainChars = 1000
	if s.beginAuxiliary() {
		t.Fatal("character budget alone allowed auxiliary before complete main round")
	}
	p.CompletedMainRounds = 1
	if !s.beginAuxiliary() {
		t.Fatal("eligible auxiliary absent")
	}
	before := p.AuxiliaryChars
	c := auditedFocusCandidate(&s, strings.Repeat("长", s.Task().ProductMaxChars+1), 100)
	c.OutputText = strings.Repeat("长", s.Task().ProductMaxChars)
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if p.AuxiliaryChars <= before || p.AuxiliaryActive {
		t.Fatal("bounded spoken auxiliary did not return to main")
	}
	if s.beginAuxiliary() {
		t.Fatal("auxiliary twice without another full main round")
	}
	s = focusFixture(t)
	s.ProductFocus.MainChars, s.ProductFocus.CompletedMainRounds = 1000, 1
	s.P0SkippedKeys = map[string]bool{s.Queue[2].Key: true, s.Queue[3].Key: true}
	if s.beginAuxiliary() || s.Phase != "opening" || s.Task().FocusLink != "1" || s.Status != "ready" {
		t.Fatal("unavailable auxiliary blocked main")
	}
	s.ProductFocus.AuxiliaryChars = 300
	if s.AuxiliaryCharBudget() != 33 {
		t.Fatal("combined budget calculated incorrectly", s.AuxiliaryCharBudget())
	}
	if s.beginAuxiliary() {
		t.Fatal("insufficient budget allowed auxiliary")
	}
}

func TestProductFocusGenericLinksAndIsolatedRunConfiguration(t *testing.T) {
	for _, main := range []string{"sku-main-7", "98"} {
		s := focusFixture(t)
		s.TenantID, s.PlanID, s.RoomID = 81001, 92002, 73003
		other := "sku-secondary-19"
		s.ProductFocus.Products = []FocusProduct{{LinkKey: other, Roles: []string{"traffic"}}, {LinkKey: main, Roles: []string{"main"}}}
		s.ProductFocus.Facts = []ScopedFact{{Text: "该方案专属主推事实", Links: []string{main}}, {Text: "该方案专属辅推事实", Links: []string{other}}}
		for _, task := range s.Queue {
			s.ProductFocus.Tasks[task.Key] = ScopedTask{Links: []string{main}}
		}
		s.ProductFocus.Tasks[s.Queue[3].Key] = ScopedTask{Links: []string{other}}
		if err := s.ProductFocus.ValidateProducts(); err != nil {
			t.Fatal(err)
		}
		s.SelectProductTask()
		if s.Task().FocusLink != main || strings.Contains(s.ProductFacts(s.Task()), "辅推") {
			t.Fatal("bound to link 1 or original product")
		}
		s.ProductFocus.MainChars, s.ProductFocus.CompletedMainRounds = 1000, 1
		if !s.beginAuxiliary() || s.Task().FocusLink != other {
			t.Fatal("arbitrary auxiliary identity failed")
		}
		separate := focusFixture(t)
		if separate.ProductFocus.MainChars != 0 || separate.ProductFocus.AuxiliaryActive || separate.Task().FocusLink != "1" {
			t.Fatal("room state leaked")
		}
		separate.ProductFocus.Products[1].Roles = []string{"main"}
		if separate.ProductFocus.ValidateProducts() == nil {
			t.Fatal("multiple main products accepted")
		}
		s.ProductFocus.ResetRun()
		if s.ProductFocus.MainChars != 0 || s.ProductFocus.CompletedMainRounds != 0 || s.ProductFocus.AuxiliaryActive || s.ProductFocus.MainLink() != main {
			t.Fatal("new run restored old progress or rewrote configuration")
		}
	}
}
