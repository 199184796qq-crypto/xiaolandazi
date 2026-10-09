package httpapi

import (
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestSavedProductFocusAcceptsLabelChangeWithoutMutatingVersion(t *testing.T) {
	old := "- [other/算账口径-老鸭] 1号老鸭85.5元；不承诺整桌不到90元"
	current := "- [other/算账] 1号老鸭85.5元；不承诺整桌不到90元"
	saved := &anchorcycle.ProductFocus{FactsSHA: derivedHash(old), Facts: []anchorcycle.ScopedFact{{Text: old, Links: []string{"1"}}}}
	focus, err := savedProductFocusForFacts(saved, current)
	if err != nil || len(focus.Facts) != 1 || focus.Facts[0].Text != current || focus.Facts[0].Links[0] != "1" || focus.FactsSHA != derivedHash(current) {
		t.Fatalf("compatible label change rejected: %+v %v", focus, err)
	}
	focus.Facts[0].Links[0] = "2"
	if saved.Facts[0].Text != old || saved.Facts[0].Links[0] != "1" || saved.FactsSHA != derivedHash(old) {
		t.Fatal("saved version was mutated")
	}
}

func TestSavedProductFocusCannotTransferChangedClaimsOrAmbiguousLabels(t *testing.T) {
	cases := []struct{ old, current string }{
		{"- [other/算账] 1号老鸭85.5元", "- [other/算账] 1号老鸭47.5元"},
		{"- [other/算账] 1号老鸭85.5元", "- [other/算账] 2号老鸭85.5元"},
		{"- [other/算账] 1号老鸭85.5元；仅今天", "- [other/算账] 1号老鸭85.5元"},
		{"- [other/旧] 已确认事实", "- [other/新甲] 已确认事实\n- [other/新乙] 已确认事实"},
	}
	for _, c := range cases {
		saved := &anchorcycle.ProductFocus{FactsSHA: derivedHash(c.old), Facts: []anchorcycle.ScopedFact{{Text: c.old, Links: []string{"1"}}}}
		focus, err := savedProductFocusForFacts(saved, c.current)
		if err != nil || len(focus.Facts) != 0 {
			t.Fatalf("changed claim inherited an old product scope: %+v %v", focus, err)
		}
	}
}

func TestSavedPlanDifferencesAreNonblockingWorkspaceNotices(t *testing.T) {
	old, current := "- [other/旧] 1号85.5元", "- [other/新] 1号85.5元"
	version := &model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": model.LiveContentAIDynamic, "anchor_generation": liveAnchorScheme{Version: 1, StyleID: 1, ScorecardID: 2, WheelID: 3, ProductFocus: &anchorcycle.ProductFocus{FactsSHA: derivedHash(old), Facts: []anchorcycle.ScopedFact{{Text: old, Links: []string{"1"}}}}}}}
	safety := model.LiveDerivedSafetyAsset{OriginalFacts: current, OriginalWheelJSON: "saved wheel", SafeFacts: current}
	checks := savedPlanDifferenceChecks(version, safety, &derivedInput{Facts: "next version facts", WheelJSON: "next wheel"})
	if len(checks) != 2 || checks[0].Code != "plan_sources_changed" || checks[1].Code != "product_focus_snapshot_changed" {
		t.Fatalf("missing workspace notices: %+v", checks)
	}
	focus, err := savedProductFocusForFacts(version.GenerationContext["anchor_generation"].(liveAnchorScheme).ProductFocus, safety.SafeFacts)
	if err != nil || focus.Facts[0].Text != current {
		t.Fatal("source edit must not change the selected saved facts")
	}
}
