package anchorcycle

import (
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/contentguard"
	"testing"
)

func TestContextualApprovalIsReusedByAdoptionAndAudio(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Surface = "formal_live"
	c := candidate("开袋后最好当天吃完。", 100)
	if err := s.CheckP0Candidate(&c); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyP0Context(&c, contentguard.ContextReview{Raw: `{"items":[{"hit_id":"h1","decision":"pass","reason":"真实食用提示"}],"instruction_leak":false}`}); err != nil {
		t.Fatal(err)
	}
	review := c.P0Report
	if ok, err := s.AddCandidate(c, anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal(ok, err)
	}
	adopted := s.Accepted[len(s.Accepted)-1].Selected
	if adopted.P0Report != review || s.CheckApprovedText(adopted, c.Text) != nil {
		t.Fatal("review lost or repeated before audio")
	}
	adopted.OutputText = "这是全网最低价。"
	if s.CheckApprovedText(adopted, adopted.OutputText) == nil {
		t.Fatal("old receipt approved different text")
	}
}

func TestApprovedFinalTextReceiptIsReusedAndMutationRequiresReview(t *testing.T) {
	s := p0Fixture(t, 2)
	c := candidate("零风险。", 100)
	c.OutputText = "这只鸭子可以按商品页规格选择。"
	if err := s.CheckP0Candidate(&c); err != nil || !c.P0Report.Passed {
		t.Fatal("raw discarded text was audited instead of final text", err)
	}
	report := c.P0Report
	if err := s.CheckP0Candidate(&c); err != nil || c.P0Report != report {
		t.Fatal("unchanged text reviewed twice", err)
	}
	if err := s.CheckApprovedText(c, c.OutputText); err != nil {
		t.Fatal(err)
	}
	c.OutputText = "零风险。"
	if s.CheckApprovedText(c, c.OutputText) == nil {
		t.Fatal("mutated voice accepted old receipt")
	}
	if err := s.CheckP0Candidate(&c); err != nil || c.P0Report.Passed {
		t.Fatal("changed text escaped review", err)
	}
}

func TestFormalLiveRepeatedP0FailuresSkipWithoutWholeRunStop(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Surface = "formal_live"
	s.Config.Rolling = true
	s.Config.PointGap = 0
	s.Config.CombinationGap = 0
	run := s.RunID
	// formal_live 下违规也走「换安全说法」局部重写（不再直接跳过），
	// 重写耗尽后才跳过；关键安全性质：反复违规不停止整个 run（不 compliance_blocked）。
	for i := 0; i < 4; i++ {
		rejectP0(t, &s)
		if s.Status != "ready" || s.Phase == "ended" || s.RunID != run || s.Uncertain {
			t.Fatal("review stopped or polluted formal session", i, s.Status, s.Phase)
		}
	}
	if len(s.P0Skipped) != 1 || len(s.Accepted) != 0 || s.P0PendingRepair != nil || s.Status != "ready" {
		t.Fatal("formal live should skip after rewrite exhaustion without stopping run")
	}
	if ok, err := s.AddCandidate(candidate("按商品页说明选择合适规格。", 100), anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal("valid next draft could not resume", err)
	}
}

func TestFormalLivePoorGenerationSkipsAndContinuesNextTask(t *testing.T) {
	s := p0Fixture(t, 2)
	s.Surface = "formal_live"
	s.Config.Rolling = true
	s.Config.PointGap = 0
	s.Config.CombinationGap = 0
	s.Config.MaxRetries = 0
	if ok, err := s.AddCandidate(candidate("这是一段不合风格的文字。", 1), anchorstylescore.Baseline{}); ok || err != nil {
		t.Fatal(ok, err)
	}
	if s.Status != "ready" || len(s.Accepted) != 0 || len(s.GenerationSkips) != 1 {
		t.Fatal("bad generation became fatal or best fallback")
	}
	if ok, err := s.AddCandidate(candidate("按商品页说明选择合适规格。", 100), anchorstylescore.Baseline{}); !ok || err != nil {
		t.Fatal(ok, err)
	}
}
