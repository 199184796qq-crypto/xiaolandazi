package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
				want := int64(400) // Two Han plus punctuation: five weighted billing characters.
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
	if provider.calls.Load() != 0 || len(store.requests) != 1 || store.requests[0].EstimatedMicros != 500000 {
		t.Fatalf("quote missed expensive contract: calls=%d requests=%+v", provider.calls.Load(), store.requests)
	}
}

func TestLiveCostTTSSuccessIsChargedBeforeDownstreamUseAndSupplierUnknownUnitDoesNotReprice(t *testing.T) {
	for _, contract := range []bool{false, true} {
		t.Run(map[bool]string{false: "reference", true: "contract_without_usage"}[contract], func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			if contract {
				s.liveCostPrices = []liveCostPriceRule{{Provider: "adapter_tts", Model: "m", Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 1000}}
			}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid", ProviderRequestID: "success-not-adopted"}}
			g := ttsgateway.New(provider.Name(), "m")
			g.Register(provider)
			ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "unadopted", Reason: "真实生成声音"})
			// Deliberately discard the media response: no download, archive,
			// selection, Core append or playback can be required for settlement.
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好，A1！"}); err != nil {
				t.Fatal(err)
			}
			if provider.calls.Load() != 1 || len(store.entries) != 1 || len(store.requests) != 1 || len(store.receipts) != 1 {
				t.Fatal(provider.calls.Load(), store.entries)
			}
			for _, entry := range store.entries {
				if entry.Status != livecost.StatusSettled || entry.ChargedMicros != 640 || entry.ReservedMicros != 0 || entry.Receipt.ActualKnown || entry.Request.Characters != 6 || entry.Request.BillableCharacters != 8 || entry.Receipt.SentTextCharacters != 6 || entry.Receipt.BillableCharacters != 8 || entry.Receipt.CharacterRule != livecost.ReferenceTTSCharacterRule || entry.Receipt.Characters != 0 {
					t.Fatalf("unadopted success omitted/repriced or mixed units: %+v", entry)
				}
				if contract && entry.Request.EstimatedMicros != 8000 {
					t.Fatal("expensive preflight quote lost", entry.Request)
				}
			}
		})
	}
}

func TestLiveCostModelReturnedIdentityAndCachedUsagePriceSuccessfulUnadoptedResponse(t *testing.T) {
	t.Setenv("LIVE_AGENT_MODEL", "fallback-profile")
	t.Setenv("AGENT_PROVIDER", "unresolved-profile")
	for _, tc := range []struct {
		input, cached, output, want int64
	}{
		{32000, 31999, 100, 1361},
		{32001, 32000, 100, 4081},
		{256000, 255999, 100, 30961},
		{256001, 256000, 100, 61922},
	} {
		t.Run(fmt.Sprint(tc.input), func(t *testing.T) {
			store := &adapterCostStore{}
			s := adapterTestServer(t, store)
			// A contract for the guessed request profile cannot be applied to
			// a different resolved provider/model in the real gateway response.
			s.liveCostPrices = []liveCostPriceRule{{Provider: "unresolved-profile", Model: "fallback-profile", Kind: livecost.KindModel, Unit: "tokens", InputMicrosPerMillion: 900000000, CachedInputMicrosPerMillion: 900000000, OutputMicrosPerMillion: 900000000}}
			session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
			request := agentgateway.Request{Stage: "answer", Messages: []agentgateway.Message{{Role: "user", Content: "实际模型由配置选定"}}}
			_, err := s.CompleteLivePaidModel(t.Context(), session, request, 5, "discarded-model-response", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
				// Empty/unadopted text still incurred a successful model request.
				return agentgateway.Response{Provider: "qwen", Model: "qwen3.7-flash", InputTokens: tc.input, CachedInputTokens: tc.cached, OutputTokens: tc.output}, nil
			})
			if err != nil || len(store.receipts) != 1 {
				t.Fatal(err, store.receipts)
			}
			r := store.receipts[0]
			if r.ActualKnown || r.Model != "qwen3.7-flash" || r.Provider != "qwen" || r.CachedInputTokens != tc.cached || r.EstimatedMicros == nil || *r.EstimatedMicros != tc.want || r.UsageSource != livecost.SourceSentTextEstimate {
				t.Fatalf("actual response identity/cache did not set full-input pricing tier: %+v want=%d", r, tc.want)
			}
			for _, entry := range store.entries {
				if entry.Status != livecost.StatusSettled || entry.ChargedMicros != tc.want || entry.ReservedMicros != 0 {
					t.Fatal("successful but unadopted model work was not charged", entry)
				}
			}
		})
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

func TestLiveCostTokenTTSUsesSharedEstimateInsteadOfDisablingAllUpdates(t *testing.T) {
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
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err != nil || provider.calls.Load() != 1 {
				t.Fatalf("optional token update permanently blocked: %v calls=%d", err, provider.calls.Load())
			}
			ctx = s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "token-necessary", Priority: livecost.PriorityNecessary, Reason: "必要声音供料"})
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err != nil || provider.calls.Load() != 2 {
				t.Fatalf("necessary token TTS blocked: %v calls=%d", err, provider.calls.Load())
			}
			if len(store.requests) != 2 || !strings.Contains(store.requests[0].Reason, "Token计费") || store.receipts[0].ActualKnown || store.requests[0].Priority != livecost.PriorityOptional {
				t.Fatalf("unknown token price not recorded honestly: requests=%+v receipts=%+v", store.requests, store.receipts)
			}
			store.deny = true
			ctx = s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "token-over-budget", Priority: livecost.PriorityOptional})
			if _, err := g.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "v", Text: "你好。"}); err == nil || provider.calls.Load() != 2 {
				t.Fatalf("token estimate bypassed shared optional budget: %v calls=%d", err, provider.calls.Load())
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
	if r.ActualKnown || r.Characters != 0 || r.SentTextCharacters != 3 || r.BillableCharacters != 5 || r.CharacterRule != ttsgateway.BillableCharacterRule || r.EstimatedMicros == nil || *r.EstimatedMicros != 400 {
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

func TestLiveCostCallEventsReceiptAmountPrecedenceAndFailureFlags(t *testing.T) {
	estimate, zero := int64(450), int64(0)
	for _, tc := range []struct {
		name        string
		receipt     livecost.Receipt
		fallback    int64
		persistErr  error
		charged     int64
		uncertain   bool
		unavailable bool
	}{
		{"successful_actual_usage", livecost.Receipt{Status: livecost.ReceiptSuccess, ActualKnown: true, ActualMicros: 120}, 900, nil, 120, false, false},
		{"successful_returned_estimate", livecost.Receipt{Status: livecost.ReceiptSuccess, EstimatedMicros: &estimate}, 900, nil, 450, false, false},
		{"successful_without_usage_keeps_preflight_quote", livecost.Receipt{Status: livecost.ReceiptSuccess}, 900, nil, 900, false, false},
		{"explicit_zero_actual_is_not_a_missing_usage", livecost.Receipt{Status: livecost.ReceiptSuccess, ActualKnown: true, ActualMicros: 0}, 900, nil, 0, false, false},
		{"explicit_zero_estimate_is_not_a_missing_usage", livecost.Receipt{Status: livecost.ReceiptSuccess, EstimatedMicros: &zero}, 900, nil, 0, false, false},
		{"unknown_is_reserved_not_a_fake_charge", livecost.Receipt{Status: livecost.ReceiptUnknown}, 900, nil, 0, true, false},
		{"known_not_sent_has_no_charge", livecost.Receipt{Status: livecost.ReceiptNotSent}, 900, nil, 0, false, false},
		{"success_persistence_failure_still_spent_money", livecost.Receipt{Status: livecost.ReceiptSuccess, EstimatedMicros: &estimate}, 900, errors.New("ledger unavailable"), 450, false, true},
		{"unknown_persistence_failure_stops_optional_work", livecost.Receipt{Status: livecost.ReceiptUnknown}, 900, errors.New("ledger unavailable"), 0, true, true},
		{"unsent_persistence_failure_marks_accounting_only", livecost.Receipt{Status: livecost.ReceiptNotSent}, 900, errors.New("ledger unavailable"), 0, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := &liveCostCallEvents{}
			events.ChargedMicros.Store(37)
			events.observeReceipt(tc.receipt, tc.fallback, tc.persistErr)
			if got := events.ChargedMicros.Load(); got != 37+tc.charged || events.Uncertain.Load() != tc.uncertain || events.AccountingUnavailable.Load() != tc.unavailable || events.OptionalDeclined.Load() {
				t.Fatalf("receipt flags/charge: charged=%d uncertain=%t unavailable=%t declined=%t", got, events.Uncertain.Load(), events.AccountingUnavailable.Load(), events.OptionalDeclined.Load())
			}
		})
	}
	var absent *liveCostCallEvents
	absent.observeReceipt(livecost.Receipt{Status: livecost.ReceiptSuccess}, 900, errors.New("not tracked"))
}

func TestLiveCostCallEventsTTSReturnedButUnusedSuccessCountsWithPendingAccounting(t *testing.T) {
	for _, failFinish := range []bool{false, true} {
		t.Run(fmt.Sprintf("persist_failure_%t", failFinish), func(t *testing.T) {
			store := &adapterCostStore{failFinish: failFinish}
			s := adapterTestServer(t, store)
			events := &liveCostCallEvents{}
			provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid", ProviderRequestID: "not-adopted"}}
			gateway := ttsgateway.New(provider.Name(), "m")
			gateway.Register(provider)
			ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "events-unused-tts", Priority: livecost.PriorityOptional, Events: events})
			// No downstream download/archive/adoption/playback is invoked.
			out, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "voice", Text: "你好，A1！"})
			if err != nil || out.AudioURL == "" || provider.calls.Load() != 1 || out.AccountingPending != failFinish {
				t.Fatalf("paid success was thrown away: out=%+v err=%v calls=%d", out, err, provider.calls.Load())
			}
			if events.ChargedMicros.Load() != 640 || events.Uncertain.Load() || events.AccountingUnavailable.Load() != failFinish {
				t.Fatalf("local round omitted unadopted TTS: charged=%d uncertain=%t unavailable=%t", events.ChargedMicros.Load(), events.Uncertain.Load(), events.AccountingUnavailable.Load())
			}
		})
	}
}

func TestLiveCostCallEventsModelSuccessChargesReturnedOrMissingUsageBeforeAdoption(t *testing.T) {
	for _, usage := range []bool{false, true} {
		for _, failFinish := range []bool{false, true} {
			t.Run(fmt.Sprintf("usage_%t_persist_failure_%t", usage, failFinish), func(t *testing.T) {
				store := &adapterCostStore{failFinish: failFinish}
				s := adapterTestServer(t, store)
				events := &liveCostCallEvents{}
				ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "events-unused-model", Priority: livecost.PriorityOptional, Events: events})
				request := agentgateway.Request{Stage: "mainline_text_rewrite", Provider: "qwen", Model: "m", MaxTokens: 800, Messages: []agentgateway.Message{{Role: "user", Content: "句"}}}
				if err := s.beginLiveModelCost(ctx, 1, request.Stage, "", request); err != nil {
					t.Fatal(err)
				}
				response := agentgateway.Response{Provider: "qwen", Model: "m"}
				want := store.requests[0].EstimatedMicros
				if usage {
					response.InputTokens, response.OutputTokens = 10, 5
					want = 6 // 10*0.2 + 5*0.8 micro-CNY at the reference rates.
				}
				// Empty returned text remains a successfully paid model response.
				s.finishLiveModelCost(ctx, 1, response, response.InputTokens, response.OutputTokens, nil)
				if want <= 0 || events.ChargedMicros.Load() != want || events.Uncertain.Load() || events.AccountingUnavailable.Load() != failFinish {
					t.Fatalf("model response omitted from local round: want=%d charged=%d uncertain=%t unavailable=%t", want, events.ChargedMicros.Load(), events.Uncertain.Load(), events.AccountingUnavailable.Load())
				}
			})
		}
	}
}

func TestLiveCostCallEventsUnknownKeepsMoneyUnconfirmedAndSameCandidateFenced(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	events := &liveCostCallEvents{}
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "events-unknown-candidate", Priority: livecost.PriorityOptional, Events: events})
	request := agentgateway.Request{Stage: "mainline_text_rewrite", Provider: "qwen", Model: "m", MaxTokens: 800, Messages: []agentgateway.Message{{Role: "user", Content: "同一源句"}}}
	if err := s.beginLiveModelCost(ctx, 1, request.Stage, "", request); err != nil {
		t.Fatal(err)
	}
	s.finishLiveModelCost(ctx, 1, agentgateway.Response{}, 0, 0, errors.New("response unknown"))
	if events.ChargedMicros.Load() != 0 || !events.Uncertain.Load() || events.AccountingUnavailable.Load() {
		t.Fatal("unknown request was described as a paid success or lost its stop flag")
	}
	for _, entry := range store.entries {
		if entry.Status != livecost.StatusUnknown || entry.ReservedMicros <= 0 || entry.ChargedMicros != 0 {
			t.Fatalf("unknown request was refunded or falsely settled: %+v", entry)
		}
	}
	if err := s.beginLiveModelCost(ctx, 2, request.Stage, "", request); !errors.Is(err, errLiveCostRequestRecorded) || len(store.requests) != 1 {
		t.Fatalf("unknown candidate became sendable again: err=%v requests=%d", err, len(store.requests))
	}
}

func TestLiveCostCallEventsDefinitelyUnsentModelFailureAddsNoFee(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	events := &liveCostCallEvents{}
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "events-unsent-candidate", Priority: livecost.PriorityOptional, Events: events})
	request := agentgateway.Request{Stage: "mainline_text_rewrite", Provider: "qwen", Model: "m", MaxTokens: 800}
	if err := s.beginLiveModelCost(ctx, 1, request.Stage, "", request); err != nil {
		t.Fatal(err)
	}
	s.finishLiveModelCost(ctx, 1, agentgateway.Response{}, 0, 0, errLiveCostModelNotSent)
	if events.ChargedMicros.Load() != 0 || events.Uncertain.Load() || events.AccountingUnavailable.Load() {
		t.Fatal("definitely unsent work charged or marked uncertain")
	}
	for _, entry := range store.entries {
		if entry.Status != livecost.StatusReleased || entry.ReservedMicros != 0 || entry.ChargedMicros != 0 {
			t.Fatalf("unsent reservation remained charged: %+v", entry)
		}
	}
}

func TestLiveCostCallEventsReserveDatabaseFailureAfterSuccessfulSummaryStopsBeforeSupplier(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	// The optimistic scheduling read is valid. The database can still become
	// unavailable before the authoritative, transactional request reservation.
	if summary, err := s.liveCostController().Summary(t.Context(), livecost.Scope{TenantID: 14, RoomID: 29}); err != nil || summary.TotalMicros != 0 {
		t.Fatal("fixture summary was not available", summary, err)
	}
	store.failReserve = true
	events := &liveCostCallEvents{}
	provider := &adapterTTSProvider{result: ttsgateway.SynthesizeResponse{AudioURL: "https://audio.invalid"}}
	gateway := ttsgateway.New(provider.Name(), "m")
	gateway.Register(provider)
	ctx := s.withLiveCostTTS(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "reserve-db-failed", Priority: livecost.PriorityOptional, Events: events})
	if _, err := gateway.SynthesizeURL(ctx, ttsgateway.SynthesizeRequest{VoiceID: "voice", Text: "你好。"}); !errors.Is(err, errLiveCostOptionalDeclined) {
		t.Fatalf("optional work did not fail closed before sending: %v", err)
	}
	if provider.calls.Load() != 0 || events.ChargedMicros.Load() != 0 || !events.OptionalDeclined.Load() || !events.AccountingUnavailable.Load() || events.Uncertain.Load() {
		t.Fatalf("database outage misclassified or supplier called: calls=%d charged=%d declined=%t unavailable=%t uncertain=%t", provider.calls.Load(), events.ChargedMicros.Load(), events.OptionalDeclined.Load(), events.AccountingUnavailable.Load(), events.Uncertain.Load())
	}
	if len(store.entries) != 0 || len(store.receipts) != 0 {
		t.Fatal("unsent optional work invented a reservation/receipt", store.entries, store.receipts)
	}
	schedule := &recordingRefreshSchedule{Budget: recordingRefreshBudget{RoundLimitMicros: 10_000}, Pending: map[recordingRefreshPosition]int64{{Tab: 0, Index: 0}: 1_000}}
	schedule.complete(0, 0, events)
	if !schedule.Paused || schedule.PauseReason != "更新请求记账暂不可用" || schedule.SpentMicros != 0 {
		t.Fatalf("outage was reported as exhausted budget: %+v", schedule)
	}
}
