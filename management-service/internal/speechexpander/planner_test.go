package speechexpander

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestBuildFixedPlansUsesVirtualTimeAndExactCharacterBudget(t *testing.T) {
	plans := BuildFixedPlans(Input{DurationMinutes: 5, TargetChars: 1250, VariantCount: 3, FactKeys: []string{"产地", "规格", "快递"}, BenefitKeys: []string{"活动"}, LinkKeys: []string{"1号链接"}})
	if len(plans) != 3 {
		t.Fatalf("plans=%d", len(plans))
	}
	for _, plan := range plans {
		if plan.Version != model.LiveSpeechExpansionVersion || plan.Mode != "fixed_simulation" || plan.DurationSeconds != 300 {
			t.Fatalf("bad plan header: %+v", plan)
		}
		if len(plan.Steps) != 5 || plan.Steps[0].StartSecond != 0 || plan.Steps[len(plan.Steps)-1].EndSecond != 300 {
			t.Fatalf("bad virtual timeline: %+v", plan.Steps)
		}
		total := 0
		for _, step := range plan.Steps {
			total += step.TargetChars
			if len(step.ExpressionMoves) != 2 || len(step.FactKeys) == 0 || len(step.FactKeys) > 1 || step.Room.Scenario == "" {
				t.Fatalf("incomplete step: %+v", step)
			}
		}
		if plan.Steps[len(plan.Steps)-1].Stage == "reentry" {
			t.Fatalf("closing unit restarted the show: %+v", plan.Steps[len(plan.Steps)-1])
		}
		if total != 1250 {
			t.Fatalf("character budget=%d", total)
		}
	}
	if plans[0].Steps[0].Room.Scenario == plans[1].Steps[0].Room.Scenario {
		t.Fatal("variants should exercise different simulated room trajectories")
	}
}

func TestBuildFixedPlansNeverCreatesBusinessKeys(t *testing.T) {
	plans := BuildFixedPlans(Input{DurationMinutes: 1, VariantCount: 1})
	if len(plans) != 1 || len(plans[0].Steps) == 0 {
		t.Fatal("missing fallback plan")
	}
	for _, step := range plans[0].Steps {
		if len(step.FactKeys) != 0 || len(step.BenefitKeys) != 0 || len(step.LinkKeys) != 0 {
			t.Fatalf("planner invented authorized content: %+v", step)
		}
	}
}

func TestAssignStyleOverlaysSpreadsCapabilitiesAcrossTimeUnits(t *testing.T) {
	plans := BuildFixedPlans(Input{DurationMinutes: 5, TargetChars: 1250, VariantCount: 1, FactKeys: []string{"产地"}})
	profile := model.LiveAgentPlanStyleOverlayProfile{Items: []model.LiveAnchorStyleOverlayItem{
		{ID: "hesitation", Enabled: true, Rule: model.LiveAnchorStyleOverlayRule{
			Label: "自然口结与停顿", Application: "occasional", Strength: 100,
			MainlineMaxPer1000Chars: 2,
		}},
		{ID: "reduplication", Enabled: true, Rule: model.LiveAnchorStyleOverlayRule{
			Label: "自然叠词", Application: "occasional", Strength: 100,
			MainlineMaxPer1000Chars: 1,
		}},
	}}
	plans = AssignStyleOverlays(plans, profile)
	counts := map[string]int{}
	for _, step := range plans[0].Steps {
		if len(step.StyleCapabilities) > 1 {
			t.Fatalf("occasional capabilities stacked in one unit: %+v", step)
		}
		for _, capability := range step.StyleCapabilities {
			counts[capability]++
		}
	}
	if counts["自然口结与停顿"] != 3 {
		t.Fatalf("hesitation count=%d, want 3", counts["自然口结与停顿"])
	}
	if counts["自然叠词"] != 2 {
		t.Fatalf("reduplication count=%d, want 2", counts["自然叠词"])
	}
}

func TestBuildFixedPlansSchedulesBenefitsAndLinksSparsely(t *testing.T) {
	plans := BuildFixedPlans(Input{
		DurationMinutes: 8, TargetChars: 2000, VariantCount: 1,
		FactKeys: []string{"产地", "规格", "物流"}, BenefitKeys: []string{"试吃"}, LinkKeys: []string{"1号链接"},
	})
	if len(plans) != 1 {
		t.Fatalf("plans=%d", len(plans))
	}
	benefitUnits, linkUnits := 0, 0
	for _, step := range plans[0].Steps {
		if len(step.BenefitKeys) > 0 {
			benefitUnits++
		}
		if len(step.LinkKeys) > 0 {
			linkUnits++
		}
	}
	if benefitUnits != 1 {
		t.Fatalf("benefit units=%d, want 1 dedicated unit", benefitUnits)
	}
	if linkUnits == 0 || linkUnits >= len(plans[0].Steps)/2 {
		t.Fatalf("links were not scheduled sparsely: units=%d total=%d", linkUnits, len(plans[0].Steps))
	}
	steps := plans[0].Steps
	if steps[len(steps)-2].InteractionOpportunity || steps[len(steps)-1].InteractionOpportunity {
		t.Fatalf("finite plan opened an interaction immediately before closing: %+v", steps[len(steps)-2:])
	}
}
