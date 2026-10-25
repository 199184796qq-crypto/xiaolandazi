package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func TestConfiguredTTSLowerPriceAndInflightPriceFreeze(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	cfg := livecost.DefaultSettings()
	cfg.Revision, cfg.TTSPriceMicrosPer10K = 1, 400_000
	s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return cfg, nil }
	// An old environment contract must not override the administrator's saved price.
	s.liveCostPrices = []liveCostPriceRule{{Provider: "adapter_tts", Model: "m", Kind: livecost.KindTTS, Unit: "characters", CharacterMicros: 9999}}
	attempt := ttsgateway.SynthesisAttempt{Provider: "adapter_tts", Model: "m", VoiceID: "v", Rate: 1, SentTextCharacters: 3, BillableCharacters: 5, CharacterRule: ttsgateway.BillableCharacterRule, TextSHA: "same-text"}
	first := &liveTTSObserver{s: s, scope: liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "old", Priority: livecost.PriorityNecessary, Reason: "必要声音"}}
	key, err := first.BeforeSynthesis(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.requests[0].EstimatedMicros; got != 200 {
		t.Fatalf("lower configured price ignored: %d", got)
	}
	original, _ := json.Marshal(store.requests[0])
	cfg.Revision, cfg.TTSPriceMicrosPer10K = 2, 1_600_000
	if err := first.AfterSynthesis(t.Context(), ttsgateway.SynthesisReceipt{SynthesisAttempt: attempt, Ticket: key, Outcome: ttsgateway.SynthesisSucceeded}); err != nil {
		t.Fatal(err)
	}
	if got := store.entries[key].ChargedMicros; got != 200 {
		t.Fatalf("inflight repriced: %d", got)
	}
	second := &liveTTSObserver{s: s, scope: liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 501, Key: "new", Priority: livecost.PriorityNecessary, Reason: "必要声音"}}
	key2, err := second.BeforeSynthesis(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.AfterSynthesis(t.Context(), ttsgateway.SynthesisReceipt{SynthesisAttempt: attempt, Ticket: key2, Outcome: ttsgateway.SynthesisSucceeded}); err != nil {
		t.Fatal(err)
	}
	if got := store.entries[key2].ChargedMicros; got != 800 {
		t.Fatalf("new request price not refreshed: %d", got)
	}
	after, _ := json.Marshal(store.entries[key].Request)
	if string(original) != string(after) {
		t.Fatal("old request price snapshot was mutated")
	}
	if store.receipts[0].ActualKnown {
		t.Fatal("configured estimate became supplier invoice")
	}
}

func TestConfiguredModelFreezesEntireLadderBeforeRealUsage(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	cfg := livecost.DefaultSettings()
	cfg.Revision = 1
	for i := range cfg.ModelPrices[0].Tiers {
		cfg.ModelPrices[0].Tiers[i].InputMicrosPerMillion /= 2
		cfg.ModelPrices[0].Tiers[i].CachedInputMicrosPerMillion /= 2
		cfg.ModelPrices[0].Tiers[i].OutputMicrosPerMillion /= 2
	}
	s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return cfg, nil }
	session := model.LiveRuntimeSession{ID: 500, TenantID: 14, RoomID: 29, ExternalID: "run"}
	req := agentgateway.Request{Stage: "answer", Provider: "qwen", Model: "qwen3.7-flash", MaxTokens: 100, Messages: []agentgateway.Message{{Role: "user", Content: "真实输入返回后跨过阶梯"}}}
	oldPrice := cfg.Snapshot(req.Provider, req.Model)
	want, err := s.quoteLiveModel(oldPrice, 32001, 32000, 100)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CompleteLivePaidModel(t.Context(), session, req, 5, "freeze-model", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		cfg = livecost.DefaultSettings()
		cfg.Revision = 2
		for i := range cfg.ModelPrices[0].Tiers {
			cfg.ModelPrices[0].Tiers[i].InputMicrosPerMillion *= 10
			cfg.ModelPrices[0].Tiers[i].CachedInputMicrosPerMillion *= 10
			cfg.ModelPrices[0].Tiers[i].OutputMicrosPerMillion *= 10
		}
		return agentgateway.Response{Text: "成功但未采纳也计费", Provider: req.Provider, Model: req.Model, InputTokens: 32001, CachedInputTokens: 32000, OutputTokens: 100}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.receipts) != 1 || store.receipts[0].EstimatedMicros == nil || *store.receipts[0].EstimatedMicros != want {
		t.Fatalf("actual input tier not priced using frozen ladder: %+v want %d", store.receipts, want)
	}
	if len(store.requests) != 1 || store.requests[0].PricingSnapshot.Revision != 1 {
		t.Fatal("missing persisted price revision", store.requests)
	}
	_, err = s.CompleteLivePaidModel(t.Context(), session, req, 5, "next-model", func(context.Context, agentgateway.Request) (agentgateway.Response, error) {
		return agentgateway.Response{Provider: req.Provider, Model: req.Model, InputTokens: 32001, CachedInputTokens: 32000, OutputTokens: 100}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	newWant, _ := s.quoteLiveModel(cfg.Snapshot(req.Provider, req.Model), 32001, 32000, 100)
	if *store.receipts[1].EstimatedMicros != newWant || newWant <= want {
		t.Fatalf("next request did not use new price: %+v", store.receipts)
	}
}

func TestConfigReadFailureDefersOptionalButKeepsNecessaryPositivePrice(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	cfg := livecost.DefaultSettings()
	cfg.Revision = 3
	cfg.TTSPriceMicrosPer10K = 600_000
	fail := false
	s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) {
		if fail {
			return livecost.Settings{}, errors.New("private database diagnostic")
		}
		return cfg, nil
	}
	if _, ok := s.liveCostSettingsSnapshot(t.Context()); !ok {
		t.Fatal("valid baseline rejected")
	}
	fail = true
	attempt := ttsgateway.SynthesisAttempt{Provider: "adapter_tts", Model: "m", BillableCharacters: 5, SentTextCharacters: 3, CharacterRule: ttsgateway.BillableCharacterRule}
	optional := &liveTTSObserver{s: s, scope: liveCostCallScope{TenantID: 14, RoomID: 29, Key: "optional", Priority: livecost.PriorityOptional}}
	if _, err := optional.BeforeSynthesis(t.Context(), attempt); !errors.Is(err, errLiveCostOptionalDeclined) || len(store.requests) != 0 {
		t.Fatal("bad config let optional supplier call pass", err, store.requests)
	}
	necessary := &liveTTSObserver{s: s, scope: liveCostCallScope{TenantID: 14, RoomID: 29, Key: "necessary", Priority: livecost.PriorityNecessary, Reason: "必要声音"}}
	key, err := necessary.BeforeSynthesis(t.Context(), attempt)
	if err != nil {
		t.Fatal(err)
	}
	if store.requests[0].EstimatedMicros != 300 || store.requests[0].PricingSnapshot.Revision != 3 {
		t.Fatal("last verified price not kept", store.requests)
	}
	if err = necessary.AfterSynthesis(t.Context(), ttsgateway.SynthesisReceipt{SynthesisAttempt: attempt, Ticket: key, Outcome: ttsgateway.SynthesisSucceeded}); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredPriceChangeCannotResendUnknownPaidRequest(t *testing.T) {
	store := &adapterCostStore{}
	s := adapterTestServer(t, store)
	cfg := livecost.DefaultSettings()
	cfg.Revision = 1
	s.liveCostConfigLoader = func(context.Context) (livecost.Settings, error) { return cfg, nil }
	ctx := withLiveCostScope(t.Context(), liveCostCallScope{TenantID: 14, RoomID: 29, SessionID: 500, Key: "one-stable-request", Priority: livecost.PriorityOptional})
	req := agentgateway.Request{Stage: "polish", Provider: "qwen", Model: "qwen3.7-flash", MaxTokens: 100, Messages: []agentgateway.Message{{Role: "user", Content: "same input"}}}
	if err := s.beginLiveModelCost(ctx, 1, "polish", "", req); err != nil {
		t.Fatal(err)
	}
	s.finishLiveModelCost(ctx, 1, agentgateway.Response{}, 0, 0, errors.New("lost response"))
	cfg.Revision = 2
	cfg.ModelPrices[0].Tiers[0].OutputMicrosPerMillion *= 2
	if err := s.beginLiveModelCost(ctx, 2, "polish", "", req); !errors.Is(err, errLiveCostRequestRecorded) {
		t.Fatalf("reprice changed stable request identity: %v", err)
	}
	if len(store.requests) != 1 || store.entries[store.requests[0].Key].Status != livecost.StatusUnknown {
		t.Fatal("unknown was refunded or new request created", store.requests)
	}
}

func TestLiveCostSettingsAuditRecordsRequestedAndCommittedRevision(t *testing.T) {
	if got := auditAction("PUT", "/api/v1/system/live-cost-settings"); got != "system.live_cost.update" {
		t.Fatal(got)
	}
	cfg := livecost.DefaultSettings()
	cfg.Revision = 8
	raw, _ := json.Marshal(cfg)
	var payload map[string]any
	_ = json.Unmarshal(raw, &payload)
	entry := model.AdminAuditLog{Action: "system.live_cost.update"}
	enrichAuditFromPayload(payload, &entry)
	applyAuditSuccessState(&entry)
	committed, err := livecost.DecodeSettings(entry.AfterState)
	if err != nil || committed.Revision != 9 || entry.ObjectName != "直播计费与小时预算" {
		t.Fatal(entry, err)
	}
}
