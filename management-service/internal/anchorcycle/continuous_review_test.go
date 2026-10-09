package anchorcycle

import (
	"livecompanion/management/internal/anchorstylescore"
	"testing"
)

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
	for i := 0; i < 20; i++ {
		if ok, err := s.AddCandidate(candidate("我们家的油零风险啊。", 100), anchorstylescore.Baseline{}); ok || err != nil {
			t.Fatal("rejected text adopted", err)
		}
		if s.Status != "ready" || s.Phase == "ended" || s.P0PendingRepair != nil || len(s.Accepted) != 0 || s.RunID != run || s.Uncertain {
			t.Fatal("review stopped or polluted formal session", i, s.Status, s.Phase)
		}
	}
	if len(s.GenerationSkips) != 20 || len(s.Used) != 0 || s.StyleWindow != "" {
		t.Fatal("skip trace or adoption memory incorrect")
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
