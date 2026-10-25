package httpapi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

func refreshSchedulePlans(tabs, sentences, percent int) (*recordingUpdatePlanner, []*recordingUpdatePlan, [][]int64) {
	planner := &recordingUpdatePlanner{}
	plans := make([]*recordingUpdatePlan, tabs)
	quotes := make([][]int64, tabs)
	for tab := range plans {
		states := make([]recordingSentenceState, sentences)
		quotes[tab] = make([]int64, sentences)
		for index := range states {
			states[index] = recordingSentenceState{Source: fmt.Sprintf("%03d", index) + strings.Repeat("句", 7), Refreshed: -1}
			quotes[tab][index] = 100
		}
		plans[tab] = planner.PlanBounded(fmt.Sprintf("tab-%d", tab), "480:2", states, percent, percent, nil)
	}
	return planner, plans, quotes
}

func refreshSchedulePositions(schedule *recordingRefreshSchedule) map[recordingRefreshPosition]int64 {
	positions := make(map[recordingRefreshPosition]int64, len(schedule.Pending))
	for position, quote := range schedule.Pending {
		positions[position] = quote
	}
	return positions
}

func TestRecordingRefreshScheduleSharesOneAllowanceAcrossAllTabs(t *testing.T) {
	planner, plans, quotes := refreshSchedulePlans(2, 4, 100)
	before := []int64{planner.Tabs["tab-0"].CreditMicros, planner.Tabs["tab-1"].CreditMicros}
	schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 100, HeadroomMicros: 1_000, RoundLimitMicros: 300}, quotes, plans)
	want := map[recordingRefreshPosition]int64{{0, 0}: 100, {1, 0}: 100, {0, 1}: 100}
	if !reflect.DeepEqual(refreshSchedulePositions(schedule), want) || schedule.pendingMicros() != 300 {
		t.Fatal("each tab borrowed the whole-script allowance, or due tabs lost their share", schedule.Pending)
	}
	for tab, plan := range plans {
		selected := 0
		for index, yes := range plan.Selected {
			if yes {
				selected++
				if schedule.Pending[recordingRefreshPosition{tab, index}] != 100 {
					t.Fatal("selected optional work has no monetary reservation", tab, index)
				}
			}
		}
		if plan.maxAttempts != selected+min(24, (selected+1)/2) || planner.Tabs[plan.TabID].CreditMicros != before[tab] {
			t.Fatal("monetary pruning spent success credit or retained an inflated retry allowance", plan.TabID, plan, planner.Tabs)
		}
	}
}

func TestRecordingRefreshScheduleFailedPaidModelKeepsLaterTabReservation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		limit   int64
		charged int64
		added   int
	}{
		{"no_charge_can_replace", 300, 0, 1},
		{"successful_but_unadopted_model_consumes_room", 350, 50, 1},
		{"one_micro_over_remainder_cannot_replace", 350, 51, 0},
		{"full_allocation_and_paid_failure_cannot_replace", 300, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner, plans, quotes := refreshSchedulePlans(2, 4, 50)
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: tc.limit}, quotes, plans)
			before := planner.Tabs["tab-0"].CreditMicros
			later := recordingRefreshPosition{1, 0}
			if schedule.Pending[later] != 100 || !schedule.admit(0, 0, livecost.Summary{}, nil) || !planner.BeginAttempt(plans[0], 0) {
				t.Fatal("fixture did not allocate and admit the initial optional work", schedule.Pending)
			}
			// The model returned successfully but echoed the source or failed a
			// later audit, so no new playable version was adopted. It still cost
			// money; only the separate successful-source credit is preserved.
			events := &liveCostCallEvents{}
			events.ChargedMicros.Store(tc.charged)
			schedule.complete(0, 0, events)
			if got := schedule.replace(planner, plans[0], 0, 0); got != tc.added {
				t.Fatalf("replacement count=%d want=%d spent=%d pending=%v", got, tc.added, schedule.SpentMicros, schedule.Pending)
			}
			if schedule.SpentMicros != tc.charged || schedule.Pending[later] != 100 ||
				planner.Tabs["tab-0"].CreditMicros != before || planner.Commit(plans[0], 0) {
				t.Fatal("paid failure was refunded, spent success credit, or stole a later tab's reservation", schedule, planner.Tabs)
			}
			if schedule.SpentMicros+schedule.pendingMicros() > tc.limit {
				t.Fatal("failure replacement oversubscribed the one shared allowance", schedule)
			}
			if got := schedule.Pending[recordingRefreshPosition{0, 2}]; (got == 100) != (tc.added == 1) {
				t.Fatal("replacement did not select only a future unattempted source", schedule.Pending)
			}
			if schedule.replace(planner, plans[0], 0, 0) != 0 || planner.BeginAttempt(plans[0], 0) {
				t.Fatal("the same failed source triggered duplicate optional work")
			}
		})
	}
}

func TestRecordingRefreshScheduleActualChargeDriftStopsRemainingOptionalWork(t *testing.T) {
	planner, plans, quotes := refreshSchedulePlans(1, 4, 50)
	schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: 200}, quotes, plans)
	if !schedule.admit(0, 0, livecost.Summary{}, nil) || !planner.BeginAttempt(plans[0], 0) {
		t.Fatal("fixture did not admit its first source")
	}
	events := &liveCostCallEvents{}
	events.ChargedMicros.Store(101) // Higher than the local quote, but already incurred.
	schedule.complete(0, 0, events)
	if schedule.SpentMicros != 101 || schedule.admit(0, 1, livecost.Summary{}, nil) || !schedule.Paused {
		t.Fatal("actual charges were replaced by quotes or permitted the next request to overspend", schedule)
	}
	if schedule.replace(planner, plans[0], 0, 0) != 0 || !plans[0].paused {
		t.Fatal("paused accounting left failed-source replacement enabled")
	}
}

func TestRecordingRefreshScheduleUncertainAccountingAndDeclinePauseOnlyOptional(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events func() *liveCostCallEvents
		reason string
	}{
		{"missing_receipt", func() *liveCostCallEvents { return nil }, "更新请求缺少费用回执"},
		{"accounting_unavailable", func() *liveCostCallEvents {
			e := &liveCostCallEvents{}
			e.ChargedMicros.Store(25)
			e.AccountingUnavailable.Store(true)
			return e
		}, "更新请求记账暂不可用"},
		{"provider_result_unknown", func() *liveCostCallEvents {
			e := &liveCostCallEvents{}
			e.ChargedMicros.Store(25)
			e.Uncertain.Store(true)
			return e
		}, "更新请求结果待核对"},
		{"optional_declined", func() *liveCostCallEvents {
			e := &liveCostCallEvents{}
			e.ChargedMicros.Store(25)
			e.OptionalDeclined.Store(true)
			return e
		}, "可选更新预算暂不足"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner, plans, quotes := refreshSchedulePlans(1, 4, 50)
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: 500}, quotes, plans)
			if !planner.BeginAttempt(plans[0], 0) {
				t.Fatal("fixture did not begin its update")
			}
			events := tc.events()
			schedule.complete(0, 0, events)
			wantSpent := int64(0)
			if events != nil {
				wantSpent = 25
			}
			if !schedule.Paused || schedule.PauseReason != tc.reason || schedule.SpentMicros != wantSpent ||
				schedule.admit(0, 1, livecost.Summary{}, nil) || schedule.replace(planner, plans[0], 0, 0) != 0 {
				t.Fatal("unknown or declined accounting opened more optional work or dropped known charges", schedule)
			}
			// Mirror the producer: an optional denial changes Refresh to false.
			// Necessary cold supply still uses Generate when no valid audio exists.
			generated := 0
			result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{Refresh: false,
				Generate: func(context.Context) ([]recordingReadyUnit, error) {
					generated++
					return []recordingReadyUnit{recordingReuseFixtureUnit("necessary", "当前事实下的基础供料。")}, nil
				}})
			if err != nil || !result.Generated || generated != 1 || !schedule.Paused {
				t.Fatal("optional accounting failure blocked independent necessary supply", result, err, generated)
			}
		})
	}
}

func TestRecordingRefreshScheduleAdmissionRechecksCurrentRoomHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary livecost.Summary
		err     error
		allow   bool
	}{
		{"exact_room_remainder", livecost.Summary{TotalMicros: 1_299_900}, nil, true},
		{"room_remainder_below_quote", livecost.Summary{TotalMicros: 1_299_901}, nil, false},
		{"optional_soft_line", livecost.Summary{TotalMicros: 1_200_000, OptionalMicros: 1_200_000}, nil, false},
		{"invalid_accounting_is_not_zero_spend", livecost.Summary{TotalMicros: -1}, nil, false},
		{"summary_unavailable", livecost.Summary{}, errors.New("只读统计不可用"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, plans, quotes := refreshSchedulePlans(1, 2, 50)
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: 100}, quotes, plans)
			if got := schedule.admit(0, 0, tc.summary, tc.err); got != tc.allow || schedule.Paused == tc.allow {
				t.Fatal("optional admission ignored current shared-room accounting", got, tc.allow, schedule)
			}
		})
	}
}

func TestRecordingRefreshScheduleFirstPassNeverSpendsAccumulatedOptionalCredit(t *testing.T) {
	planner, _, quotes := refreshSchedulePlans(1, 4, 100)
	states := make([]recordingSentenceState, 4)
	for index := range states {
		states[index] = recordingSentenceState{Source: fmt.Sprintf("%03d", index) + strings.Repeat("句", 7), Refreshed: -1}
	}
	before := planner.Tabs["tab-0"].CreditMicros
	plan := planner.PlanBounded("tab-0", "481:1", states, 0, 0, nil)
	schedule := newRecordingRefreshSchedule(recordingRefreshBudget{}, quotes, []*recordingUpdatePlan{plan})
	if len(schedule.Pending) != 0 || schedule.SpentMicros != 0 || planner.Tabs["tab-0"].CreditMicros != before ||
		schedule.admit(0, 0, livecost.Summary{}, nil) || planner.BeginAttempt(plan, 0) || schedule.replace(planner, plan, 0, 0) != 0 {
		t.Fatal("first pass used old update debt to authorize paid optional rewriting", schedule, plan, planner.Tabs)
	}
}

func TestRecordingRefreshScheduleWholeLongSourceNeedsActualMonetaryHeadroom(t *testing.T) {
	for _, tc := range []struct {
		name     string
		headroom int64
		allow    bool
	}{
		{"no_headroom", 0, false},
		{"one_micro_short", 799, false},
		{"exact_whole_source_headroom", 800, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := &recordingUpdatePlanner{}
			states := []recordingSentenceState{{Source: strings.Repeat("旧", 80)}, {Source: strings.Repeat("新", 20)}}
			var plan *recordingUpdatePlan
			for round := 0; round < 4; round++ {
				plan = planner.PlanBounded("long", fmt.Sprint(round), states, 25, 25, nil)
			}
			if !plan.SingleLong || !plan.Selected[0] || plan.Selected[1] {
				t.Fatal("fixture did not accumulate enough credit for exactly one old long source", plan)
			}
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 25, HeadroomMicros: tc.headroom, RoundLimitMicros: 250}, [][]int64{{800, 200}}, []*recordingUpdatePlan{plan})
			if got := schedule.Pending[recordingRefreshPosition{0, 0}] > 0; got != tc.allow || plan.Selected[0] != tc.allow {
				t.Fatal("saved source credit was mistaken for money or a whole source was needlessly split", schedule, plan)
			}
			if tc.allow && (schedule.Budget.RoundLimitMicros != 800 || schedule.pendingMicros() != 800) {
				t.Fatal("whole-source exception did not remain within actual monetary headroom", schedule)
			}
		})
	}
}

func TestRecordingRefreshScheduleMissingOrInvalidQuoteNeverAuthorizesAnAttempt(t *testing.T) {
	for _, quote := range []int64{-1, 0} {
		planner, plans, quotes := refreshSchedulePlans(1, 2, 50)
		quotes[0][0] = quote
		schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: 500}, quotes, plans)
		if len(schedule.Pending) != 0 || plans[0].Selected[0] || planner.BeginAttempt(plans[0], 0) {
			t.Fatal("invalid estimate opened unquoted optional paid work", quote, schedule, plans[0])
		}
	}
	planner, plans, _ := refreshSchedulePlans(1, 2, 50)
	schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1_000, RoundLimitMicros: 500}, nil, plans)
	if len(schedule.Pending) != 0 || planner.BeginAttempt(plans[0], 0) || schedule.quote(-1, 0) != 0 || schedule.quote(0, -1) != 0 {
		t.Fatal("missing estimate metadata was treated as free optional work", schedule)
	}
}

func TestRecordingRefreshScheduleMoneyCanChooseCheaperDueSource(t *testing.T) {
	for _, tc := range []struct {
		name        string
		creditRunes int64
		initial     []bool
	}{
		{"production_preselection_can_use_accumulated_debt", 102, []bool{true, true}},
		{"money_reject_backfills_initially_unselected_due_source", 80, []bool{true, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, version, policy := recordingRefreshQuoteFixture()
			states := []recordingSentenceState{
				{Source: strings.Repeat("甲", 40) + "。", Refreshed: -1},
				{Source: strings.Repeat("a", 60) + "。", Refreshed: -1},
			}
			quotes := make([]int64, len(states))
			for index := range states {
				var err error
				quotes[index], err = server.recordingRefreshQuote(states[index].Source, version, policy)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !reflect.DeepEqual(quotes, []int64{8844, 6952}) {
				t.Fatal("fixture no longer represents differently weighted, real model+TTS prices", quotes)
			}
			planner := &recordingUpdatePlanner{}
			planner.PlanBounded("warm", "480:2", states, 100, 100, nil)
			credit := planner.Tabs["warm"]
			credit.CreditMicros = tc.creditRunes * recordingUpdateCreditScale
			credit.SentenceFreshAt = []int64{1, 2} // A is older, but cannot fit the money.
			planner.Tabs["warm"] = credit
			// Percentage earns source credit. Production lets existing debt use
			// up to the whole tab, and money remains the definitive admission.
			plan := planner.PlanBounded("warm", "480:2", states, 48, 100, nil)
			if !reflect.DeepEqual(plan.Selected, tc.initial) || plan.SingleLong {
				t.Fatal("fixture did not create the intended due-work preselection", plan.Selected, plan.SingleLong)
			}
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 48, HeadroomMicros: 7727, RoundLimitMicros: 7727}, [][]int64{quotes}, []*recordingUpdatePlan{plan})
			if !reflect.DeepEqual(plan.Selected, []bool{false, true}) || schedule.pendingMicros() != 6952 ||
				schedule.Pending[recordingRefreshPosition{0, 1}] != 6952 || planner.Tabs["warm"].CreditMicros != tc.creditRunes*recordingUpdateCreditScale ||
				!schedule.admit(0, 1, livecost.Summary{}, nil) || !planner.BeginAttempt(plan, 1) {
				t.Fatal("an expensive oldest source froze an affordable, already-due warm source", schedule, plan, planner.Tabs)
			}
		})
	}
}

func TestRecordingRefreshScheduleRanksInventoryAndAgeAcrossTabs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []int
		freshAt  []int64
		winner   int
	}{
		{"fewest_versions_before_age", []int{4, 1, 2}, []int64{0, 900, 0}, 1},
		{"oldest_when_version_counts_match", []int{2, 2, 2}, []int64{300, 100, 200}, 1},
		{"unknown_age_older_than_known_age", []int{2, 2, 2}, []int64{300, 0, 200}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			planner := &recordingUpdatePlanner{}
			plans := make([]*recordingUpdatePlan, len(tc.versions))
			quotes := make([][]int64, len(plans))
			for tab := range plans {
				id := fmt.Sprintf("rank-%d", tab)
				states := []recordingSentenceState{{Source: "欢迎朋友。", VersionCount: tc.versions[tab], InventoryKnown: true, UpdateEligible: true}}
				planner.PlanBounded(id, "480:2", states, 100, 100, nil)
				credit := planner.Tabs[id]
				credit.SentenceFreshAt[0] = tc.freshAt[tab]
				planner.Tabs[id] = credit
				plans[tab] = planner.PlanBounded(id, "480:2", states, 100, 100, nil)
				quotes[tab] = []int64{100}
				// A later persisted-state mutation must not rewrite the already
				// constructed schedule's immutable age snapshot.
				credit = planner.Tabs[id]
				credit.SentenceFreshAt[0] = int64(tab)
				planner.Tabs[id] = credit
			}
			schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 100, HeadroomMicros: 100, RoundLimitMicros: 100}, quotes, plans)
			if len(schedule.Pending) != 1 || schedule.Pending[recordingRefreshPosition{tc.winner, 0}] != 100 {
				t.Fatal("earlier playback tabs outranked fewer-version or older due work elsewhere", schedule.Pending)
			}
		})
	}
}

// The planner is read-only. These methods deliberately fail if preparation
// starts a reservation or records a completion, even against a local fixture.
type refreshScheduleSummaryStore struct {
	SummaryValue livecost.Summary
	SummaryErr   error
	SummaryCalls int
}

func (*refreshScheduleSummaryStore) ReserveLiveCost(context.Context, livecost.Request, livecost.Policy, time.Time) (livecost.Reservation, error) {
	panic("只读更新规划不得预留费用")
}

func (*refreshScheduleSummaryStore) FinishLiveCost(context.Context, livecost.Scope, string, livecost.Receipt, time.Time) error {
	panic("只读更新规划不得写入费用回执")
}

func (store *refreshScheduleSummaryStore) LiveCostSummary(context.Context, livecost.Scope, livecost.Policy, time.Time) (livecost.Summary, error) {
	store.SummaryCalls++
	return store.SummaryValue, store.SummaryErr
}

func TestRecordingRefreshSchedulePrepareFirstPassSkipsInventoryAndAccounting(t *testing.T) {
	for _, name := range []string{"nil_server", "summary_unavailable"} {
		t.Run(name, func(t *testing.T) {
			var server *Server
			store := &refreshScheduleSummaryStore{SummaryErr: errors.New("账务不可用")}
			if name != "nil_server" {
				server = &Server{liveCostRequests: livecost.New(store, livecost.DefaultPolicy())}
			}
			states := [][]recordingSentenceState{{{Source: "欢迎新来的朋友。"}}}
			planner := &recordingUpdatePlanner{}
			planner.PlanBounded("first", "old:4", states[0], 100, 100, nil)
			credit := planner.Tabs["first"].CreditMicros
			schedule, plans := server.prepareRecordingRefreshSchedule(context.Background(), 14, 29, model.LiveAgentPlanVersion{}, nil,
				states, func(int, int) []recordingcache.Entry {
					t.Fatal("first-pass baseline supply scanned the complete inventory")
					return nil
				}, planner, []string{"first"}, "481:1", 0, true, nil)
			if store.SummaryCalls != 0 || len(schedule.Pending) != 0 || schedule.Budget != (recordingRefreshBudget{}) ||
				len(plans) != 1 || plans[0].Selected[0] || states[0][0].InventoryKnown || planner.Tabs["first"].CreditMicros != credit {
				t.Fatal("first pass depended on pricing, inventory or optional refresh despite its zero-update contract", schedule, plans, store)
			}
		})
	}
}

func TestRecordingRefreshScheduleColdSourceDoesNotFreezeFullWarmRotation(t *testing.T) {
	server, version, quotePolicy := recordingRefreshQuoteFixture()
	policy := incrementalPolicy(t)
	policy.TextModel = quotePolicy.TextModel
	policy.Config.TextProfileID = quotePolicy.Config.TextProfileID
	store := &refreshScheduleSummaryStore{}
	server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
	const warm = "欢迎朋友，先看看介绍。"
	tabs := [][]recordingSentenceState{{{Source: "今天先和大家聊聊。"}, {Source: warm}}}
	for id := int64(1); id <= recordingCacheMaxVersions; id++ {
		unit := inventoryFixtureUnit(id, warm)
		unit.Track.DurationMS = 60_000
		unit.Track.Timeline[0].EndMS = 60_000
		tabs[0][1].Versions = append(tabs[0][1].Versions, []recordingReadyUnit{unit})
	}
	planner := &recordingUpdatePlanner{}
	schedule, plans := server.prepareRecordingRefreshSchedule(context.Background(), 14, 29, version, policy, tabs,
		func(int, int) []recordingcache.Entry { return nil }, planner, []string{"mixed"}, "480:2", 0, false, nil)
	if store.SummaryCalls != 1 || tabs[0][0].VersionCount != 0 || tabs[0][0].UpdateEligible ||
		tabs[0][1].VersionCount != recordingCacheMaxVersions || !tabs[0][1].UpdateEligible ||
		plans[0].Selected[0] || !plans[0].Selected[1] || schedule.Pending[recordingRefreshPosition{0, 1}] <= 0 || schedule.Paused {
		t.Fatal("missing baseline audio froze the separate full warm pool's optional rotation", schedule, tabs, plans[0])
	}
}

func TestRecordingRefreshScheduleSourceDurationUsesApprovedAudioBeforeTextForecast(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友，先看看介绍。"
	unit := func(id int64, text string, ms int) recordingReadyUnit {
		result := inventoryFixtureUnit(id, text)
		result.Track.DurationMS = ms
		result.Track.Timeline[0].EndMS = ms
		return result
	}
	old := unit(101, source, 12_000)
	newest := []recordingReadyUnit{unit(102, "欢迎朋友。", 2_000), unit(103, "先看看介绍。", 3_000)}
	cached := inventoryFixtureEntry(t, source, unit(104, source, 20_000))
	if got := recordingRefreshSourceDuration(source, [][]recordingReadyUnit{{old}, newest}, []recordingcache.Entry{cached}, policy); got != 5*time.Second {
		t.Fatal("duration multiplied all versions, ignored multipart audio or treated text speed as real duration", got)
	}
	invalid := unit(105, "这是全国第一，绝对最好。", 1_000)
	if got := recordingRefreshSourceDuration(source, [][]recordingReadyUnit{{old}, {invalid}}, nil, policy); got != 12*time.Second {
		t.Fatal("currently rejected audio determined the next pass's duration", got)
	}
	broken := unit(106, source, 1_000)
	broken.Track.AudioURL = ""
	parts := inventoryFixtureEntry(t, source, newest...)
	parts.Duration = 99_000 // The parts are authoritative when present.
	if got := recordingRefreshSourceDuration(source, [][]recordingReadyUnit{{broken}}, []recordingcache.Entry{parts}, policy); got != 5*time.Second {
		t.Fatal("a broken memory asset hid valid multipart persistent duration", got)
	}
	if got := recordingRefreshSourceDuration(source, nil, []recordingcache.Entry{cached}, policy); got != 20*time.Second {
		t.Fatal("valid persisted duration was replaced by a character forecast", got)
	}
	forecast := time.Duration(len([]rune(source))) * time.Minute / 220
	if got := recordingRefreshSourceDuration(source, nil, nil, policy); got != forecast {
		t.Fatal("uncached source lacks a text-duration fallback", got, forecast)
	}
	if got := recordingRefreshSourceDuration(source, [][]recordingReadyUnit{{old}}, []recordingcache.Entry{cached}, nil); got != forecast {
		t.Fatal("unapproved inventory contributed an authoritative duration", got, forecast)
	}
}
