package anchorcycle

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentguard"
)

func p0Fixture(t *testing.T, n int) State {
	t.Helper()
	s := fixture(t, n)
	s.Config.MaxRetries = 0
	raw, err := os.ReadFile("../../../configs/prompts/anchor-style/compliance/L1/p0-hard-block.md")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.InstallP0(string(raw), "fixture"); err != nil {
		t.Fatal(err)
	}
	return s
}

func rejectP0(t *testing.T, s *State) {
	t.Helper()
	c := candidate("我们家的油零风险啊。", 100)
	c.P0Patch = s.P0PendingRepair
	if c.P0Patch != nil {
		c.P0RewriteNumber = c.P0Patch.RewriteNumber
	}
	if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted {
		t.Fatal("P0 rejected draft adopted", err)
	}
}

func TestP0FirstPlusThreeRepairsThenSkipWithoutAdoption(t *testing.T) {
	s := p0Fixture(t, 2)
	task := s.Task()
	s.PendingOpeningHook = &OpeningHook{ID: "unused-hook"}
	for i := 0; i < 4; i++ {
		rejectP0(t, &s)
		if len(s.Accepted) != 0 || len(s.Used) != 0 || s.StyleWindow != "" || s.BodyCount != 0 || len(s.RecentOpeningHookIDs) != 0 {
			t.Fatal("illegal text polluted memory")
		}
		if i < 3 && (s.P0PendingRepair == nil || s.P0PendingRepair.RewriteNumber != i+1 || s.Cursor != 0) {
			t.Fatalf("repair %d not scheduled: %+v", i, s)
		}
	}
	if len(s.P0Skipped) != 1 || len(s.P0Skipped[0].Attempts) != 4 || !s.P0SkippedKeys[task.Key] || s.Task().Key == task.Key || s.P0PendingRepair != nil {
		t.Fatal("skip not advanced", s)
	}
	if !add(t, &s, "老乡，我们家在这个镇上做油啊。", 80) || len(s.Accepted) != 1 || s.P0ConsecutiveSkips != 0 {
		t.Fatal("next task unable to adopt")
	}
	for _, a := range s.P0Skipped[0].Attempts {
		if a.ContentIssue != "p0_blocked" || a.P0Report == nil || a.P0Report.Passed {
			t.Fatal("skip trace lost", a)
		}
	}
}

func TestP0SafePatchPreservesRawAndCannotSelectIllegalHighestScore(t *testing.T) {
	s := p0Fixture(t, 2)
	rejectP0(t, &s)
	patch := s.P0PendingRepair
	raw := `{"replacements":[{"id":"part_1","text":"我们家在这个镇上做油啊。"}]}`
	text, err := patch.Apply(raw, 300)
	if err != nil {
		t.Fatal(err)
	}
	c := candidate(text, 30)
	c.RawText = raw
	c.P0Patch = patch
	// The style retry cap is zero. It may adopt the best safe draft, never the
	// initial 100-point blocked draft, despite that draft being in the trace.
	adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{})
	if err != nil || !adopted || s.Accepted[0].Selected.RawText != raw || s.Accepted[0].Selected.Text != text || strings.Contains(s.StyleWindow, "零风险") {
		t.Fatal("unsafe best selected", err, s)
	}
	if err = s.CheckP0Voice(text); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckP0Voice("零风险"); err == nil {
		t.Fatal("unsafe voice passed")
	}
}

func TestP0MalformedPatchConsumesBudgetAndSnapshotSurvives(t *testing.T) {
	s := p0Fixture(t, 2)
	rejectP0(t, &s)
	for i := 0; i < 3; i++ {
		encoded, _ := json.Marshal(s)
		var restored State
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		s = restored
		c := candidate(s.P0PendingRepair.BaseText, 100)
		c.P0Patch = s.P0PendingRepair
		c.P0PatchError = "invalid replacement schema"
		c.RawText = "bad json"
		if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted {
			t.Fatal(err)
		}
	}
	if len(s.P0Skipped) != 1 || s.P0Skipped[0].Attempts[3].RawText != "bad json" || s.Uncertain {
		t.Fatal("bad schema stuck forever", s)
	}
}

func TestP0RollingExhaustedPoolAndConsecutiveSkipProtection(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Config.Rolling = true
	s.Config.PointGap = 0
	s.Config.CombinationGap = 0
	for i := 0; i < 8; i++ {
		rejectP0(t, &s)
	}
	if s.Status != "compliance_blocked" || len(s.P0Skipped) != 2 {
		t.Fatal("exhausted pool kept calling", s)
	}
	s = p0Fixture(t, 8)
	s.Config.Rolling = true
	s.Config.PointGap = 0
	s.Config.CombinationGap = 0
	for i := 0; i < 20; i++ {
		rejectP0(t, &s)
	}
	if s.Status != "compliance_blocked" || len(s.P0Skipped) != 5 {
		t.Fatal("consecutive skip cap ignored", s)
	}
}

func TestP0HookCannotDemandForbiddenQuantityOrUserScopeOverride(t *testing.T) {
	s := p0Fixture(t, 1)
	s.PendingOpeningHook = &OpeningHook{CheckNumbers: true, RequiredQuantities: []string{"100%", "5斤"}}
	if s.openingHookProblem("开盖试吃5斤啊。") != "" {
		t.Fatal("forbidden hook number forced back")
	}
	if s.openingHookProblem("先看规则。") != "opening_hook_hard_value_missing" {
		t.Fatal("unrelated numeric checks waived")
	}
	s.Config.ComplianceScope = "all"
	if s.InstallP0(s.P0PolicyMD, "fixture") == nil {
		t.Fatal("scope override allowed")
	}
	empty := State{}
	if empty.CheckP0Voice("普通文字") == nil {
		t.Fatal("old run without policy bypassed voice gate")
	}
}

func TestP0ChecksActualOutputAndForgedReport(t *testing.T) {
	s := p0Fixture(t, 2)
	c := candidate("普通文字。", 100)
	c.OutputText = "零风险。"
	c.P0Report = &contentguard.Report{Passed: true}
	if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted || s.P0PendingRepair == nil {
		t.Fatal("output or forged report bypassed gate", err)
	}
}

func TestP0PlanAllowanceAppliesToTextAndVoiceOnlyForExactRuleAndTerm(t *testing.T) {
	s := p0Fixture(t, 2)
	p, err := contentguard.Compile(s.P0PolicyMD)
	if err != nil {
		t.Fatal(err)
	}
	s.P0Allowances = []P0Allowance{{RuleID: "l1.p0.absolute_claim", MatchedText: "老字号", MatchKey: p.MatchKey("老字号"), Decision: "用户已知晓并确认"}}
	if err = s.CheckP0Voice("我们是经营七十年的老字号。 "); err != nil {
		t.Fatal("approved plan-local wording was still blocked", err)
	}
	if err = s.CheckP0Voice("我们是全国第一。 "); err == nil {
		t.Fatal("an unrelated term from the same rule was incorrectly released")
	}
	c := candidate("我们是经营七十年的老字号。", 100)
	if adopted, addErr := s.AddCandidate(c, anchorstylescore.Baseline{}); addErr != nil || !adopted {
		t.Fatal("approved generated speech was not adopted", addErr)
	}
}
