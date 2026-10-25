package httpapi

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRecordingUpdateInventoryFewerVersionsPrecedeOlderFullerSource(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", Refreshed: -100, VersionCount: 3, InventoryKnown: true, UpdateEligible: true},
		{Source: "乙", Refreshed: 100, VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
	}
	planner.Plan("tab", "initialize", states, 0, nil)
	credit := planner.Tabs["tab"]
	credit.SentenceFreshAt = []int64{1, 2_000}
	credit.SentenceUpdated = []uint64{0, 100}
	planner.Tabs["tab"] = credit
	plan := planner.PlanBounded("tab", "round:1", states, 50, 50, nil)
	if !reflect.DeepEqual(plan.Selected, []bool{false, true}) || !reflect.DeepEqual(plan.order, []int{1, 0}) {
		t.Fatalf("one-version source lost priority to three versions: selected=%v order=%v", plan.Selected, plan.order)
	}
}

func TestRecordingUpdateInventoryExcludesIneligibleInitialAndReplacementWork(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
		{Source: "乙", VersionCount: 4, InventoryKnown: true},
		{Source: "丙", VersionCount: 0, InventoryKnown: true},
		{Source: "丁", VersionCount: 2, InventoryKnown: true, UpdateEligible: true},
	}
	plan := planner.PlanBounded("tab", "round:1", states, 25, 25, nil)
	if !reflect.DeepEqual(plan.Selected, []bool{true, false, false, false}) || !reflect.DeepEqual(plan.order, []int{0, 3}) || plan.maxAttempts != 2 {
		t.Fatalf("ineligible inventory affected the initial plan: %+v", plan)
	}
	// Character debt still derives from all four source characters; filtering
	// eligible work must neither create extra credit nor shrink the source base.
	if planner.Tabs["tab"].CreditMicros != recordingUpdateCreditScale {
		t.Fatal("inventory filtering changed the accrual base", planner.Tabs["tab"])
	}
	if !planner.BeginAttempt(plan, 0) {
		t.Fatal("initial eligible attempt was fenced")
	}
	if added := planner.ReplaceFailed(plan, 0, 1); !reflect.DeepEqual(added, []int{3}) {
		t.Fatalf("replacement bypassed filtered inventory order: %v", added)
	}
	if plan.Selected[1] || plan.Selected[2] || planner.BeginAttempt(plan, 1) || planner.BeginAttempt(plan, 2) {
		t.Fatal("known ineligible full/empty inventory was scheduled")
	}
	if !planner.BeginAttempt(plan, 3) || !planner.Commit(plan, 3) || planner.Tabs["tab"].CreditMicros != 0 {
		t.Fatal("eligible replacement did not spend exactly its successful character debt")
	}
}

func TestRecordingUpdateInventoryAllIneligibleProducesNoAttempts(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", VersionCount: 0, InventoryKnown: true},
		{Source: "乙", VersionCount: 4, InventoryKnown: true},
	}
	plan := planner.PlanBounded("tab", "round:1", states, 100, 25, nil)
	if len(plan.order) != 0 || plan.maxAttempts != 0 || plan.SingleLong || plan.Selected[0] || plan.Selected[1] || planner.BeginAttempt(plan, 0) {
		t.Fatalf("no eligible inventory still produced optional work: %+v", plan)
	}
	if planner.Tabs["tab"].CreditMicros != 2*recordingUpdateCreditScale {
		t.Fatal("no-attempt plan discarded or fabricated source debt")
	}
}

func TestRecordingUpdateInventoryAllFullStillRotatesWhenEligible(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", VersionCount: 4, InventoryKnown: true, UpdateEligible: true},
		{Source: "乙", VersionCount: 4, InventoryKnown: true, UpdateEligible: true},
	}
	first := planner.PlanBounded("tab", "round:1", states, 50, 50, nil)
	if !reflect.DeepEqual(first.Selected, []bool{true, false}) || !planner.CommitAt(first, 0, time.UnixMilli(1000)) {
		t.Fatal("full inventory was permanently stopped", first)
	}
	second := planner.PlanBounded("tab", "round:2", states, 50, 50, nil)
	if !reflect.DeepEqual(second.Selected, []bool{false, true}) || !planner.CommitAt(second, 1, time.UnixMilli(2000)) {
		t.Fatal("full inventory did not rotate to its older source", second)
	}
}

func TestRecordingUpdateInventoryIneligibleFirstSourceCannotBlockSingleLong(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: strings.Repeat("甲", 200), VersionCount: 0, InventoryKnown: true},
		{Source: strings.Repeat("乙", 80), VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
	}
	plan := planner.PlanBounded("tab", "round:1", states, 100, 25, nil)
	if !plan.SingleLong || !reflect.DeepEqual(plan.Selected, []bool{false, true}) || !reflect.DeepEqual(plan.order, []int{1}) || plan.maxAttempts != 1 {
		t.Fatalf("ineligible source blocked the one eligible long sentence: %+v", plan)
	}
}

func TestRecordingUpdateInventorySameCountKeepsFreshnessPriority(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", Refreshed: 0, VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
		{Source: "乙", Refreshed: 100, VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
	}
	planner.Plan("tab", "initialize", states, 0, nil)
	credit := planner.Tabs["tab"]
	credit.SentenceFreshAt = []int64{2000, 1}
	credit.SentenceUpdated = []uint64{0, 100}
	planner.Tabs["tab"] = credit
	plan := planner.PlanBounded("tab", "round:1", states, 50, 50, nil)
	if !reflect.DeepEqual(plan.Selected, []bool{false, true}) {
		t.Fatal("same-count inventory ignored its oldest update time", plan.Selected)
	}
}

func TestRecordingUpdateInventoryPlanOwnsCrossTabSnapshots(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{
		{Source: "甲", VersionCount: 1, InventoryKnown: true, UpdateEligible: true},
		{Source: "乙", VersionCount: 3, InventoryKnown: true, UpdateEligible: true},
		{Source: "丙", VersionCount: 99}, // Unknown inventory is a zero-count compatibility snapshot.
	}
	planner.Plan("tab", "initialize", states, 0, nil)
	credit := planner.Tabs["tab"]
	credit.SentenceFreshAt = []int64{100, 200, 300}
	planner.Tabs["tab"] = credit
	plan := planner.PlanBounded("tab", "round:1", states, 100, 100, nil)
	if !reflect.DeepEqual(plan.versions, []int{1, 3, 0}) || !reflect.DeepEqual(plan.freshAt, []int64{100, 200, 300}) {
		t.Fatalf("cross-tab inventory/freshness snapshot missing: versions=%v freshAt=%v", plan.versions, plan.freshAt)
	}
	states[0].VersionCount = 4
	credit = planner.Tabs["tab"]
	credit.SentenceFreshAt[1] = 900
	planner.Tabs["tab"] = credit
	if !planner.CommitAt(plan, 0, time.UnixMilli(1000)) {
		t.Fatal("fixture commit did not succeed")
	}
	if !reflect.DeepEqual(plan.versions, []int{1, 3, 0}) || !reflect.DeepEqual(plan.freshAt, []int64{100, 200, 300}) {
		t.Fatal("state mutation or a later commit rewrote a plan's ordering snapshot")
	}
}
