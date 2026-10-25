package anchorcycle

import (
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

// rejectP0Unfixable 用删词后也为空的违规文本（整段只剩违规词），
// 模拟「局部重写耗尽 + 删词兜底失败」→ 仍走跳过。
func rejectP0Unfixable(t *testing.T, s *State) {
	t.Helper()
	c := candidate("零风险。", 100)
	c.P0Patch = s.P0PendingRepair
	if c.P0Patch != nil {
		c.P0RewriteNumber = c.P0Patch.RewriteNumber
	}
	if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted {
		t.Fatal("P0 rejected draft adopted", err)
	}
}

func TestP0ViolationSkipsImmediatelyWithoutAdoption(t *testing.T) {
	s := p0Fixture(t, 2)
	task := s.Task()
	s.PendingOpeningHook = &OpeningHook{ID: "unused-hook"}
	// 局部重写已移除：P0 违规（删词兜底也救不回的整段违规）应直接跳过，不调模型重写。
	rejectP0Unfixable(t, &s)
	if len(s.Accepted) != 0 || len(s.Used) != 0 || s.SegmentDirective != "" || s.BodyCount != 0 || len(s.RecentOpeningHookIDs) != 0 {
		t.Fatal("illegal text polluted memory")
	}
	if s.P0PendingRepair != nil || s.Cursor == 0 {
		t.Fatal("violation should skip immediately without scheduling repair", s)
	}
	if len(s.P0Skipped) != 1 || len(s.P0Skipped[0].Attempts) != 1 || !s.P0SkippedKeys[task.Key] || s.Task().Key == task.Key {
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

func TestP0ViolationFallsBackToDeleteRepair(t *testing.T) {
	s := p0Fixture(t, 2)
	// 局部重写已移除：P0 违规直接走程序删词兜底（deleteRepair），能删干净就采纳。
	// 用「含一句正常 + 一句违规」的文本，删掉违规词后应正常采纳。
	c := candidate("老乡，我们家在这个镇上做油啊。零风险。", 100)
	adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{})
	if err != nil {
		t.Fatal(err)
	}
	if !adopted {
		t.Fatal("delete repair should salvage the safe remainder", s)
	}
	if len(s.Accepted) != 1 || strings.Contains(s.Accepted[0].Selected.Text, "零风险") {
		t.Fatal("salvaged text still violates", s)
	}
	if s.P0RewritesUsed != 0 || s.P0PendingRepair != nil {
		t.Fatal("rewrite budget/pending should not be set after salvage", s.P0RewritesUsed, s.P0PendingRepair)
	}
}

func TestP0RollingExhaustedPoolAndConsecutiveSkipProtection(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Config.Rolling = true
	s.Config.PointGap = 0
	s.Config.CombinationGap = 0
	// 局部重写已移除：每次 P0 违规直接跳过。2 个卖点全部跳过 → 轮盘耗尽 → compliance_blocked。
	for i := 0; i < 8; i++ {
		if s.Status == "compliance_blocked" {
			break
		}
		rejectP0Unfixable(t, &s)
	}
	if s.Status != "compliance_blocked" || len(s.P0Skipped) == 0 {
		t.Fatal("exhausted pool did not stop or lost skip trace", s.Status, len(s.P0Skipped))
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
	// 局部重写已移除：output 伪造 P0 report 绕过门禁时，应直接跳过而非调度重写。
	if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted || s.P0PendingRepair != nil || len(s.P0Skipped) == 0 {
		t.Fatal("output or forged report bypassed gate", err, s)
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

func TestBaseP0AllowancesOnlyReleaseTimeOrdinalContexts(t *testing.T) {
	state := p0Fixture(t, 1)
	policy, err := contentguard.Compile(state.P0PolicyMD)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		text string
		pass bool
	}{
		{"今天第一天开，欢迎朋友们。", true},
		{"第一口先尝这个味道。", true},
		{"第一次来直播间，先看看。", true},
		{"第一年做这个，慢慢来。", true},
		{"第一步先解冻。", true},
		{"最好当天吃完。", true},
		{"最好提前预约。", true},
		{"最好先问一下。", true},
		{"最好冷藏保存。", true},
		{"最好加热再吃。", true},
		{"最高温度零下十八度。", true},
		{"最大程度保留原味。", true},
		{"全网第一的品牌。", false},
		{"第一天销量第一。", false},
		{"第一天第一品牌。", false},
		{"我们是第一品牌。", false},
		{"这是最好的产品。", false},
		{"最高品质。", false},
		{"最大优惠。", false},
		{"最低价。", false},
		{"最好品牌。", false},
	}
	for _, c := range cases {
		report := state.scanP0(policy, c.text)
		if report.Passed != c.pass {
			t.Fatalf("%q passed=%t, want %t hits=%+v", c.text, report.Passed, c.pass, report.Hits)
		}
	}
}
