package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/recordingcache"
)

func refreshRoundStates(count, runes int) []recordingSentenceState {
	states := make([]recordingSentenceState, count)
	for i := range states {
		states[i] = recordingSentenceState{Source: fmt.Sprintf("%03d", i) + strings.Repeat("句", runes-3), Refreshed: -1}
	}
	return states
}

func refreshRoundSelected(plan *recordingUpdatePlan) []int {
	var selected []int
	for i, yes := range plan.Selected {
		if yes {
			selected = append(selected, i)
		}
	}
	return selected
}

func refreshRoundSources(states []recordingSentenceState) []string {
	sources := make([]string, len(states))
	for i, state := range states {
		sources[i] = state.Source
	}
	return sources
}

func TestRecordingRefreshRoundFailureOnlyReplacesFutureAndSpendsOnSuccess(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := refreshRoundStates(8, 10)
	plan := planner.PlanBounded("tab", "480:2", states, 25, 50, nil)
	if got := refreshRoundSelected(plan); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatal("fixture needs two original update slots", got)
	}
	before := planner.Tabs["tab"].CreditMicros
	if !planner.BeginAttempt(plan, 0) || planner.BeginAttempt(plan, 0) {
		t.Fatal("a source must begin at most once in this pass")
	}
	added := planner.ReplaceFailed(plan, 0, 4)
	if !reflect.DeepEqual(added, []int{4}) || planner.Tabs["tab"].CreditMicros != before || planner.Commit(plan, 0) {
		t.Fatal("failure spent successful-update credit, remained committable or revisited past content", added, planner.Tabs)
	}
	if again := planner.ReplaceFailed(plan, 0, 4); len(again) != 0 {
		t.Fatal("duplicate failure notification selected more paid work", again)
	}
	at := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)
	for _, index := range []int{1, 4} {
		if !planner.BeginAttempt(plan, index) || !planner.CommitAt(plan, index, at) || planner.CommitAt(plan, index, at) {
			t.Fatal("replacement or pending original failed to commit exactly once", index, planner.Tabs)
		}
	}
	credit := planner.Tabs["tab"]
	if credit.CreditMicros != 0 || credit.SentenceUpdated[0] != 0 || credit.SentenceFreshAt[0] != 0 || credit.SentenceFreshAt[4] != at.UnixMilli() {
		t.Fatal("failed source was marked fresh, or successful character credit was mischarged", credit)
	}
}

func TestRecordingRefreshRoundBackfillHasFiniteUniqueAttemptBound(t *testing.T) {
	for _, tc := range []struct{ sentences, percent int }{{20, 25}, {200, 50}} {
		t.Run(fmt.Sprintf("%d_sentences", tc.sentences), func(t *testing.T) {
			planner := recordingUpdatePlanner{}
			states := refreshRoundStates(tc.sentences, 10)
			plan := planner.PlanBounded("tab", "480:2", states, tc.percent, 50, nil)
			initial := len(refreshRoundSelected(plan))
			limit := min(tc.sentences, initial+min(24, (initial+1)/2))
			before := planner.Tabs["tab"].CreditMicros
			attempts := 0
			for i := range states {
				if !plan.Selected[i] || !planner.BeginAttempt(plan, i) {
					continue
				}
				attempts++
				if planner.BeginAttempt(plan, i) {
					t.Fatal("same source began twice", i)
				}
				for _, replacement := range planner.ReplaceFailed(plan, i, i+1) {
					if replacement <= i || replacement >= len(states) {
						t.Fatal("replacement escaped the remaining playback suffix", i, replacement)
					}
				}
			}
			if attempts != limit || planner.Tabs["tab"].CreditMicros != before || planner.Tabs["tab"].UpdateSequence != 0 {
				t.Fatal("all failures must stop at a bounded source-attempt count without fake success", attempts, limit, planner.Tabs)
			}
		})
	}
}

func TestRecordingRefreshRoundReplacementFitsSuccessfulCharacterTarget(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: strings.Repeat("甲", 12)}, {Source: strings.Repeat("乙", 4)}, {Source: strings.Repeat("丙", 20)}, {Source: strings.Repeat("丁", 6)}}
	// Seed exactly sixteen source characters of debt: initial slots are 12+4.
	planner.Tabs = map[string]recordingUpdateCredit{"tab": {ContentKey: recordingcache.ScriptHash(refreshRoundSources(states)), CreditMicros: 16 * recordingUpdateCreditScale, LastAccrualKey: "480:2"}}
	plan := planner.PlanBounded("tab", "480:2", states, 50, 50, nil)
	if got := refreshRoundSelected(plan); !reflect.DeepEqual(got, []int{0, 1}) || !planner.BeginAttempt(plan, 0) {
		t.Fatal("fixture did not reserve sixteen characters", got)
	}
	if added := planner.ReplaceFailed(plan, 0, 2); !reflect.DeepEqual(added, []int{3}) {
		t.Fatal("oversized future sentence blocked a later whole sentence that fits", added)
	}
	successfulCharacters := 0
	for _, index := range []int{1, 3} {
		if !planner.BeginAttempt(plan, index) || !planner.CommitAt(plan, index, time.Unix(100, 0)) {
			t.Fatal("valid selected replacement could not finish", index)
		}
		successfulCharacters += utf8.RuneCountInString(states[index].Source)
	}
	if successfulCharacters > 16 || planner.Tabs["tab"].CreditMicros != int64(16-successfulCharacters)*recordingUpdateCreditScale {
		t.Fatal("replacement exceeded the original successful-character allowance", successfulCharacters, planner.Tabs)
	}
}

func TestRecordingRefreshRoundLastFailureNeverBacktracks(t *testing.T) {
	states := refreshRoundStates(8, 10)
	fresh := make([]int64, len(states))
	for i := range fresh {
		fresh[i] = 1000
	}
	fresh[7] = 0
	planner := recordingUpdatePlanner{Tabs: map[string]recordingUpdateCredit{"tab": {ContentKey: recordingcache.ScriptHash(refreshRoundSources(states)), SentenceFreshAt: fresh}}}
	plan := planner.PlanBounded("tab", "480:2", states, 13, 50, nil)
	if got := refreshRoundSelected(plan); !reflect.DeepEqual(got, []int{7}) || !planner.BeginAttempt(plan, 7) {
		t.Fatal("fixture needs the only selected source at the final playback position", got)
	}
	before := planner.Tabs["tab"].CreditMicros
	if got := planner.ReplaceFailed(plan, 7, len(states)); len(got) != 0 || planner.Tabs["tab"].CreditMicros != before {
		t.Fatal("last failure replayed earlier content or erased its remaining debt", got, planner.Tabs)
	}
}

func TestRecordingRefreshRoundFirstPassOrZeroBudgetNeverBackfillsDebt(t *testing.T) {
	for _, tc := range []struct{ percent, maxPercent int }{{0, 50}, {25, 0}} {
		t.Run(fmt.Sprintf("%d_%d", tc.percent, tc.maxPercent), func(t *testing.T) {
			states := refreshRoundStates(8, 10)
			planner := recordingUpdatePlanner{Tabs: map[string]recordingUpdateCredit{"tab": {ContentKey: recordingcache.ScriptHash(refreshRoundSources(states)), CreditMicros: 80 * recordingUpdateCreditScale}}}
			plan := planner.PlanBounded("tab", "new-session:0", states, tc.percent, tc.maxPercent, nil)
			if len(refreshRoundSelected(plan)) != 0 || plan.SingleLong {
				t.Fatal("first pass or explicit zero cap spent old optional-refresh debt", plan)
			}
			for i := range states {
				if planner.BeginAttempt(plan, i) || len(planner.ReplaceFailed(plan, i, i+1)) != 0 || planner.Commit(plan, i) {
					t.Fatal("zero budget enabled an optional attempt or replacement", i)
				}
			}
		})
	}
}

func TestRecordingRefreshRoundAccumulatedDebtRespectsExplicitPassCap(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := refreshRoundStates(10, 10)
	var plan *recordingUpdatePlan
	for round := 1; round <= 100; round++ {
		plan = planner.PlanBounded("tab", fmt.Sprintf("480:%d", round), states, 25, 50, nil)
	}
	if planner.Tabs["tab"].CreditMicros > 100*recordingUpdateCreditScale || len(refreshRoundSelected(plan)) != 5 || plan.SingleLong {
		t.Fatal("normal debt burst exceeded the explicitly configured half-pass cap", refreshRoundSelected(plan), planner.Tabs)
	}
	successful := 0
	for _, index := range refreshRoundSelected(plan) {
		if !planner.BeginAttempt(plan, index) || !planner.CommitAt(plan, index, time.Unix(100, 0)) {
			t.Fatal("selected debt repayment failed", index)
		}
		successful += utf8.RuneCountInString(states[index].Source)
	}
	if successful != 50 || planner.Tabs["tab"].CreditMicros != 50*recordingUpdateCreditScale {
		t.Fatal("successful work did not retain the bounded unpaid remainder", successful, planner.Tabs)
	}
}

func TestRecordingRefreshRoundSingleOldLongSentenceCannotStarve(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := []recordingSentenceState{{Source: strings.Repeat("长", 80), Refreshed: -1}, {Source: strings.Repeat("短", 20), Refreshed: 1}}
	var selected *recordingUpdatePlan
	for round := 1; round <= 4; round++ {
		plan := planner.PlanBounded("tab", fmt.Sprintf("480:%d", round), states, 25, 50, nil)
		if plan.Selected[0] {
			selected = plan
			break
		}
		if plan.Selected[1] {
			t.Fatal("newer short source consumed the oldest whole long source's saving")
		}
	}
	if selected == nil || !selected.SingleLong || !reflect.DeepEqual(refreshRoundSelected(selected), []int{0}) || !planner.BeginAttempt(selected, 0) || !planner.CommitAt(selected, 0, time.Unix(100, 0)) || planner.Tabs["tab"].CreditMicros < 0 {
		t.Fatal("oldest long source never received its one-source cap exception", selected, planner.Tabs)
	}
}

func TestRecordingRefreshRoundRecentEditDoesNotOvertakeUnchangedOldSource(t *testing.T) {
	oldSources := []string{"甲句", "乙句", "丙句"}
	newSources := []string{"丙句", "甲改", "乙句"}
	planner := recordingUpdatePlanner{Version: 1, Tabs: map[string]recordingUpdateCredit{"tab": {
		ContentKey: recordingcache.ScriptHash(oldSources), SentenceUpdated: []uint64{9, 4, 7}, UpdateSequence: 9,
		SentenceFreshAt: []int64{1000, 2000, 3000},
	}}}
	before := time.Now().UTC().UnixMilli()
	if !planner.MigrateTab("tab", oldSources, newSources) {
		t.Fatal("trusted changed source snapshot was not migrated")
	}
	credit := planner.Tabs["tab"]
	if !reflect.DeepEqual(credit.SentenceUpdated, []uint64{7, 0, 4}) || credit.SentenceFreshAt[0] != 3000 || credit.SentenceFreshAt[2] != 2000 || credit.SentenceFreshAt[1] < before || credit.CreditMicros != 0 || credit.UpdateSequence != 9 {
		t.Fatal("migration manufactured optional success or lost unchanged source freshness", credit)
	}
	plan := planner.PlanBounded("tab", "481:2", migrationStates(newSources), 34, 50, nil)
	if plan.Selected[1] || !plan.Selected[2] {
		t.Fatal("just-edited source's zero rewrite count jumped ahead of old unchanged text", plan.Selected)
	}
}

func TestRecordingRefreshRoundChronologicalFairnessSurvivesJSON(t *testing.T) {
	states := refreshRoundStates(4, 10)
	planner := recordingUpdatePlanner{Version: 1, Tabs: map[string]recordingUpdateCredit{"tab": {
		ContentKey: recordingcache.ScriptHash(refreshRoundSources(states)), SentenceUpdated: []uint64{1, 2, 99, 3}, UpdateSequence: 99,
		SentenceFreshAt: []int64{1000, 3000, 0, 2000},
	}}}
	first := planner.PlanBounded("tab", "480:2", states, 25, 50, nil)
	at := time.Unix(10, 0).UTC()
	if !reflect.DeepEqual(refreshRoundSelected(first), []int{2}) || !planner.BeginAttempt(first, 2) || !planner.CommitAt(first, 2, at) {
		t.Fatal("unknown old freshness must precede known recent text despite its larger legacy sequence", first.Selected)
	}
	raw, err := json.Marshal(planner)
	var restored recordingUpdatePlanner
	if err != nil || json.Unmarshal(raw, &restored) != nil {
		t.Fatal("successful freshness did not cross a restart boundary", err)
	}
	second := restored.PlanBounded("tab", "481:2", states, 25, 50, nil)
	if !reflect.DeepEqual(refreshRoundSelected(second), []int{0}) || restored.Tabs["tab"].SentenceFreshAt[2] != at.UnixMilli() {
		t.Fatal("restart forgot successful chronological fairness", second.Selected, restored.Tabs)
	}
	if planner.BeginAttempt(first, 2) {
		t.Fatal("successful source was attempted again")
	}
}

func TestRecordingRefreshRoundFailureDoesNotErasePaidWorkOrInventUpdate(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := refreshRoundStates(4, 10)
	plan := planner.PlanBounded("tab", "480:2", states, 25, 50, nil)
	stats := newRecordingSupplyStats(14, 29, 480, 4, 2, "tab", states, plan.Selected, 25, 25)
	modelSuccesses, ttsSuccesses := 0, 0
	if !planner.BeginAttempt(plan, 0) {
		t.Fatal("initial selected source did not begin")
	}
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old", states[0].Source)}
	failed, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{Refresh: true, Versions: [][]recordingReadyUnit{old}, Generate: func(context.Context) ([]recordingReadyUnit, error) {
		modelSuccesses++ // A paid successful model response can still echo its input.
		return nil, errors.New("改写未改变原句，保留有效旧音")
	}})
	stats.observe(states[0].Source, true, failed, err)
	if err != nil || failed.Generated || failed.GenerationErr == nil || stats.Updated != 0 || stats.Reused != 1 {
		t.Fatal("fallback to existing audio was counted as a successful update", failed, stats, err)
	}
	added := planner.ReplaceFailed(plan, 0, 1)
	if !reflect.DeepEqual(added, []int{1}) || !planner.BeginAttempt(plan, 1) {
		t.Fatal("failed update did not select one future replacement", added)
	}
	fresh, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{Refresh: true, Generate: func(context.Context) ([]recordingReadyUnit, error) {
		modelSuccesses++
		ttsSuccesses++
		return []recordingReadyUnit{recordingReuseFixtureUnit("new", "一份新的有效表达。")}, nil
	}})
	stats.observe(states[1].Source, true, fresh, err)
	if err != nil || !fresh.Generated || !planner.CommitAt(plan, 1, time.Unix(100, 0)) || modelSuccesses != 2 || ttsSuccesses != 1 || stats.Updated != 1 || stats.RefreshFailed != 1 || planner.Tabs["tab"].CreditMicros != 0 {
		t.Fatal("source-update success and paid request successes were conflated", fresh, stats, planner.Tabs, modelSuccesses, ttsSuccesses, err)
	}
}

func TestRecordingRefreshRoundStalePlanCannotBeginReplaceOrCommit(t *testing.T) {
	planner := recordingUpdatePlanner{}
	states := refreshRoundStates(4, 10)
	old := planner.PlanBounded("tab", "480:2", states, 25, 50, nil)
	if !planner.BeginAttempt(old, 0) {
		t.Fatal("fixture did not create an in-flight source")
	}
	current := planner.PlanBounded("tab", "480:3", states, 25, 50, nil)
	before := planner.Tabs["tab"]
	if planner.BeginAttempt(old, 1) || len(planner.ReplaceFailed(old, 0, 1)) != 0 || planner.CommitAt(old, 0, time.Unix(100, 0)) || !reflect.DeepEqual(before, planner.Tabs["tab"]) || len(refreshRoundSelected(current)) == 0 {
		t.Fatal("superseded round changed current update credit/fairness", planner.Tabs)
	}
}
