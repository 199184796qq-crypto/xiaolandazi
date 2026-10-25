package httpapi

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestRecordingUpdatePlannerShortTabRefreshesOncePerFourRounds(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "一句。", Refreshed: -1}}
	updates := 0
	for round := 1; round <= 8; round++ {
		plan := planner.Plan("stable-tab", fmt.Sprintf("session-a:%d", round), states, 25, nil)
		if plan.Selected[0] != (round%4 == 0) {
			t.Fatalf("round=%d selected=%v credit=%+v", round, plan.Selected, planner.Tabs["stable-tab"])
		}
		if plan.Selected[0] {
			if !planner.Commit(plan, 0) || planner.Commit(plan, 0) {
				t.Fatal("successful update must charge exactly once")
			}
			states[0].Refreshed = round
			updates++
		}
	}
	if updates != 2 || planner.Tabs["stable-tab"].CreditMicros != 0 {
		t.Fatal(updates, planner.Tabs)
	}
}

func TestRecordingUpdatePlannerFailedOrDeniedWorkDoesNotSpendAndDebtIsCapped(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "一句。", Refreshed: -1}}
	var last *recordingUpdatePlan
	for round := 1; round <= 100; round++ {
		last = planner.Plan("stable-tab", fmt.Sprintf("session-a:%d", round), states, 25, nil)
		// No fresh playable result: deliberately do not call Commit.
	}
	fullPass := int64(3) * recordingUpdateCreditScale
	if !last.Selected[0] || planner.Tabs["stable-tab"].CreditMicros != fullPass {
		t.Fatal("failed updates should retain at most one pass of debt", last, planner.Tabs)
	}
	blocked := planner.Plan("stable-tab", "session-a:101", states, 0, nil)
	if blocked.Selected[0] || planner.Commit(blocked, 0) || planner.Tabs["stable-tab"].CreditMicros != fullPass || planner.Commit(last, 0) {
		t.Fatal("denied updates spent credit or an obsolete plan remained valid", blocked, planner.Tabs)
	}
	resumed := planner.Plan("stable-tab", "session-a:102", states, 25, nil)
	if !resumed.Selected[0] || !planner.Commit(resumed, 0) || planner.Tabs["stable-tab"].CreditMicros != 0 {
		t.Fatal("unblocked work should refresh once, not burst through accumulated rounds", resumed, planner.Tabs)
	}
}

func TestRecordingUpdatePlannerDuplicateRoundDoesNotAccrueAgain(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "一句。", Refreshed: -1}}
	for retry := 0; retry < 12; retry++ {
		plan := planner.Plan("stable-tab", "same-session:same-round", states, 25, nil)
		if plan.Selected[0] || planner.Tabs["stable-tab"].CreditMicros != 750_000 {
			t.Fatal("retry accrued the same round more than once", plan, planner.Tabs)
		}
	}
}

func TestRecordingUpdatePlannerUsesCharactersAndDoesNotStarveOldLongSentence(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: strings.Repeat("长", 80), Refreshed: -1},
		{Source: strings.Repeat("短", 20), Refreshed: 0},
	}
	for round := 1; round <= 4; round++ {
		plan := planner.Plan("tab", fmt.Sprintf("session:%d", round), states, 25, rand.New(rand.NewSource(4)))
		if round < 4 {
			if plan.Selected[0] || plan.Selected[1] {
				t.Fatal("short newer sentence consumed the oldest long sentence's balance", round, plan.Selected)
			}
		} else {
			if !plan.Selected[0] || !plan.Selected[1] || !planner.Commit(plan, 0) || !planner.Commit(plan, 1) {
				t.Fatal("100 source characters at 25% need one complete pass in four rounds", plan, planner.Tabs)
			}
		}
	}
	if planner.Tabs["tab"].CreditMicros != 0 {
		t.Fatal(planner.Tabs)
	}
}

func TestRecordingUpdatePlannerPartialSuccessChargesOnlySuccessfulSentence(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "甲", Refreshed: -1}, {Source: "乙", Refreshed: -1}}
	plan := planner.Plan("tab", "session:1", states, 100, nil)
	if !plan.Selected[0] || !plan.Selected[1] || !planner.Commit(plan, 1) || planner.Commit(plan, 1) || planner.Commit(plan, -1) || planner.Commit(plan, 2) {
		t.Fatal(plan, planner.Tabs)
	}
	if planner.Tabs["tab"].CreditMicros != recordingUpdateCreditScale {
		t.Fatal("failed first sentence was charged", planner.Tabs)
	}
	states[1].Refreshed = 1
	retry := planner.Plan("tab", "session:1", states, 100, nil)
	if !retry.Selected[0] || retry.Selected[1] || !planner.Commit(retry, 0) || planner.Tabs["tab"].CreditMicros != 0 {
		t.Fatal("same-round retry should use the unpaid remainder only", retry, planner.Tabs)
	}
}

func TestRecordingUpdatePlannerStableTabIDSurvivesJSONAndPlaybackReorder(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "一句。", Refreshed: -1}}
	for round := 1; round <= 3; round++ {
		planner.Plan("tab-a", fmt.Sprintf("session-a:%d", round), states, 25, nil)
	}
	planner.Plan("tab-b", "session-a:3", states, 0, nil)
	raw, err := json.Marshal(planner)
	if err != nil {
		t.Fatal(err)
	}
	var restored recordingUpdatePlanner
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if first := restored.Plan("tab-b", "session-b:1", states, 25, nil); first.Selected[0] {
		t.Fatal("first visited tab borrowed the former first tab's balance")
	}
	if second := restored.Plan("tab-a", "session-b:1", states, 25, nil); !second.Selected[0] || !restored.Commit(second, 0) {
		t.Fatal("stable ID lost its fractional balance", second, restored.Tabs)
	}
	if restored.Tabs["tab-a"].CreditMicros != 0 || restored.Tabs["tab-b"].CreditMicros != 750_000 {
		t.Fatal(restored.Tabs)
	}
}

func TestRecordingUpdatePlannerContentChangeClearsOnlyChangedTabsBalance(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "旧句。", Refreshed: -1}}
	for round := 1; round <= 3; round++ {
		planner.Plan("tab-a", fmt.Sprintf("session:%d", round), states, 25, nil)
		planner.Plan("tab-b", fmt.Sprintf("session:%d", round), states, 25, nil)
	}
	states[0].Source = "新句。"
	changed := planner.Plan("tab-a", "session:4", states, 25, nil)
	if changed.Selected[0] || planner.Tabs["tab-a"].CreditMicros != 750_000 || planner.Tabs["tab-b"].CreditMicros != 2_250_000 {
		t.Fatal("content revision reused stale credit or changed another tab", changed, planner.Tabs)
	}
}

func TestRecordingUpdatePlannerPersistsSuccessfulSentenceFairnessAcrossSessions(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: "甲", Refreshed: -1}, {Source: "乙", Refreshed: -1}}
	first := planner.Plan("tab", "session-a:1", states, 50, nil)
	if !first.Selected[0] || first.Selected[1] || !planner.Commit(first, 0) {
		t.Fatal(first, planner.Tabs)
	}
	raw, err := json.Marshal(planner)
	if err != nil {
		t.Fatal(err)
	}
	var restored recordingUpdatePlanner
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	// New producer memory has both Refreshed fields reset. The persisted
	// success sequence still gives the never-updated second sentence priority.
	second := restored.Plan("tab", "session-b:1", states, 50, nil)
	if second.Selected[0] || !second.Selected[1] || !restored.Commit(second, 1) {
		t.Fatal("restart forgot successful updates and repeated the first sentence", second, restored.Tabs)
	}
	credit := restored.Tabs["tab"]
	if credit.UpdateSequence != 2 || credit.SentenceUpdated[0] != 1 || credit.SentenceUpdated[1] != 2 {
		t.Fatal(credit)
	}
	failed := restored.Plan("tab", "session-b:2", states, 50, nil)
	if !failed.Selected[0] || failed.Selected[1] {
		t.Fatal(failed)
	}
	if unchanged := restored.Tabs["tab"]; unchanged.UpdateSequence != 2 || unchanged.SentenceUpdated[0] != 1 {
		t.Fatal("planning a failed update recorded a false success", unchanged)
	}
}
