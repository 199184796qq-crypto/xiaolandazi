package httpapi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func strictAdapterServer(t *testing.T, store *adapterCostStore, pool string) *Server {
	s := adapterTestServer(t, store)
	s.liveRoomBudgetLoader = func(_ context.Context, tenant, room, plan, session int64) (string, error) {
		if tenant != 14 || room != 29 || plan != 5 || session != 500 {
			return "", errors.New("wrong live scope")
		}
		return pool, nil
	}
	return s
}

func TestStrictTTSMissingUsageKeepsConservativeQuoteAndKnownUsageSettles(t *testing.T) {
	for _, hasUsage := range []bool{false, true} {
		store := &adapterCostStore{}
		s := strictAdapterServer(t, store, "standard")
		in, out := int64(400), int64(1000)
		usage := ttsgateway.SynthesisUsage{}
		if hasUsage {
			usage = ttsgateway.SynthesisUsage{InputTokens: &in, OutputTokens: &out}
		}
		provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid", Usage: usage}}
		g := ttsgateway.New(provider.Name(), "qwen-audio-3.1-tts-flash")
		g.Register(provider)
		ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Priority: livecost.PriorityNecessary, Reason: "主线原文", Key: "paragraph"})
		if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: strings.Repeat("字", 400)}); err != nil {
			t.Fatal(err)
		}
		if provider.calls.Load() != 1 || len(store.requests) != 1 || len(store.receipts) != 1 {
			t.Fatal(provider.calls.Load(), store.requests, store.receipts)
		}
		r := store.requests[0]
		receipt := store.receipts[0]
		if r.BudgetPool != "standard" || r.BudgetClass != livecost.BudgetMainline || r.EstimatedMicros < 40_000 {
			t.Fatal(r)
		}
		if hasUsage {
			if !receipt.ActualKnown || receipt.ActualMicros != 12_600 || receipt.EstimatedMicros != nil {
				t.Fatal(receipt)
			}
		} else if receipt.ActualKnown || receipt.EstimatedMicros == nil || *receipt.EstimatedMicros != r.EstimatedMicros {
			t.Fatal("missing usage was replaced with a cheap character estimate", receipt, r)
		}
	}
}

func TestStrictVolcenginePricesPunctuationWithoutHanDoubleCount(t *testing.T) {
	store := &adapterCostStore{}
	s := strictAdapterServer(t, store, "high_fidelity")
	scope := liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Priority: livecost.PriorityNecessary, Reason: "主线", Key: "volc"}
	o := &liveTTSObserver{s: s, scope: scope}
	a := ttsgateway.SynthesisAttempt{Provider: ttsgateway.ProviderVolcengine, Model: ttsgateway.VolcengineICL2, VoiceID: "A", SentTextCharacters: 401, BillableCharacters: 801, CharacterRule: ttsgateway.BillableCharacterRule}
	key, err := o.BeforeSynthesis(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	chars := int64(401)
	if err := o.AfterSynthesis(t.Context(), ttsgateway.SynthesisReceipt{SynthesisAttempt: a, Ticket: key, Outcome: ttsgateway.SynthesisSucceeded, Usage: ttsgateway.SynthesisUsage{Characters: &chars}}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 || store.requests[0].EstimatedMicros != 120_300 || store.requests[0].BudgetPool != "high_fidelity" || store.receipts[0].ActualMicros != 120_300 {
		t.Fatal(store.requests, store.receipts)
	}
}

func TestStrictNecessaryTTSStopsBeforeProviderWhenBudgetOrLedgerUnavailable(t *testing.T) {
	for _, name := range []string{"budget", "ledger", "config", "session"} {
		t.Run(name, func(t *testing.T) {
			store := &adapterCostStore{}
			if name == "ledger" {
				store.failReserve = true
			}
			if name == "budget" {
				r := livecost.Request{Key: "already-paid", TenantID: 14, RoomID: 29, Kind: livecost.KindTTS, BudgetClass: livecost.BudgetMainline}
				store.entries = map[string]livecost.Entry{"old": {Request: r, Status: livecost.StatusSettled, ChargedMicros: 800_000}}
			}
			s := strictAdapterServer(t, store, "standard")
			if name == "config" {
				s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
					return livecost.Settings{}, errors.New("unavailable")
				}
			}
			if name == "session" {
				s.liveRoomBudgetLoader = func(context.Context, int64, int64, int64, int64) (string, error) {
					return "", errors.New("live session replaced")
				}
			}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
			g := ttsgateway.New(provider.Name(), "qwen-audio-3.1-tts-flash")
			g.Register(provider)
			ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Priority: livecost.PriorityNecessary, Reason: "主线", Key: "new"})
			_, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "测试正文"})
			if err == nil || provider.calls.Load() != 0 || !ttsgateway.DefinitelyNotSent(err) {
				t.Fatal("necessary supply bypassed budget", err, provider.calls.Load())
			}
		})
	}
}

func TestStrictModelUsesRoomPoolAndUTF8UpperEstimateWithoutChangingPrompt(t *testing.T) {
	store := &adapterCostStore{}
	s := strictAdapterServer(t, store, "high_fidelity")
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Reason: "直播必要互动模型调用", Key: "question"})
	req := agentgateway.Request{Provider: "aliyun_qwen", Model: "qwen3.7-flash", MaxTokens: 900, Messages: []agentgateway.Message{{Role: "user", Content: strings.Repeat("字", 100)}}}
	if err := s.beginLiveModelCost(ctx, -1, "interaction", "", req); err != nil {
		t.Fatal(err)
	}
	r := store.requests[0]
	if r.BudgetPool != "high_fidelity" || r.BudgetClass != livecost.BudgetInteraction || r.InputTokens != 428 || r.OutputTokens != 900 || req.Messages[0].Content != strings.Repeat("字", 100) {
		t.Fatal(r, req)
	}
	if err := s.beginLiveModelCost(ctx, -2, "interaction", "", agentgateway.Request{Messages: req.Messages}); err == nil {
		t.Fatal("uncapped output admitted")
	}
	if err := s.beginLiveModelCost(ctx, -3, "agent_cycle_turn", "", map[string]any{"session_id": "legacy"}); err == nil {
		t.Fatal("opaque legacy live request bypassed accounting")
	}
}

func TestStrictOptionalPauseAndRuntimeScopeInheritance(t *testing.T) {
	store := &adapterCostStore{}
	s := strictAdapterServer(t, store, "standard")
	cfg := livecost.DefaultSettings()
	cfg.MainlineHourlyMicros = 0
	cfg.OptionalHourlyMicros = 0
	s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return cfg, nil }
	ctx := agentgateway.WithRoomModelScope(t.Context(), agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 500})
	ctx = s.withLiveCostTTS(ctx, liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, Priority: livecost.PriorityOptional, Reason: "话术后台改写", Key: "rewrite"})
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "qwen-audio-3.1-tts-flash")
	g.Register(provider)
	_, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "测试正文"})
	if !errors.Is(err, errLiveCostOptionalDeclined) || provider.calls.Load() != 0 || len(store.requests) != 0 {
		t.Fatal("optional zero setting was bypassed", err, provider.calls.Load(), store.requests)
	}
	cfg.OptionalHourlyMicros = livecost.DefaultSettings().OptionalHourlyMicros
	cfg.MainlineHourlyMicros = livecost.DefaultSettings().MainlineHourlyMicros
	cfg.Revision++
	if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "测试正文"}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 || store.requests[0].SessionID != 500 || store.requests[0].BudgetClass != livecost.BudgetMainline {
		t.Fatal("rewrite TTS lost live identity/class", store.requests)
	}
	ctx = withLiveCostScope(ctx, liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, Priority: livecost.PriorityOptional, Reason: "话术后台改写", Key: "rewrite-model"})
	if err := s.beginLiveModelCost(ctx, -10, "script_rewrite", "", agentgateway.Request{MaxTokens: 2400, Messages: []agentgateway.Message{{Role: "user", Content: "正文"}}}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 2 || store.requests[1].BudgetClass != livecost.BudgetRewrite {
		t.Fatal("rewrite model consumed the interaction allowance", store.requests)
	}
}

// A declined paragraph must be retryable when budget returns. Once purchased,
// its exact cached bytes remain reusable with zero new provider calls.
func TestStrictParagraphBudgetDeclineDoesNotPoisonPaidCache(t *testing.T) {
	store := &adapterCostStore{entries: map[string]livecost.Entry{"old": {
		Request: livecost.Request{TenantID: 14, RoomID: 29, Kind: livecost.KindTTS},
		Status:  livecost.StatusSettled, ChargedMicros: 800_000,
	}}}
	s := strictAdapterServer(t, store, "standard")
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "qwen-audio-3.1-tts-flash")
	g.Register(provider)
	unit := volcengineScriptUnit{Dir: t.TempDir(), Key: "budget-paragraph"}
	ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, Priority: livecost.PriorityNecessary, Reason: "主线原文", Key: unit.Key})
	generate := func(c context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		out, err := g.SynthesizeURL(c, ttsgateway.SynthesizeRequest{VoiceID: "A", Text: "测试正文"})
		if err != nil {
			return out, err
		}
		// Local media stand-in: no download, network or real provider.
		if err := os.WriteFile(path, []byte("cached-paid-audio"), 0600); err != nil {
			return out, err
		}
		out.AudioFile = path
		return out, nil
	}
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); !errors.Is(err, livecost.ErrRoomBudget) || provider.calls.Load() != 0 {
		t.Fatal(err, provider.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(unit.Dir, "paragraph-job.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsent admission failure left a permanent purchase fence", err)
	}
	delete(store.entries, "old") // The test's rolling window now has headroom.
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); err != nil {
		t.Fatal(err)
	}
	store.entries["old"] = livecost.Entry{Request: livecost.Request{TenantID: 14, RoomID: 29, Kind: livecost.KindTTS}, Status: livecost.StatusSettled, ChargedMicros: 800_000}
	if _, err := prepareScriptFirstPaid(ctx, unit, generate); err != nil || provider.calls.Load() != 1 {
		t.Fatal("cache playback purchased again", err, provider.calls.Load())
	}
}

type strictScriptModelProvider struct {
	calls atomic.Int64
	seen  []agentgateway.Request
}

func (*strictScriptModelProvider) Name() string { return agentgateway.ProviderQwen }
func (p *strictScriptModelProvider) Complete(_ context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	p.calls.Add(1)
	p.seen = append(p.seen, r)
	return agentgateway.Response{Provider: r.Provider, Model: r.Model, Text: "改好的句子。", InputTokens: 100, OutputTokens: 50}, nil
}

func TestStrictScriptWorkerModelsUseRewriteBudgetBeforeAnyTransport(t *testing.T) {
	store := &adapterCostStore{entries: map[string]livecost.Entry{"old-rewrites": {
		Request: livecost.Request{TenantID: 14, RoomID: 29, Kind: livecost.KindModel, BudgetClass: livecost.BudgetRewrite},
		Status:  livecost.StatusSettled, ChargedMicros: 50_000,
	}}}
	s := strictAdapterServer(t, store, "standard")
	provider := &strictScriptModelProvider{}
	s.agentGateway = agentgateway.New(provider.Name(), agentgateway.DefaultQwenModel)
	s.agentGateway.Register(provider)
	ctx := agentgateway.WithRoomModelScope(t.Context(), agentgateway.RoomModelScope{TenantID: 14, RoomID: 29, RuntimeID: 500})
	ctx = withLiveCostScope(ctx, liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, Priority: livecost.PriorityOptional, Reason: "话术后台改写", Key: "rewrite-task"})
	ctx = withScriptModelCostSteps(ctx)
	if _, ok := s.askRewriteSentences(ctx, model.LiveAgentPlanVersion{}, []string{"原句。"}); ok || provider.calls.Load() != 0 {
		t.Fatal("sentence repair bypassed the paid gate")
	}
	if _, ok := s.askRewriteOpening(ctx, "原文。"); ok || provider.calls.Load() != 0 {
		t.Fatal("opening repair bypassed the paid gate")
	}
	stages := []string{"script_rewrite", "script_rewrite_repair", "script_rewrite_extend", "script_rewrite_opening"}
	for _, stage := range stages {
		if _, err := s.completePaidScriptModel(ctx, agentgateway.Request{Stage: stage, MaxTokens: 800, Messages: []agentgateway.Message{{Role: "user", Content: "原提示词"}}}); !errors.Is(err, livecost.ErrRoomBudget) {
			t.Fatal(stage, err)
		}
	}
	if provider.calls.Load() != 0 || len(store.receipts) != 0 {
		t.Fatal("rewrite model paid despite a full category", provider.calls.Load(), store.receipts)
	}
	delete(store.entries, "old-rewrites")
	for _, stage := range stages {
		if _, err := s.completePaidScriptModel(ctx, agentgateway.Request{Stage: stage, MaxTokens: 800, Messages: []agentgateway.Message{{Role: "user", Content: "原提示词"}}}); err != nil {
			t.Fatal(stage, err)
		}
	}
	if provider.calls.Load() != 4 || len(store.receipts) != 4 {
		t.Fatal(provider.calls.Load(), store.receipts)
	}
	for _, r := range store.entries {
		if r.Request.BudgetClass != livecost.BudgetRewrite || r.Request.SessionID != 500 || r.Status != livecost.StatusSettled {
			t.Fatal("unaccounted rewrite model work", r)
		}
	}
	for _, r := range provider.seen {
		if len(r.Messages) != 1 || r.Messages[0].Content != "原提示词" || r.MaxTokens != 800 {
			t.Fatal("billing changed the model request", r)
		}
	}
}
