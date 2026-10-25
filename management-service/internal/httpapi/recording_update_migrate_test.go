package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"livecompanion/management/internal/recordingcache"
)

func migrationStates(sources []string) []recordingSentenceState {
	states := make([]recordingSentenceState, len(sources))
	for index, source := range sources {
		states[index] = recordingSentenceState{Source: source, Refreshed: -1}
	}
	return states
}

func TestRecordingUpdateMigrateMatchesSourceIdentityRatherThanNewPosition(t *testing.T) {
	oldSources := []string{"甲句。", "乙句。", "丙句。", "将删除。", "重复。", "重复。"}
	newSources := []string{"丙句。", "甲句改写。", "乙句。", "新增。", "重复。"}
	old := recordingUpdateCredit{ContentKey: recordingcache.ScriptHash(oldSources), CreditMicros: 9 * recordingUpdateCreditScale,
		LastAccrualKey: "479:3", Revision: 8, UpdateSequence: 12, SentenceUpdated: []uint64{7, 8, 9, 10, 11, 12}}
	planner := recordingUpdatePlanner{Version: 1, Tabs: map[string]recordingUpdateCredit{"tab-a": old}}
	if !planner.MigrateTab("tab-a", oldSources, newSources) {
		t.Fatal("existing stable tab was not migrated")
	}
	next := planner.Tabs["tab-a"]
	if !reflect.DeepEqual(next.SentenceUpdated, []uint64{9, 0, 8, 0, 0}) || next.ContentKey != recordingcache.ScriptHash(newSources) ||
		next.CreditMicros != old.CreditMicros || next.LastAccrualKey != old.LastAccrualKey || next.UpdateSequence != 12 || next.Revision != 9 || planner.Version != 1 {
		t.Fatalf("edit/reorder borrowed a removed or ambiguous source's update history: %+v", next)
	}
	if !reflect.DeepEqual(old.SentenceUpdated, []uint64{7, 8, 9, 10, 11, 12}) {
		t.Fatal("migration mutated the previous source snapshot")
	}
	// The remapped state survives the same JSON boundary as a cache manifest.
	raw, err := json.Marshal(planner)
	var restored recordingUpdatePlanner
	if err != nil || json.Unmarshal(raw, &restored) != nil || !reflect.DeepEqual(restored.Tabs, planner.Tabs) {
		t.Fatal("migrated planning state was not durable", string(raw), err)
	}
}

func TestRecordingUpdateMigrateDuplicateSourcesAreConservativelyReset(t *testing.T) {
	for _, tc := range []struct {
		name          string
		old, current  []string
		history, want []uint64
	}{
		{"old_duplicate", []string{"重复。", "重复。", "唯一。"}, []string{"重复。", "唯一。"}, []uint64{1, 2, 3}, []uint64{0, 3}},
		{"new_duplicate", []string{"唯一。", "另一句。"}, []string{"唯一。", "唯一。", "另一句。"}, []uint64{1, 2}, []uint64{0, 0, 2}},
		{"unchanged_duplicates", []string{"重复。", "重复。"}, []string{"重复。", "重复。"}, []uint64{1, 2}, []uint64{1, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := recordingUpdatePlanner{Tabs: map[string]recordingUpdateCredit{"tab": {
				ContentKey: recordingcache.ScriptHash(tc.old), CreditMicros: recordingUpdateCreditScale,
				SentenceUpdated: tc.history, UpdateSequence: 3,
			}}}
			planner.MigrateTab("tab", tc.old, tc.current)
			if got := planner.Tabs["tab"].SentenceUpdated; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ambiguous source identity inherited history: got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestRecordingUpdateMigrateUnavailableOrUntrustedSnapshotOnlyKeepsExactTab(t *testing.T) {
	for _, tc := range []struct {
		name              string
		previous, current []string
		wantCredit        int64
		wantHistory       []uint64
		wantSequence      uint64
	}{
		{"missing_unchanged", nil, []string{"旧句。"}, recordingUpdateCreditScale, []uint64{6}, 6},
		{"wrong_snapshot_unchanged", []string{"不是旧正文。"}, []string{"旧句。"}, recordingUpdateCreditScale, []uint64{6}, 6},
		{"missing_changed", nil, []string{"新句。"}, 0, []uint64{0}, 0},
		{"wrong_snapshot_changed", []string{"不是旧正文。"}, []string{"新句。"}, 0, []uint64{0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := recordingUpdatePlanner{Version: 1, Tabs: map[string]recordingUpdateCredit{"tab": {
				ContentKey: recordingcache.ScriptHash([]string{"旧句。"}), CreditMicros: recordingUpdateCreditScale,
				LastAccrualKey: "479:2", Revision: 4, SentenceUpdated: []uint64{6}, UpdateSequence: 6,
			}}}
			planner.MigrateTab("tab", tc.previous, tc.current)
			next := planner.Tabs["tab"]
			if next.CreditMicros != tc.wantCredit || !reflect.DeepEqual(next.SentenceUpdated, tc.wantHistory) || next.UpdateSequence != tc.wantSequence || next.Revision != 5 || next.LastAccrualKey != "479:2" || next.ContentKey != recordingcache.ScriptHash(tc.current) {
				t.Fatal("untrusted snapshot was migrated as known source identity", next)
			}
		})
	}
}

func TestRecordingUpdateMigrateNeverAccruesCreditAndCapsToNewTabSize(t *testing.T) {
	for _, tc := range []struct {
		name             string
		old, current     []string
		credit, expected int64
	}{
		{"longer", []string{"旧句。"}, []string{"旧句。", "新增长句。"}, recordingUpdateCreditScale, recordingUpdateCreditScale},
		{"shorter", []string{"原来长句。"}, []string{"新。"}, 5 * recordingUpdateCreditScale, 2 * recordingUpdateCreditScale},
		{"empty", []string{"旧句。"}, []string{}, 2 * recordingUpdateCreditScale, 0},
		{"negative", []string{"旧句。"}, []string{"旧句。", "新增。"}, -1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := recordingUpdatePlanner{Tabs: map[string]recordingUpdateCredit{"tab": {
				ContentKey: recordingcache.ScriptHash(tc.old), CreditMicros: tc.credit, LastAccrualKey: "already-accrued",
			}}}
			planner.MigrateTab("tab", tc.old, tc.current)
			if planner.Tabs["tab"].CreditMicros != tc.expected || planner.Tabs["tab"].LastAccrualKey != "already-accrued" {
				t.Fatal("editing manufactured update credit", planner.Tabs["tab"])
			}
			// Reprocessing the same manifest must not create another accrual.
			planner.MigrateTab("tab", tc.old, tc.current)
			if planner.Tabs["tab"].CreditMicros != tc.expected {
				t.Fatal("duplicate migration added credit", planner.Tabs["tab"])
			}
		})
	}
}

func TestRecordingUpdateMigrateInvalidatesOldPlansAndKeepsStableTabIsolation(t *testing.T) {
	sources := []string{"甲", "乙"}
	states := migrationStates(sources)
	planner := recordingUpdatePlanner{}
	oldPlan := planner.Plan("tab-a", "479:1", states, 100, nil)
	if !oldPlan.Selected[0] || !oldPlan.Selected[1] {
		t.Fatal("test did not create a pending update plan")
	}
	planner.Plan("tab-b", "479:1", states, 25, nil)
	otherBefore := planner.Tabs["tab-b"]
	if planner.MigrateTab("unknown-tab", sources, sources) {
		t.Fatal("unknown stable ID borrowed another tab")
	}
	if !planner.MigrateTab("tab-a", nil, sources) || planner.Commit(oldPlan, 0) {
		t.Fatal("unchanged migration left an old update plan commit-able")
	}
	if !reflect.DeepEqual(planner.Tabs["tab-b"], otherBefore) {
		t.Fatal("migrating tab-a changed tab-b")
	}
	newPlan := planner.Plan("tab-a", "479:1", states, 100, nil)
	if !planner.Commit(newPlan, 0) || planner.Commit(newPlan, 0) || planner.Tabs["tab-a"].CreditMicros != recordingUpdateCreditScale {
		t.Fatal("new plan lost existing credit or accrued the same round again", planner.Tabs["tab-a"])
	}
	if planner.MigrateTab("", sources, sources) || (*recordingUpdatePlanner)(nil).MigrateTab("tab", sources, sources) {
		t.Fatal("invalid planner/tab was migrated")
	}
}

func TestRecordingUpdateMigratePreservesMonotonicSequenceWithMissingOldOrder(t *testing.T) {
	oldSources := []string{"甲", "乙"}
	planner := recordingUpdatePlanner{Tabs: map[string]recordingUpdateCredit{"tab": {
		ContentKey: recordingcache.ScriptHash(oldSources), CreditMicros: 2 * recordingUpdateCreditScale,
		SentenceUpdated: []uint64{7}, UpdateSequence: 2,
	}}}
	planner.MigrateTab("tab", oldSources, []string{"乙", "甲", "丙"})
	if got := planner.Tabs["tab"]; got.UpdateSequence != 7 || !reflect.DeepEqual(got.SentenceUpdated, []uint64{0, 0, 0}) || got.CreditMicros != 2*recordingUpdateCreditScale {
		t.Fatal("invalid old position array was guessed or sequence went backward", got)
	}
}
