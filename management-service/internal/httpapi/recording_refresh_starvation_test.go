package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/recordingcache"
)

// Session 481 repeatedly retried a few short sources for hours while excluding
// every healthy four-version source, despite ample per-round money remaining.
func TestRecordingRefreshUnderfilledTailDoesNotStarveWarmRotation(t *testing.T) {
	server, version, quotePolicy := recordingRefreshQuoteFixture()
	policy := incrementalPolicy(t)
	policy.TextModel = quotePolicy.TextModel
	policy.Config.TextProfileID = quotePolicy.Config.TextProfileID
	store := &refreshScheduleSummaryStore{}
	server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
	tabs := [][]recordingSentenceState{make([]recordingSentenceState, 124)}
	for index := range tabs[0] {
		state := &tabs[0][index]
		state.Source = "欢迎朋友，先看看介绍。"
		count := recordingCacheMaxVersions
		if index == 0 {
			state.Source, count = "欢迎朋友。", 1
		}
		for variant := 0; variant < count; variant++ {
			unit := inventoryFixtureUnit(int64(index*10+variant+1), state.Source)
			unit.Track.DurationMS = 5_000
			unit.Track.Timeline[0].EndMS = 5_000
			state.Versions = append(state.Versions, []recordingReadyUnit{unit})
		}
	}
	planner := &recordingUpdatePlanner{}
	for round := 19; round <= 37; round++ {
		schedule, plans := server.prepareRecordingRefreshSchedule(context.Background(), 14, 29, version, policy, tabs,
			func(int, int) []recordingcache.Entry { return nil }, planner, []string{"t1"}, fmt.Sprintf("481:%d", round), time.Hour, false, nil)
		plan := plans[0]
		if !schedule.InventoryFilling || schedule.Paused || schedule.Budget.RoundLimitMicros <= 0 {
			t.Fatalf("round %d lost available refresh allowance: %+v", round, schedule)
		}
		if !tabs[0][1].UpdateEligible || len(schedule.Pending) < 2 || !plan.Selected[0] || plan.order[0] != 0 {
			t.Fatalf("round %d: one stubborn short source excluded the healthy pool (pending=%d, healthyEligible=%v)", round, len(schedule.Pending), tabs[0][1].UpdateEligible)
		}
		if schedule.pendingMicros() > schedule.Budget.RoundLimitMicros {
			t.Fatal("refresh reservations exceeded the shared round budget")
		}
		if !planner.BeginAttempt(plan, 0) {
			t.Fatal("underfilled source lost priority")
		}
		// A definitely returned duplicate still costs money, but produces no TTS.
		events := &liveCostCallEvents{}
		events.ChargedMicros.Store(124)
		schedule.complete(0, 0, events)
		schedule.replace(planner, plan, 0, 0)
		healthyAttempt := false
		for index := 1; index < len(plan.Selected); index++ {
			if plan.Selected[index] && schedule.admit(0, index, livecost.Summary{}, nil) && planner.BeginAttempt(plan, index) {
				healthyAttempt = true
				break
			}
		}
		if !healthyAttempt || plan.maxAttempts > len(plan.order) || schedule.SpentMicros != 124 {
			t.Fatal("a paid duplicate starved healthy work, escaped the retry bound or lost its charge")
		}
	}
}
