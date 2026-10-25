package httpapi

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/recordingcache"
)

func TestRecordingRefreshConfiguredBudgetChangesPacingAndPreservesZero(t *testing.T) {
	policy := livecost.Policy{Configured: true, Window: time.Hour, MainlineTargetMicros: 500_000, OptionalLimitMicros: 600_000, TotalTargetMicros: 650_000}
	got := recordingDynamicRefreshBudgetWithPolicy(livecost.Summary{}, 2200, 400_000, 0, policy)
	want := recordingRefreshBudget{Percent: 20, HeadroomMicros: 600_000, RoundLimitMicros: 83_333}
	if got != want {
		t.Fatal("configured mainline target did not replace the fixed one-yuan pace", got, want)
	}
	got = recordingDynamicRefreshBudgetForDurationWithPolicy(livecost.Summary{TotalMicros: 600_000}, 400_000, 0, 10*time.Minute, policy)
	if got != (recordingRefreshBudget{Percent: 12, HeadroomMicros: 50_000, RoundLimitMicros: 50_000}) {
		t.Fatal("configured room total did not constrain the shared remaining budget", got)
	}
	for _, p := range []livecost.Policy{
		{Configured: true, Window: time.Hour, OptionalLimitMicros: 600_000, TotalTargetMicros: 650_000},
		{Configured: true, Window: time.Hour, TotalTargetMicros: 650_000},
		{Configured: true, Window: time.Hour, MainlineTargetMicros: 700_000, OptionalLimitMicros: 600_000, TotalTargetMicros: 650_000},
	} {
		if got := recordingDynamicRefreshBudgetWithPolicy(livecost.Summary{}, 2200, 1, 0, p); got != (recordingRefreshBudget{}) {
			t.Fatal("disabled or invalid policy restored free/default optional work", p, got)
		}
	}
	policy.Window = 30 * time.Minute
	if got := recordingDynamicRefreshBudgetForDurationWithPolicy(livecost.Summary{}, 400_000, 0, 10*time.Minute, policy); got.RoundLimitMicros != 166_666 {
		t.Fatal("forecast pacing and accounting window used different policies", got)
	}
}

func TestRecordingRefreshQuoteUsesOneExplicitPriceSnapshotIncludingLowerPrices(t *testing.T) {
	server, version, policy := recordingRefreshQuoteFixture()
	for _, tc := range []struct {
		name string
		tts  int64
		in   int64
		out  int64
		want int64
	}{
		{"half_price", 400_000, 100_000, 400_000, 550},
		{"double_price", 1_600_000, 400_000, 1_600_000, 2200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := livecost.DefaultSettings()
			settings.Revision, settings.TTSPriceMicrosPer10K = 1, tc.tts
			settings.ModelPrices = nil
			settings.DefaultModelRates = []livecost.TokenTier{{InputMicrosPerMillion: tc.in, OutputMicrosPerMillion: tc.out}}
			server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
				t.Fatal("an explicit whole-pass quote queried settings per sentence")
				return livecost.Settings{}, errors.New("unexpected read")
			}
			got, err := server.recordingRefreshQuoteWithSettings("你好。", version, policy, settings)
			if err != nil || got != tc.want {
				t.Fatal("old reference floor or current global settings overrode the explicit price", got, tc.want, err)
			}
		})
	}
	settings := livecost.DefaultSettings()
	settings.TTSPriceMicrosPer10K = 0
	if _, err := server.recordingRefreshQuoteWithSettings("你好。", version, policy, settings); err == nil {
		t.Fatal("invalid zero price authorized a free update")
	}
}

func TestRecordingRefreshPrepareReadsSettingsOnceAndAdmissionSeesNewZeroTarget(t *testing.T) {
	server, version, quotePolicy := recordingRefreshQuoteFixture()
	policy := incrementalPolicy(t)
	policy.TextModel, policy.Config.TextProfileID = quotePolicy.TextModel, quotePolicy.Config.TextProfileID
	settings := livecost.DefaultSettings()
	settings.Revision = 1
	reads := 0
	server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
		reads++
		return settings, nil
	}
	store := &refreshScheduleSummaryStore{}
	server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
	tabs := [][]recordingSentenceState{{{Source: "欢迎朋友。"}, {Source: "先看看介绍。"}}}
	for index := range tabs[0] {
		unit := inventoryFixtureUnit(int64(501+index), tabs[0][index].Source)
		unit.Track.DurationMS, unit.Track.Timeline[0].EndMS = 60_000, 60_000
		tabs[0][index].Versions = [][]recordingReadyUnit{{unit}}
	}
	planner := &recordingUpdatePlanner{}
	schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
		func(int, int) []recordingcache.Entry { return nil }, planner, []string{"settings"}, "480:2", 0, false, nil)
	if reads != 1 || schedule.Paused || len(schedule.Pending) != 2 || len(store.Policies) != 1 || store.Policies[0] != settings.Policy() {
		t.Fatal("whole-pass planning did not share one validated configuration", reads, schedule, store.Policies)
	}
	settings.Revision, settings.MainlineHourlyMicros = 2, 0
	if server.admitRecordingRefresh(t.Context(), schedule, 14, 29, 0, 0) || reads != 2 || !schedule.Paused || plans[0].attempted[0] {
		t.Fatal("an old planned quote bypassed a newly disabled mainline target", schedule, plans[0], reads)
	}
	if store.Policies[len(store.Policies)-1] != settings.Policy() {
		t.Fatal("execution summary silently used the old shared controller policy", store.Policies)
	}
}

func TestRecordingRefreshUnavailableSettingsSkipsOptionalInventoryWithoutBlockingSupply(t *testing.T) {
	server, version, policy := recordingRefreshQuoteFixture()
	server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
		return livecost.Settings{}, errors.New("配置查询失败")
	}
	planner := &recordingUpdatePlanner{}
	schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy,
		[][]recordingSentenceState{{{Source: "欢迎朋友。"}}}, func(int, int) []recordingcache.Entry {
			t.Fatal("unavailable optional settings triggered a whole-library inventory scan")
			return nil
		}, planner, []string{"unavailable"}, "480:2", 0, false, nil)
	if !schedule.Paused || schedule.PauseReason != "更新费用设置暂不可用" || len(schedule.Pending) != 0 || plans[0].Selected[0] {
		t.Fatal("unavailable settings scheduled optional work", schedule, plans[0])
	}
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old-settings-failure", "欢迎朋友。")}
	result, err := resolveRecordingReuse(t.Context(), recordingReuseRequest{Refresh: false, Versions: [][]recordingReadyUnit{old},
		Generate: func(context.Context) ([]recordingReadyUnit, error) {
			t.Fatal("settings outage bought audio instead of reusing the existing version")
			return nil, nil
		}})
	if err != nil || !reflect.DeepEqual(result.Units, old) {
		t.Fatal("optional settings outage damaged independent cached supply", result, err)
	}
}

func TestRecordingRefreshMidPassPriceRiseCanRetainCacheAfterTTSBudgetDecline(t *testing.T) {
	planner, plans, quotes := refreshSchedulePlans(1, 4, 50)
	schedule := newRecordingRefreshSchedule(recordingRefreshBudget{Percent: 50, HeadroomMicros: 1000, RoundLimitMicros: 500}, quotes, plans)
	if !schedule.admitWithPolicy(0, 0, livecost.Summary{}, nil, livecost.DefaultSettings().Policy()) || !planner.BeginAttempt(plans[0], 0) {
		t.Fatal("fixture did not start the planned optional source")
	}
	creditBefore := planner.Tabs[plans[0].TabID]
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old-before-price-rise", "仍然有效的已有表达。")}
	events := &liveCostCallEvents{}
	result, err := resolveRecordingReuse(t.Context(), recordingReuseRequest{Refresh: true, Versions: [][]recordingReadyUnit{old},
		Generate: func(context.Context) ([]recordingReadyUnit, error) {
			// A model already returned successfully. A later TTS transaction
			// using a newly raised quote declines before contacting its supplier.
			events.ChargedMicros.Store(50)
			events.OptionalDeclined.Store(true)
			return nil, errLiveCostOptionalDeclined
		}})
	schedule.complete(0, 0, events)
	if err != nil || result.Generated || !reflect.DeepEqual(result.Units, old) || schedule.SpentMicros != 50 || !schedule.Paused ||
		schedule.replace(planner, plans[0], 0, 0) != 0 || planner.BeginAttempt(plans[0], 1) ||
		!reflect.DeepEqual(planner.Tabs[plans[0].TabID], creditBefore) {
		t.Fatal("price rise erased cache, refunded a paid model or authorized repeated optional attempts", result, schedule, err)
	}
}
