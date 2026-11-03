package httpapi

import (
	"context"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/recordingcache"
	"livecompanion/management/internal/ttsgateway"
)

func TestInteractionModelAndTTSIgnoreFullMainlineAndOptionalZero(t *testing.T) {
	for _, pool := range []string{"standard", "high_fidelity"} {
		t.Run(pool, func(t *testing.T) {
			store := &adapterCostStore{entries: map[string]livecost.Entry{
				"mainline-full":   {Request: livecost.Request{TenantID: 14, RoomID: 29, BudgetClass: livecost.BudgetMainline}, Status: livecost.StatusSettled, ChargedMicros: 30_000_000},
				"old-interaction": {Request: livecost.Request{TenantID: 14, RoomID: 29, BudgetClass: livecost.BudgetInteraction}, Status: livecost.StatusUnknown, ReservedMicros: 20_000_000},
			}}
			s := strictAdapterServer(t, store, pool)
			cfg := livecost.DefaultSettings()
			cfg.MainlineHourlyMicros, cfg.OptionalHourlyMicros = 0, 0
			s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return cfg, nil }
			scope := liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Key: "interaction-model", Reason: "互动回答", Priority: livecost.PriorityOptional, Attribution: &livecost.Attribution{Source: "interaction"}}
			ctx := withLiveCostScope(t.Context(), scope)
			calls := 0
			request := agentgateway.Request{Provider: "aliyun_qwen", Model: "qwen3.7-flash", Stage: "interaction", MaxTokens: 100, Messages: []agentgateway.Message{{Role: "user", Content: "原问题"}}}
			_, err := s.completeScopedPaidModel(ctx, request, func(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
				calls++
				if len(r.Messages) != 1 || r.Messages[0].Content != "原问题" {
					t.Fatal("budget changed prompt", r)
				}
				return agentgateway.Response{Provider: r.Provider, Model: r.Model, Text: "回答正文", InputTokens: 10, OutputTokens: 10}, nil
			})
			if err != nil || calls != 1 {
				t.Fatal("interaction model was capped", err, calls)
			}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
			gateway := ttsgateway.New(provider.Name(), "qwen-audio-3.1-tts-flash")
			gateway.Register(provider)
			scope.Key = "interaction-tts"
			ctx = s.withLiveCostTTS(t.Context(), scope)
			if _, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "回答正文"}); err != nil {
				t.Fatal("interaction audio was capped", err)
			}
			if _, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "回答正文"}); err == nil || provider.calls.Load() != 1 {
				t.Fatal("uncapped interaction lost purchase fence", err, provider.calls.Load())
			}
			if len(store.receipts) != 2 {
				t.Fatal("interaction lost accounting", store.receipts)
			}
			for _, r := range store.requests {
				if r.BudgetClass != livecost.BudgetInteraction || r.BudgetPool != pool {
					t.Fatal(r)
				}
			}
			if store.entries["old-interaction"].Status != livecost.StatusUnknown {
				t.Fatal("old accounting modified")
			}
		})
	}
}

func TestMainlinePlanningExcludesAllInteractionCosts(t *testing.T) {
	for _, pool := range []string{"standard", "high_fidelity"} {
		b, _ := livecost.RoomBudgetForPool(pool)
		base := livecost.Summary{TotalMicros: 300_000, MainlineMicros: 200_000, RewriteMicros: 100_000}
		want := recordingDynamicRefreshBudgetForDurationWithPolicy(base, 200_000, time.Hour, 10*time.Minute, b.Policy())
		base.TotalMicros += 20_000_000
		base.InteractionMicros = 20_000_000
		base.OptionalMicros = 20_000_000
		got := recordingDynamicRefreshBudgetForDurationWithPolicy(base, 200_000, time.Hour, 10*time.Minute, b.Policy())
		if got != want || !got.Unlimited || got.Percent != 100 {
			t.Fatal(pool, got, want)
		}
		base.RewriteMicros = b.Total - base.MainlineMicros
		if got := recordingDynamicRefreshBudgetForDurationWithPolicy(base, 200_000, time.Hour, 10*time.Minute, b.Policy()); !got.Unlimited || got.Percent != 100 {
			t.Fatal("full mainline planning still buys", got)
		}
	}
}

func TestMainlineScheduleKeepsPoolBudgetThroughAdmission(t *testing.T) {
	s, version, quotePolicy := recordingRefreshQuoteFixture()
	version.VoiceIdentity.Provider = ttsgateway.ProviderQwen
	policy := incrementalPolicy(t)
	policy.TextModel, policy.Config.TextProfileID = quotePolicy.TextModel, quotePolicy.Config.TextProfileID
	store := &refreshScheduleSummaryStore{SummaryValue: livecost.Summary{TotalMicros: 20_300_000, MainlineMicros: 200_000, RewriteMicros: 100_000, InteractionMicros: 20_000_000}}
	s.liveCostRequests = livecost.New(store, livecost.DefaultPolicy())
	unit := inventoryFixtureUnit(501, "欢迎朋友。")
	unit.Track.DurationMS, unit.Track.Timeline[0].EndMS = 60_000, 60_000
	tabs := [][]recordingSentenceState{{{Source: "欢迎朋友。", Versions: [][]recordingReadyUnit{{unit}}}}}
	schedule, _ := s.prepareRecordingRefreshSchedule(t.Context(), 14, 29, version, policy, tabs,
		func(int, int) []recordingcache.Entry { return nil }, &recordingUpdatePlanner{}, []string{"one"}, "631:2", time.Hour, false, nil)
	if schedule.Paused || len(schedule.Pending) != 1 || !schedule.Budget.Unlimited || store.SummaryCalls != 0 {
		t.Fatal("planning used interaction or old sublimits", schedule)
	}
	if !s.admitRecordingRefresh(t.Context(), schedule, 14, 29, 0, 0) {
		t.Fatal("admission lost the mainline-only policy", schedule)
	}
	store.SummaryValue.RewriteMicros = 1_100_000
	if !s.admitRecordingRefresh(t.Context(), schedule, 14, 29, 0, 0) {
		t.Fatal("new rewrite spending bypassed shared mainline allowance")
	}
}
