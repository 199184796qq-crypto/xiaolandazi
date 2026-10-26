package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/recordingcache"
)

func TestRecordingRefreshFirstPassResumesPersistedPartialBaseline(t *testing.T) {
	server, quoteVersion, quotePolicy := recordingRefreshQuoteFixture()
	policy := incrementalPolicy(t)
	policy.TextModel, policy.Config.TextProfileID = quotePolicy.TextModel, quotePolicy.Config.TextProfileID
	version := incrementalVersion("欢迎朋友。\n先看看介绍。\n今天先和大家聊聊。")
	version.VoiceIdentity = quoteVersion.VoiceIdentity
	store := &refreshScheduleSummaryStore{}
	server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
	scope, err := recordingcache.ScopeFor(t.TempDir(), 14, 29, version.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	blueprint, err := recordingCacheSnapshot(version, recordingStreamTexts(version), policy, 101)
	if err != nil {
		t.Fatal(err)
	}
	manifest, planner, _, err := openRecordingIncrementalCache(scope, blueprint)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Tabs) != 1 || len(manifest.Tabs[0].Sources) != 3 {
		t.Fatal("fixture must contain three independently cached sources", manifest.Tabs)
	}
	states := func(manifest *recordingcache.Manifest) [][]recordingSentenceState {
		tabs := make([][]recordingSentenceState, len(manifest.Tabs))
		for tab, sources := range manifest.Tabs {
			for _, source := range sources.Sources {
				tabs[tab] = append(tabs[tab], recordingSentenceState{Source: source})
			}
		}
		return tabs
	}
	initial, _ := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, states(manifest),
		func(int, int) []recordingcache.Entry { return nil }, &planner, []string{"intro"}, "101:0", 0, true, nil)
	if store.SummaryCalls != 0 || len(initial.Pending) != 0 {
		t.Fatal("a truly cold initial pass must only supply baseline audio", initial, store)
	}
	// The user stops before the first pass completes. Only its first source
	// has reached the per-source durable save; no completed-round marker exists.
	entry := incrementalEntry(manifest.Tabs[0], 0, 101, false)
	entry.Duration, entry.Segments[0].EndMS = 60_000, 60_000
	manifest.Entries = []recordingcache.Entry{entry}
	manifest.UpdateState, err = json.Marshal(planner)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := scope.SaveActive(manifest); err != nil || !ok {
		t.Fatal("partial baseline save failed", ok, err)
	}
	blueprint, err = recordingCacheSnapshot(version, recordingStreamTexts(version), policy, 102)
	if err != nil {
		t.Fatal(err)
	}
	next, resumed, stats, err := openRecordingIncrementalCache(scope, blueprint)
	if err != nil || stats.InheritedVersions != 1 {
		t.Fatal("the new session failed to inherit the partial cache", stats, err)
	}
	if resumed.Tabs["intro"].CreditMicros != planner.Tabs["intro"].CreditMicros {
		t.Fatal("restart reset the compatible update balance")
	}
	hit := recordingScopedCacheLookup(next)
	candidates := func(tab, index int) []recordingcache.Entry {
		return recordingCacheCandidates(hit, next.Tabs[tab].UnitKeys[index], next.Tabs[tab].ContextKeys[index])
	}
	tabs := states(next)
	schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
		candidates, &resumed, []string{"intro"}, "102:0", 0, true, nil)
	if store.SummaryCalls != 1 || schedule.Paused || !plans[0].Selected[0] || len(schedule.Pending) != 1 ||
		schedule.Pending[recordingRefreshPosition{0, 0}] <= 0 || schedule.ForecastDurationMS < 60_000 {
		t.Fatal("new-session first pass bypassed planning for the inherited warm source", schedule, plans[0], store)
	}
	if !tabs[0][0].UpdateEligible || tabs[0][0].VersionCount != 1 || tabs[0][1].UpdateEligible || tabs[0][2].UpdateEligible ||
		plans[0].Selected[1] || plans[0].Selected[2] {
		t.Fatal("missing baseline audio was confused with optional warm refresh", tabs, plans[0])
	}
	// Exercise the actual reuse resolver with local, P0-approved media stubs:
	// a selected cached source plays its new version now; cold sources supply
	// their original baseline once. No model or TTS provider is contacted.
	generated := 0
	for index, state := range tabs[0] {
		result, err := resolveRecordingReuse(t.Context(), recordingReuseRequest{
			Cached: candidates(0, index), Refresh: plans[0].Selected[index],
			BuildCache: func(_ context.Context, e recordingcache.Entry) ([]recordingReadyUnit, bool) {
				return []recordingReadyUnit{inventoryFixtureUnit(e.AssetID, e.Spoken)}, true
			},
			Generate: func(context.Context) ([]recordingReadyUnit, error) {
				text := state.Source
				if plans[0].Selected[index] {
					text = "朋友，" + text
				}
				if _, err := approvedRecordingSpoken(policy, text); err != nil {
					return nil, err
				}
				generated++
				return []recordingReadyUnit{inventoryFixtureUnit(int64(201+index), text)}, nil
			},
		})
		if err != nil || !result.Generated || recordingAssetIDFromTrack(result.Units[0].Track.ID) != int64(201+index) {
			t.Fatal("first-pass selected update or necessary baseline did not reach playback", index, result, err)
		}
	}
	if generated != 3 {
		t.Fatal("first-pass supply repeated a source", generated)
	}
}

func TestRecordingRefreshFirstPassWarmReuseWhenOptionalUnavailable(t *testing.T) {
	for _, name := range []string{"budget_spent", "disabled", "accounting_unavailable", "pricing_unavailable"} {
		t.Run(name, func(t *testing.T) {
			server, version, quotePolicy := recordingRefreshQuoteFixture()
			policy := incrementalPolicy(t)
			policy.TextModel, policy.Config.TextProfileID = quotePolicy.TextModel, quotePolicy.Config.TextProfileID
			store := &refreshScheduleSummaryStore{}
			settings := livecost.DefaultSettings()
			switch name {
			case "budget_spent":
				store.SummaryValue.TotalMicros = settings.Policy().TotalTargetMicros
			case "disabled":
				settings.MainlineHourlyMicros = 0
			case "accounting_unavailable":
				store.SummaryErr = errors.New("local accounting unavailable")
			case "pricing_unavailable":
				server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
					return livecost.Settings{}, errors.New("local settings unavailable")
				}
			}
			if name != "pricing_unavailable" {
				server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return settings, nil }
			}
			server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
			const source = "欢迎朋友。"
			cached := inventoryFixtureEntry(t, source, inventoryFixtureUnit(101, source))
			tabs := [][]recordingSentenceState{{{Source: source}, {Source: "先看看介绍。"}}}
			planner := &recordingUpdatePlanner{}
			planner.PlanBounded("intro", "old:1", tabs[0], 100, 100, nil)
			credit := planner.Tabs["intro"].CreditMicros
			schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
				func(_ int, index int) []recordingcache.Entry {
					if index == 0 {
						return []recordingcache.Entry{cached}
					}
					return nil
				}, planner, []string{"intro"}, "102:0", 0, true, nil)
			if !schedule.Paused || len(schedule.Pending) != 0 || plans[0].Selected[0] || plans[0].Selected[1] ||
				planner.Tabs["intro"].CreditMicros != credit {
				t.Fatal("unavailable optional work erased credit or forced regeneration", schedule, plans[0], planner)
			}
			result, err := resolveRecordingReuse(t.Context(), recordingReuseRequest{
				Cached: []recordingcache.Entry{cached}, Refresh: plans[0].Selected[0],
				BuildCache: func(_ context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
					return []recordingReadyUnit{inventoryFixtureUnit(entry.AssetID, entry.Spoken)}, true
				},
				Generate: func(context.Context) ([]recordingReadyUnit, error) {
					t.Fatal("warm first-pass reuse started a paid regeneration")
					return nil, nil
				},
			})
			if err != nil || !result.CacheHit || result.Generated || recordingAssetIDFromTrack(result.Units[0].Track.ID) != 101 {
				t.Fatal("original cached wording/audio was not reused", result, err)
			}
		})
	}
}

func TestRecordingRefreshFirstPassFindsWarmInventoryAcrossTabs(t *testing.T) {
	for _, count := range []int{1, recordingCacheMaxVersions} {
		t.Run(fmt.Sprintf("%d_versions", count), func(t *testing.T) {
			server, version, quotePolicy := recordingRefreshQuoteFixture()
			policy := incrementalPolicy(t)
			policy.TextModel, policy.Config.TextProfileID = quotePolicy.TextModel, quotePolicy.Config.TextProfileID
			store := &refreshScheduleSummaryStore{}
			server.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
			const warm = "欢迎朋友，先看看介绍。"
			tabs := [][]recordingSentenceState{{{Source: "今天先和大家聊聊。"}}, {{Source: warm}}}
			for id := 1; id <= count; id++ {
				unit := inventoryFixtureUnit(int64(id), warm)
				unit.Track.DurationMS, unit.Track.Timeline[0].EndMS = 60_000, 60_000
				tabs[1][0].Versions = append(tabs[1][0].Versions, []recordingReadyUnit{unit})
			}
			reads := 0
			schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
				func(int, int) []recordingcache.Entry { reads++; return nil }, &recordingUpdatePlanner{},
				[]string{"cold", "warm"}, "102:0", time.Second, true, nil)
			if reads != 2 || store.SummaryCalls != 1 || schedule.Paused || plans[0].Selected[0] || !plans[1].Selected[0] ||
				schedule.Pending[recordingRefreshPosition{1, 0}] <= 0 || tabs[1][0].VersionCount != count {
				t.Fatal("the first cold tab hid compatible warm audio or inventory was scanned twice", reads, schedule, plans, tabs)
			}
		})
	}
}

func TestRecordingRefreshFirstPassRejectedCacheRemainsCold(t *testing.T) {
	for _, name := range []string{"invalid_audio", "p0_rejected", "stale_context"} {
		t.Run(name, func(t *testing.T) {
			server, version, _ := recordingRefreshQuoteFixture()
			policy := incrementalPolicy(t)
			server.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
				t.Fatal("unusable cached metadata started optional pricing/accounting")
				return livecost.Settings{}, nil
			}
			const source = "欢迎朋友。"
			entry := inventoryFixtureEntry(t, source, inventoryFixtureUnit(101, source))
			entry.UnitKey, entry.ContextKey = "current-source", "current-context"
			switch name {
			case "invalid_audio":
				entry.AssetID = 0
			case "p0_rejected":
				entry.Spoken = "这是全国第一，绝对最好。"
				entry.Segments[0].Text = entry.Spoken
			case "stale_context":
				entry.ContextKey = "old-context"
				entry.Polished = true
				entry.Spoken, entry.Segments[0].Text = "朋友，欢迎来看看。", "朋友，欢迎来看看。"
			}
			tabs := [][]recordingSentenceState{{{Source: source, UpdateEligible: true, VersionCount: 4}}}
			schedule, plans := server.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
				func(int, int) []recordingcache.Entry {
					return recordingCacheCandidates(map[string][]recordingcache.Entry{"current-source": {entry}}, "current-source", "current-context")
				}, &recordingUpdatePlanner{}, []string{"intro"}, "102:0", 0, true, nil)
			if schedule.Paused || len(schedule.Pending) != 0 || plans[0].Selected[0] || tabs[0][0].VersionCount != 0 ||
				tabs[0][0].UpdateEligible || !tabs[0][0].InventoryKnown {
				t.Fatal("rejected/stale cache or stale inventory state made a cold source optionally refreshable", schedule, tabs)
			}
		})
	}
}
