package speechruntime

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/speechexpander"
)

func TestLengthDebtRollsIntoLaterUnitsAndClosingTightens(t *testing.T) {
	ledger := NewLedger(600)
	step := model.LiveSpeechExpansionStep{Stage: "fact_direct", FactKeys: []string{"origin"}, ExpressionMoves: []string{"直述", "问后自答"}}
	first := ledger.Next(step, 0, 3)
	if first.TargetChars != 200 || first.ConstraintLevel != "soft" {
		t.Fatalf("first=%+v", first)
	}
	if first.SegmentRole != "opening" || !first.OpeningAllowed || first.ClosingAllowed || first.ContinuationMode != "fresh_open" {
		t.Fatalf("opening contract=%+v", first)
	}
	if len(first.PreviouslyCoveredFacts) != 0 {
		t.Fatalf("opening invented covered facts: %+v", first.PreviouslyCoveredFacts)
	}
	id := ledger.Enqueue(first, strings.Repeat("讲", 150))
	if ledger.CommittedChars() != 0 {
		t.Fatal("pending text leaked into committed memory")
	}
	if !ledger.Commit(id) {
		t.Fatal("commit failed")
	}
	second := ledger.Next(step, 1, 3)
	if second.TargetChars <= 200 || second.ConstraintLevel != "tight" || len(second.AvoidRecent) == 0 {
		t.Fatalf("debt/cooldown missing: %+v", second)
	}
	if second.SegmentRole != "middle" || second.OpeningAllowed || second.ClosingAllowed || second.PrimaryFactKey != "origin" {
		t.Fatalf("middle contract=%+v", second)
	}
	if len(second.PreviouslyCoveredFacts) != 1 || second.PreviouslyCoveredFacts[0] != "origin" {
		t.Fatalf("covered facts missing: %+v", second.PreviouslyCoveredFacts)
	}
	id = ledger.Enqueue(second, strings.Repeat("说", 225))
	ledger.Commit(id)
	last := ledger.Next(step, 2, 3)
	if last.TargetChars != 223 || last.ConstraintLevel != "closing" || last.FinishMode != "close" {
		t.Fatalf("last=%+v committed=%d", last, ledger.CommittedChars())
	}
	if last.SegmentRole != "closing" || last.OpeningAllowed || !last.ClosingAllowed || last.NewcomerReentryAllowed {
		t.Fatalf("closing contract=%+v", last)
	}
}

func TestRecentConversationalMarkersBecomeAntiChecklistHints(t *testing.T) {
	ledger := NewLedger(400)
	step := model.LiveSpeechExpansionStep{Stage: "fact_direct", FactKeys: []string{"origin"}, ExpressionMoves: []string{"直述"}}
	first := ledger.Next(step, 0, 2)
	id := ledger.Enqueue(first, "哥哥姐姐们，嗯，这个重点先讲清楚。对呀，事实就是这样。")
	if !ledger.Commit(id) {
		t.Fatal("commit failed")
	}
	second := ledger.Next(step, 1, 2)
	hints := strings.Join(second.AvoidRecent, "\n")
	if !strings.Contains(hints, "口头标记“嗯”") || !strings.Contains(hints, "口头标记“对呀”") {
		t.Fatalf("marker cooldown missing: %s", hints)
	}
	issues := ledger.AntiChecklistIssues(second, "嗯，这个事情接着讲。对呀，情况就是这样。")
	if len(issues) == 0 {
		t.Fatal("adjacent marker checklist was not rejected")
	}
	if issues := ledger.AntiChecklistIssues(second, "嗯，这个事情接着讲。后面还有一个重点。"); len(issues) != 0 {
		t.Fatalf("one natural repeated marker should be allowed: %v", issues)
	}
}

func TestFourHourFiniteFactSimulationStaysContinuous(t *testing.T) {
	const target = 4 * 60 * 250
	plans := speechexpander.BuildFixedPlans(speechexpander.Input{
		DurationMinutes: 240,
		TargetChars:     target,
		VariantCount:    1,
		FactKeys:        []string{"产地", "规格", "发货"},
		BenefitKeys:     []string{"活动"},
		LinkKeys:        []string{"1号链接"},
	})
	if len(plans) != 1 || len(plans[0].Steps) != 240 {
		t.Fatalf("unexpected four-hour plan: %+v", plans)
	}
	ledger := NewLedger(target)
	cooldownSignals := 0
	for index, step := range plans[0].Steps {
		spec := ledger.Next(step, index, len(plans[0].Steps))
		if spec.TargetChars <= 0 {
			t.Fatalf("unit %d has no remaining budget", index+1)
		}
		cooldownSignals += len(spec.AvoidRecent)
		id := ledger.Enqueue(spec, strings.Repeat("讲", spec.TargetChars))
		if !ledger.Commit(id) {
			t.Fatalf("unit %d did not commit", index+1)
		}
	}
	chars := ledger.CommittedChars()
	if chars < target-target/100 || chars > target+target/100 {
		t.Fatalf("four-hour chars=%d target=%d", chars, target)
	}
	if len(ledger.Records()) != 240 || cooldownSignals == 0 {
		t.Fatalf("records=%d cooldown=%d", len(ledger.Records()), cooldownSignals)
	}
}

func TestDiscardedPendingTextNeverChangesMemory(t *testing.T) {
	ledger := NewLedger(200)
	spec := ledger.Next(model.LiveSpeechExpansionStep{Stage: "bridge"}, 0, 2)
	id := ledger.Enqueue(spec, strings.Repeat("待", 80))
	if !ledger.Discard(id) || ledger.CommittedChars() != 0 || ledger.CommittedText() != "" {
		t.Fatalf("discard failed: %+v", ledger.Records())
	}
}
