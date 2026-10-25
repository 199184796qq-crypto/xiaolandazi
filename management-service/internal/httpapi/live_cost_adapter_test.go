package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

type adapterCostStore struct {
	mu                            sync.Mutex
	entries                       map[string]livecost.Entry
	requests                      []livecost.Request
	receipts                      []livecost.Receipt
	deny, failReserve, failFinish bool
	limitMicros                   int64
	afterReserve                  func()
}

func (s *adapterCostStore) ReserveLiveCost(_ context.Context, r livecost.Request, _ livecost.Policy, _ time.Time) (livecost.Reservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failReserve {
		return livecost.Reservation{}, errors.New("db unavailable")
	}
	if s.entries == nil {
		s.entries = map[string]livecost.Entry{}
	}
	if old, ok := s.entries[r.Key]; ok {
		if !reflect.DeepEqual(old.Request, r) {
			return livecost.Reservation{}, livecost.ErrRequestConflict
		}
		return livecost.Reservation{Existing: true, Status: old.Status}, nil
	}
	s.requests = append(s.requests, r)
	if (s.deny || (s.limitMicros > 0 && r.EstimatedMicros > s.limitMicros)) && r.Priority == livecost.PriorityOptional {
		return livecost.Reservation{Allowed: false, Status: livecost.StatusDeclined}, nil
	}
	s.entries[r.Key] = livecost.Entry{Request: r, Status: livecost.StatusReserved, ReservedMicros: r.EstimatedMicros}
	if s.afterReserve != nil {
		s.afterReserve()
	}
	return livecost.Reservation{Allowed: true, Status: livecost.StatusReserved, EstimatedMicros: r.EstimatedMicros}, nil
}
func (s *adapterCostStore) FinishLiveCost(_ context.Context, _ livecost.Scope, key string, r livecost.Receipt, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failFinish {
		return errors.New("db unavailable")
	}
	entry, ok := s.entries[key]
	if !ok {
		return livecost.ErrRequestNotFound
	}
	next, err := livecost.ApplyReceipt(entry, r)
	if err != nil {
		return err
	}
	s.entries[key] = next
	s.receipts = append(s.receipts, r)
	return nil
}
func (s *adapterCostStore) LiveCostSummary(context.Context, livecost.Scope, livecost.Policy, time.Time) (livecost.Summary, error) {
	return livecost.Summary{}, nil
}
func adapterTestServer(t *testing.T, store *adapterCostStore) *Server {
	t.Helper()
	policy := livecost.DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	return &Server{liveCostRequests: livecost.New(store, policy)}
}

type adapterTTSProvider struct {
	calls  atomic.Int64
	result ttsgateway.SynthesizeResponse
	err    error
}

func (*adapterTTSProvider) Name() string { return "adapter_tts" }
func (p *adapterTTSProvider) SynthesizeURL(context.Context, ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	p.calls.Add(1)
	return p.result, p.err
}
func (*adapterTTSProvider) CloneVoice(context.Context, ttsgateway.CloneRequest) (ttsgateway.CloneResponse, error) {
	return ttsgateway.CloneResponse{}, nil
}

func TestLiveCostTTSOptionalDeclineAndUnknownDoNotCallSupplierAgain(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "optional_decline"}[optional], func(t *testing.T) {
			store := &adapterCostStore{deny: optional}
			s := adapterTestServer(t, store)
			provider := &adapterTTSProvider{err: errors.New("response lost")}
			g := ttsgateway.New(provider.Name(), "m")
			g.Register(provider)
			scope := liveCostCallScope{TenantID: 14, RoomID: 29, PlanID: 5, SessionID: 500, RunID: "run", Key: "stable-unit", Reason: "必要声音供料", Priority: livecost.PriorityNecessary}
			if optional {
				scope.Priority = livecost.PriorityOptional
			}
			ctx := s.withLiveCostTTS(t.Context(), scope)
			for i := 0; i < 2; i++ {
				if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "voice", Text: "你好。"}); err == nil {
					t.Fatal("expected decline/unknown error")
				}
			}
			want := int64(1)
			if optional {
				want = 0
			}
			if provider.calls.Load() != want {
				t.Fatalf("supplier calls=%d want=%d", provider.calls.Load(), want)
			}
			if !optional && (len(store.receipts) != 1 || store.receipts[0].Status != livecost.ReceiptUnknown) {
				t.Fatalf("unknown response refunded: %+v", store.receipts)
			}
		})
	}
}

func TestLiveCostTTSSupplierUsageNeedsExactContractAndZeroRemainsEstimate(t *testing.T) {
	for _, name := range []string{"missing", "zero", "estimate", "contract", "wrong_model"} {
		t.Run(name, func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			value := int64(5)
			usage := ttsgateway.SynthesisUsage{Present: true, Characters: &value}
			if name == "missing" {
				usage = ttsgateway.SynthesisUsage{}
			}
			if name == "zero" {
				value = 0
			}
			if name == "contract" || name == "wrong_model" {
				m := "m"
				if name == "wrong_model" {
					m = "other"
				}
				s.liveCostPrices = []liveCostPriceRule{{Provider: "adapter_tts", Model: m, Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 100}}
			}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid/?Signature=secret", ProviderRequestID: "provider-123", Usage: usage}}
			g := ttsgateway.New(provider.Name(), "m")
			g.Register(provider)
			out, err := g.SynthesizeURL(s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "unit", Reason: "声音供料"}), ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"})
			if err != nil || out.AudioURL == "" || out.AccountingPending || len(store.receipts) != 1 || provider.calls.Load() != 1 {
				t.Fatalf("out=%+v err=%v receipts=%+v", out, err, store.receipts)
			}
			r := store.receipts[0]
			if name == "contract" {
				if !r.ActualKnown || r.ActualMicros != 500 || r.UsageSource != livecost.SourceSupplierUsage || r.EstimatedMicros != nil {
					t.Fatalf("contract not settled: %+v", r)
				}
			} else {
				if r.ActualKnown || r.UsageSource != livecost.SourceSentTextEstimate || r.EstimatedMicros == nil {
					t.Fatalf("estimate mislabelled actual: %+v", r)
				}
				want := int64(800)
				if name == "missing" {
					want = 480
				}
				if name == "zero" {
					want = 0
				}
				if *r.EstimatedMicros != want {
					t.Fatalf("estimate=%d want=%d", *r.EstimatedMicros, want)
				}
			}
			raw, _ := json.Marshal(store.requests)
			if strings.Contains(string(raw), "你好") || strings.Contains(string(raw), "Signature") || strings.Contains(string(raw), "audio.invalid") {
				t.Fatal("cost request leaked text or URL")
			}
		})
	}
}

func TestLiveCostTTSSuccessSurvivesLedgerFinishFailure(t *testing.T) {
	store := &adapterCostStore{failFinish: true}
	s := adapterTestServer(t, store)
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	out, err := g.SynthesizeURL(s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "unit", Reason: "声音供料"}), ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"})
	if err != nil || out.AudioURL == "" || !out.AccountingPending || provider.calls.Load() != 1 {
		t.Fatalf("successful paid result lost: %+v %v", out, err)
	}
}

func TestLiveCostNecessaryDBFailureWritesWALAndAllowsOnePaidModel(t *testing.T) {
	store := &adapterCostStore{failReserve: true, failFinish: true}
	s := adapterTestServer(t, store)
	var calls atomic.Int64
	call := func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls.Add(1)
		return agentgateway.Response{Text: "可用文案", InputTokens: 20, OutputTokens: 10}, nil
	}
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	request := agentgateway.Request{Stage: "answer", Provider: "qwen", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "问题正文"}}, MaxTokens: 100}
	result, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "decision-1", call)
	if err != nil || result.Text != "可用文案" {
		t.Fatalf("necessary result lost: %+v %v", result, err)
	}
	if _, err = s.CompleteLivePaidModel(t.Context(), session, request, 5, "decision-1", call); err == nil || calls.Load() != 1 {
		t.Fatalf("unknown/degraded request resent: calls=%d err=%v", calls.Load(), err)
	}
}

func TestLiveCostModelUsageReferencePriceAndConfiguredCachedDiscount(t *testing.T) {
	for _, contract := range []bool{false, true} {
		t.Run(map[bool]string{false: "reference", true: "contract"}[contract], func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			if contract {
				s.liveCostPrices = []liveCostPriceRule{{Provider: "qwen", Model: "m", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 200000, CachedInputMicrosPerMillion: 40000, OutputMicrosPerMillion: 800000}}
			}
			calls := 0
			session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
			request := agentgateway.Request{Stage: "answer", Provider: "qwen", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "问题正文"}}, MaxTokens: 100}
			result, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "decision-1", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
				calls++
				return agentgateway.Response{Text: "答复", Provider: "qwen", Model: "m", InputTokens: 100, OutputTokens: 10, CachedInputTokens: 50}, nil
			})
			if err != nil || result.Text == "" || calls != 1 || len(store.receipts) != 1 {
				t.Fatalf("result=%+v err=%v receipts=%+v", result, err, store.receipts)
			}
			r := store.receipts[0]
			if contract {
				if !r.ActualKnown || r.ActualMicros != 20 || r.UsageSource != livecost.SourceSupplierUsage {
					t.Fatalf("configured fee=%+v", r)
				}
			} else {
				if r.ActualKnown || r.EstimatedMicros == nil || *r.EstimatedMicros != 20 || r.UsageSource != livecost.SourceSentTextEstimate {
					t.Fatalf("reference estimate=%+v", r)
				}
			}
		})
	}
}

func TestLiveCostModelZeroUsageAndSimulationDoNotPretendActual(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	request := agentgateway.Request{Stage: "answer", Provider: "qwen", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "问题"}}}
	call := func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{Text: "答复"}, nil
	}
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "decision", call); err != nil {
		t.Fatal(err)
	}
	if len(store.receipts) != 1 || store.receipts[0].ActualKnown || store.receipts[0].EstimatedMicros != nil || store.receipts[0].UsageSource != livecost.SourceSentTextEstimate {
		t.Fatalf("missing usage invented: %+v", store.receipts)
	}
	session.ID = 0
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "simulation", call); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 {
		t.Fatal("simulation billed")
	}
}

func TestLiveCostModelOnlyActualGatewayRequestsAreReserved(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, Key: "k"})
	for _, request := range []any{map[string]string{"text": "不是模型调用"}, ttsgateway.SynthesizeRequest{Text: "声音"}, nil} {
		if err := s.beginLiveModelCost(ctx, 1, "mainline_tts", "", request); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.requests) != 0 {
		t.Fatalf("non-model requests charged: %+v", store.requests)
	}
	if err := s.beginLiveModelCost(ctx, 2, "tts_p0_sentence_rewrite", "", agentgateway.Request{Stage: "tts_p0_sentence_rewrite", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "修正原句"}}}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 1 || store.requests[0].Kind != livecost.KindModel {
		t.Fatal("actual TTS-preparation model request was skipped")
	}
}

func TestLiveCostOptionalDBUnavailableRejectsBeforeSupplier(t *testing.T) {
	store := &adapterCostStore{failReserve: true}
	s := adapterTestServer(t, store)
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, Key: "optional", Priority: livecost.PriorityOptional})
	err := s.beginLiveModelCost(ctx, 1, "polish", "", agentgateway.Request{Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "问题"}}})
	if !errors.Is(err, errLiveCostOptionalDeclined) {
		t.Fatalf("optional not deferred: %v", err)
	}
}

func TestLiveCostConfigContractMustIncludeUnitAndPositivePrices(t *testing.T) {
	for _, r := range []liveCostPriceRule{{Provider: "qwen", Model: "m", Kind: livecost.KindTTS, Unit: "characters"}, {Provider: "qwen", Model: "m", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 200000}, {Provider: "qwen", Model: "m", Kind: livecost.KindModel, Unit: "characters", CharacterMicros: 160}} {
		if validLiveCostPriceRule(r) {
			t.Fatalf("incomplete/mixed contract accepted: %+v", r)
		}
	}
}

func TestLiveCostReplayRepairsOnlyAccounting(t *testing.T) {
	store := &adapterCostStore{}
	policy := livecost.DefaultPolicy()
	policy.SpoolDir = t.TempDir()
	s := &Server{liveCostRequests: livecost.New(store, policy)}
	store.failReserve = true
	scope := liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "unit", Reason: "声音供料"}
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	if _, err := g.SynthesizeURL(s.withLiveCostTTS(t.Context(), scope), ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err != nil {
		t.Fatal(err)
	}
	store.failReserve = false
	s.liveCostReplayAt = time.Time{}
	s.replayLiveCostSpool(t.Context(), s.liveCostRequests)
	if provider.calls.Load() != 1 {
		t.Fatal("replay invoked supplier")
	}
	files, err := os.ReadDir(policy.SpoolDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".json") {
			t.Fatalf("receipt not replayed: %s", file.Name())
		}
	}
}

func TestLiveCostPartiallyMissingModelTokensStayEstimate(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	s.liveCostPrices = []liveCostPriceRule{{Provider: "qwen", Model: "m", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 200000, OutputMicrosPerMillion: 800000}}
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	request := agentgateway.Request{Stage: "answer", Provider: "qwen", Model: "m", MaxTokens: 100, Messages: []agentgateway.Message{{Role: "user", Content: "问题正文"}}}
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "partial", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{Text: "实际答复", Provider: "qwen", Model: "m", InputTokens: 20}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r := store.receipts[0]
	if r.ActualKnown || r.EstimatedMicros == nil || *r.EstimatedMicros != 84 || r.UsageSource != livecost.SourceSentTextEstimate {
		t.Fatalf("unreported output tokens counted as free actual usage: %+v", r)
	}
	if liveCostCachedRate(s.liveCostPrices[0]) != 200000 {
		t.Fatal("missing cached price treated as free")
	}
}

func TestLivePaidContextSimulationCannotInheritPaidTTSObserver(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "parent", Reason: "必要供料"})
	ctx = s.LivePaidContext(ctx, model.LiveRuntimeSession{TenantID: 14, RoomID: 29}, "simulation", 5, "child")
	if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err != nil {
		t.Fatal(err)
	}
	if len(store.requests) != 0 || provider.calls.Load() != 1 {
		t.Fatal("simulation accidentally recorded live spend")
	}
}

func TestLiveCostExpensiveTTSContractReservesBeforePaidCall(t *testing.T) {
	store := &adapterCostStore{limitMicros: 1000}
	s := adapterTestServer(t, store)
	s.liveCostPrices = []liveCostPriceRule{{Provider: "adapter_tts", Model: "m", Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 100000}}
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "expensive", Priority: livecost.PriorityOptional})
	if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err == nil {
		t.Fatal("expensive optional synthesis was not deferred")
	}
	if provider.calls.Load() != 0 || len(store.requests) != 1 || store.requests[0].EstimatedMicros != 300000 {
		t.Fatalf("quote missed expensive contract: calls=%d requests=%+v", provider.calls.Load(), store.requests)
	}
}

func TestLiveCostExpensiveModelContractReservesBeforePaidCall(t *testing.T) {
	for _, variant := range []string{"expensive", "mismatch", "negative"} {
		t.Run(variant, func(t *testing.T) {
			store := &adapterCostStore{limitMicros: 1000}
			s := adapterTestServer(t, store)
			rule := liveCostPriceRule{Provider: "qwen", Model: "m", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 2000000000, OutputMicrosPerMillion: 8000000000}
			if variant == "mismatch" {
				rule.Model = "other"
			}
			if variant == "negative" {
				rule.OutputMicrosPerMillion = -1
			}
			s.liveCostPrices = []liveCostPriceRule{rule}
			ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "model", Priority: livecost.PriorityOptional})
			err := s.beginLiveModelCost(ctx, 1, "polish", "", agentgateway.Request{Stage: "polish", Provider: "qwen", Model: "m", MaxTokens: 100, Messages: []agentgateway.Message{{Role: "user", Content: "问题正文"}}})
			calls := 0
			if err == nil {
				calls++
			}
			if variant == "expensive" {
				if !errors.Is(err, errLiveCostOptionalDeclined) || calls != 0 || store.requests[0].EstimatedMicros != 840000 {
					t.Fatalf("expensive request sent: err=%v calls=%d requests=%+v", err, calls, store.requests)
				}
			} else if err != nil || calls != 1 || store.requests[0].EstimatedMicros != 84 {
				t.Fatalf("nonmatching/invalid price applied: err=%v calls=%d requests=%+v", err, calls, store.requests)
			}
		})
	}
}

func TestLiveCostTokenTTSWithoutPreflightBoundDefersOptional(t *testing.T) {
	for _, contract := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown_price", true: "configured_tokens"}[contract], func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			m := "qwen-audio-3.1-tts-flash"
			if contract {
				s.liveCostPrices = []liveCostPriceRule{{Provider: "adapter_tts", Model: m, Kind: livecost.KindTTS, Unit: "tokens", InputMicrosPerMillion: 200000, OutputMicrosPerMillion: 800000}}
			}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
			g := ttsgateway.New(provider.Name(), m)
			g.Register(provider)
			ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "token-optional", Priority: livecost.PriorityOptional})
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err == nil || provider.calls.Load() != 0 {
				t.Fatalf("unbounded optional token TTS sent: %v calls=%d", err, provider.calls.Load())
			}
			ctx = s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "token-necessary", Priority: livecost.PriorityNecessary, Reason: "必要声音供料"})
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err != nil || provider.calls.Load() != 1 {
				t.Fatalf("necessary token TTS blocked: %v calls=%d", err, provider.calls.Load())
			}
			if len(store.requests) != 1 || !strings.Contains(store.requests[0].Reason, "Token计费") || store.receipts[0].ActualKnown {
				t.Fatalf("unknown token price not recorded honestly: requests=%+v receipts=%+v", store.requests, store.receipts)
			}
		})
	}
}

func TestLiveCostInvalidPartialTTSUsageCannotReduceEstimate(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	zero := int64(0)
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid", Usage: ttsgateway.SynthesisUsage{Present: true, Invalid: true, Characters: &zero}}}
	g := ttsgateway.New(provider.Name(), "m")
	g.Register(provider)
	out, err := g.SynthesizeURL(s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "bad-usage", Reason: "必要供料"}), ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"})
	if err != nil || out.AudioURL == "" || len(store.receipts) != 1 {
		t.Fatalf("successful audio lost: %v %+v", err, out)
	}
	r := store.receipts[0]
	if r.ActualKnown || r.Characters != 3 || r.EstimatedMicros == nil || *r.EstimatedMicros != 480 {
		t.Fatalf("invalid supplier usage reduced price: %+v", r)
	}
}

func TestLiveCostUnknownModelCannotEscapeByChangingPrompt(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	request := agentgateway.Request{Stage: "polish", Provider: "qwen", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "随机提示A"}}}
	calls := 0
	call := func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, errors.New("supplier response lost")
	}
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "same-logical-unit", call); err == nil {
		t.Fatal("expected unknown response")
	}
	request.Messages[0].Content = "随机提示B"
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "same-logical-unit", call); err == nil || calls != 1 {
		t.Fatalf("random prompt bypassed unknown request: calls=%d err=%v", calls, err)
	}
}

func TestLiveCostKnownSuccessAllowsOnlyExplicitNewModelAttempt(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	request := agentgateway.Request{Stage: "polish", Provider: "qwen", Model: "m", Messages: []agentgateway.Message{{Role: "user", Content: "首个请求"}}}
	calls := 0
	call := func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{Text: "已完成", Provider: "qwen", Model: "m", InputTokens: 10, OutputTokens: 5}, nil
	}
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "unit:attempt:1", call); err != nil {
		t.Fatal(err)
	}
	request.Messages[0].Content = "成功后明确的下一次修复"
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "unit:attempt:1", call); err == nil || calls != 1 {
		t.Fatalf("changed request reused same attempt: %d %v", calls, err)
	}
	if _, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "unit:attempt:2", call); err != nil || calls != 2 {
		t.Fatalf("explicit follow-up rejected: %d %v", calls, err)
	}
}

func TestLiveCostModelCancellationBeforeTransportDoesNotKeepReservation(t *testing.T) {
	for _, afterReserve := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_reserve", true: "after_reserve"}[afterReserve], func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if afterReserve {
				store.afterReserve = cancel
			} else {
				cancel()
			}
			calls := 0
			_, err := s.CompleteLivePaidModel(ctx, model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29}, agentgateway.Request{Stage: "answer", Model: "m"}, 5, "cancel", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
				calls++
				return agentgateway.Response{}, nil
			})
			if !errors.Is(err, context.Canceled) || calls != 0 {
				t.Fatalf("cancelled supplier invoked: %v calls=%d", err, calls)
			}
			if afterReserve {
				if len(store.receipts) != 1 || store.receipts[0].Status != livecost.ReceiptNotSent {
					t.Fatalf("definitely uncalled attempt retained: %+v", store.receipts)
				}
				for _, entry := range store.entries {
					if entry.Status != livecost.StatusReleased || entry.ReservedMicros != 0 {
						t.Fatalf("reservation not released: %+v", entry)
					}
				}
			} else if len(store.requests) != 0 {
				t.Fatal("pre-cancelled call reserved money")
			}
		})
	}
}
