package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestPurposeDependsOnRealHeatAndMainlineProgress(t *testing.T) {
	cfg, err := interactionpolicy.Load()
	if err != nil {
		t.Fatal(err)
	}
	rule := cfg.Events["welcome"]
	cold, angle, err := chooseContentGoal(rule, "COLD", "opening", nil, "welcome")
	if err != nil {
		t.Fatal(err)
	}
	if (cold.Key != "discover_need" && cold.Key != "hook_welcome") || cold.Action == "" || angle == "" {
		t.Fatal("cold welcome has no purpose")
	}
	selection, _, _ := chooseContentGoal(rule, "WARM", "selection", nil, "welcome")
	if selection.Key != "resolve_choice" {
		t.Fatal("selection welcome not contextual")
	}
	hot, _, _ := chooseContentGoal(rule, "HOT", "explanation", nil, "welcome")
	if hot.Key != "brief_orientation" {
		t.Fatal("hot room should use short orientation")
	}
	history := []playedContent{{ContentGoal: cold.Key, ContentAngle: angle, Scene: "welcome"}}
	_, other, _ := chooseContentGoal(rule, "COLD", "opening", history, "welcome")
	if other == angle {
		t.Fatal("actual spoken angle repeated")
	}
}

func TestInteractionGenerationContainsPurposeAndSourceBoundary(t *testing.T) {
	w := New(readyVoiceStore(), nil, nil, nil)
	guidance, goal, angle, err := w.interactionContentGuidance(context.Background(), model.LiveRuntimeSession{RoomID: 7, TenantID: 2}, decisionItem{MissionKind: "conversion_signal", OrderSource: "viewer_claim", CurrentMainline: "现在讲规格区别"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"viewer_claim", "期望观众行动", "已生效纠正规则", "不当作后台订单确认"} {
		if !strings.Contains(guidance, s) {
			t.Fatalf("missing executable constraint %s", s)
		}
	}
	if goal.Key == "" || angle == "" {
		t.Fatal("generation has no concrete goal")
	}
}
