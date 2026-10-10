package anchorcycle

import (
	"encoding/json"
	"strings"
	"testing"
)

func hookPlanFixture() (string, []HookFact) {
	f := []HookFact{{ID: "price", Products: []string{"1"}, Benefits: []string{}, Data: "日常价92元，当前价89.8元"}, {ID: "p2", Products: []string{"2"}, Benefits: []string{"b2"}, Data: "活动价格29.9元，2组，每组12包，额外2包"}}
	raw := `{"communication_goal":"解释选择依据","hooks":[{"hook_id":"h1","title":"理解价差","audience_concern":"优惠如何比较","entry_method":"从价格关系进入","explanation_action":"说明两种价格的差额","fact_ids":["price"],"product_ids":["1"],"benefit_ids":[]}],"calculations":[{"label":"价格差","operator":"subtract","operands":["92","89.8"],"fact_ids":["price"],"unit":"元"}]}`
	return raw, f
}
func TestHookPlanStrictStructureAndScopes(t *testing.T) {
	raw, f := hookPlanFixture()
	if _, e := ParseHookPlan(raw, f, []string{"1", "2"}); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(raw, `"title":"理解价差"`, `"title":"理解价差","speech_example":"念这个"`, 1), strings.Replace(raw, `"fact_ids":["price"]`, `"fact_ids":["unknown"]`, 1), strings.Replace(raw, `"product_ids":["1"]`, `"product_ids":["3"]`, 1), strings.Replace(raw, `"benefit_ids":[]`, `"benefit_ids":["b2"]`, 1)} {
		if _, e := ParseHookPlan(bad, f, []string{"1", "2"}); e == nil {
			t.Fatal("invalid protocol accepted", bad)
		}
	}
	// Comparing two products can use each one's private facts. No fixed
	// two-product limit and no requirement that every private fact be public.
	comparison := strings.Replace(raw, `"fact_ids":["price"],"product_ids":["1"],"benefit_ids":[]`, `"fact_ids":["price","p2"],"product_ids":["1","2"],"benefit_ids":["b2"]`, 1)
	if _, e := ParseHookPlan(comparison, f, []string{"1", "2"}); e != nil {
		t.Fatal(e)
	}
}
func TestHookPlanRotationImmutableScopeAndNoPhaseReplanning(t *testing.T) {
	s := fixture(t, 2)
	s.AnglePlanMotherSHA = "mother1"
	s.Queue[0].AllowedLinksJSON = `["1"]`
	s.Facts = "价格92元，当前89.8元"
	raw, f := hookPlanFixture()
	p, e := ParseHookPlan(raw, f, []string{"1"})
	if e != nil {
		t.Fatal(e)
	}
	p.Cards = append(p.Cards, AngleHook{ID: "h2", Title: "份量"})
	key := s.AnglePlanKey()
	s.AnglePlans = map[string]*AnglePlan{key: p}
	if s.CurrentAngleHook().ID != "h1" {
		t.Fatal("first hook missing")
	}
	s.Phase = "body"
	if s.AnglePlanKey() != key || s.CurrentAnglePlan() != p {
		t.Fatal("phase caused replanning")
	}
	s.Used[s.Task().Key] = Use{Count: 1}
	if s.CurrentAngleHook().ID != "h2" {
		t.Fatal("hook did not rotate")
	}
	s.Facts += "新条件"
	if s.CurrentAnglePlan() != nil {
		t.Fatal("stale facts reused")
	}
}
func TestHookCalculationExactDecimalsAndNonfatalUnsupportedOperands(t *testing.T) {
	raw, f := hookPlanFixture()
	p, e := ParseHookPlan(raw, f, []string{"1"})
	if e != nil {
		t.Fatal(e)
	}
	v, e := computeHookCalculation(p.Calculations[0], map[string]HookFact{"price": f[0]})
	if e != nil || v != "2.2" {
		t.Fatal(v, e)
	}
	for _, c := range []struct {
		op       string
		operands []string
		want     string
	}{{"subtract", []string{"87.6", "85.5"}, "2.1"}, {"multiply", []string{"29.9", "2"}, "59.8"}, {"divide", []string{"59.8", "26"}, "2.3"}, {"add", []string{"24", "2"}, "26"}} {
		data := strings.Join(c.operands, " ")
		v, e := computeHookCalculation(HookCalculation{Label: "fixture", Operator: c.op, Operands: c.operands, Facts: []string{"f"}}, map[string]HookFact{"f": {Data: data}})
		if e != nil || v != c.want {
			t.Fatal(v, e, c.want)
		}
	}
	s := fixture(t, 2)
	s.AnglePlanMotherSHA = "v1"
	s.Facts = "92和89.8"
	p.Cards[0].Facts = []string{"F001"}
	p.Calculations[0].Facts = []string{"F001"}
	p.Calculations[0].Operands = []string{"92", "999"}
	s.AnglePlans = map[string]*AnglePlan{s.AnglePlanKey(): p}
	values, warnings := s.CurrentHookCalculations()
	if len(values) != 0 || len(warnings) != 1 || s.CurrentAnglePlan() == nil || s.Phase == "ended" {
		t.Fatal("calculation failure blocked plan", values, warnings)
	}
}
func TestHookFactsKeepSavedReferencesBenefitsAndProductScope(t *testing.T) {
	s := fixture(t, 2)
	s.Queue[0].FocusLink = "1"
	s.Queue[0].AllowedLinksJSON = `["1"]`
	s.ProductFocus = &ProductFocus{Products: []FocusProduct{{LinkKey: "1", Roles: []string{"main"}}, {LinkKey: "2"}}, Facts: []ScopedFact{{ID: "public", Text: "公共资料", Links: []string{"1", "2"}, Shared: true}, {ID: "one", Text: "商品1价格", Links: []string{"1"}, Applicable: true}, {ID: "two", Text: "商品2价格", Links: []string{"2"}, Applicable: true}, {ID: "b1", Text: "活动条件", Links: []string{"1"}, BenefitIDs: []string{"benefits:9"}, Applicable: true}}}
	raw, _ := json.Marshal(s.HookFacts())
	if strings.Contains(string(raw), "商品2价格") || !strings.Contains(string(raw), "benefits:9") || len(s.HookFacts()) != 3 {
		t.Fatal(string(raw))
	}
}
