package livecost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"
)

type contextPolicyStore struct {
	mu       sync.Mutex
	reserves []Policy
	reads    []Policy
}

func (store *contextPolicyStore) ReserveLiveCost(_ context.Context, request Request, policy Policy, _ time.Time) (Reservation, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reserves = append(store.reserves, policy)
	allowed := request.Priority == PriorityNecessary || (request.EstimatedMicros <= policy.OptionalLimitMicros && request.EstimatedMicros <= policy.TotalTargetMicros)
	return Reservation{Key: request.Key, Allowed: allowed, EstimatedMicros: request.EstimatedMicros}, nil
}

func (*contextPolicyStore) FinishLiveCost(context.Context, Scope, string, Receipt, time.Time) error {
	return nil
}

func (store *contextPolicyStore) LiveCostSummary(_ context.Context, _ Scope, policy Policy, _ time.Time) (Summary, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.reads = append(store.reads, policy)
	return Summary{}, nil
}

func TestConfiguredPolicyPreservesZeroOptionalWhileLegacyUsesDefaults(t *testing.T) {
	legacy, err := NormalizePolicy(Policy{})
	if err != nil || legacy != DefaultPolicy() {
		t.Fatal("legacy zero policy lost compatible defaults", legacy, err)
	}
	zero := Policy{Configured: true, Window: time.Hour, TotalTargetMicros: 500_000}
	if got, err := NormalizePolicy(zero); err != nil || got != zero {
		t.Fatal("configured zero optional budget silently restored paid optional work", got, err)
	}
	if got := New(nil, zero).policy; got != zero {
		t.Fatal("constructor changed explicitly disabled optional budgets", got)
	}
}

func TestControllerContextPolicyIsPerCallAndKeepsDurableSpoolPath(t *testing.T) {
	store := &contextPolicyStore{}
	original := DefaultPolicy()
	original.SpoolDir = t.TempDir()
	controller := New(store, original)
	configured := Policy{Configured: true, Window: time.Hour, MainlineTargetMicros: 0, OptionalLimitMicros: 0, TotalTargetMicros: 500_000, SpoolDir: t.TempDir()}
	ctx := WithPolicy(t.Context(), configured)
	request := costRequest("disabled-optional")
	request.Priority, request.EstimatedMicros = PriorityOptional, 1
	reservation, err := controller.Begin(ctx, request)
	if err != nil || reservation.Allowed {
		t.Fatal("explicit zero budget authorized optional work", reservation, err)
	}
	if _, err := controller.Summary(ctx, Scope{TenantID: 14, RoomID: 29}); err != nil {
		t.Fatal(err)
	}
	request.Key = "default-after-override"
	if reservation, err := controller.Begin(t.Context(), request); err != nil || !reservation.Allowed {
		t.Fatal("context update leaked into another request", reservation, err)
	}
	configured.SpoolDir = original.SpoolDir
	if len(store.reserves) != 2 || len(store.reads) != 1 || store.reserves[0] != configured || store.reads[0] != configured ||
		store.reserves[1] != original || controller.policy != original {
		t.Fatal("per-call budget rewrote shared state or took a caller-controlled spool path", store, controller.policy)
	}
}

func TestControllerConcurrentContextPoliciesDoNotReplaceOneAnother(t *testing.T) {
	store := &contextPolicyStore{}
	controller := New(store, DefaultPolicy())
	var group sync.WaitGroup
	for index := 0; index < 32; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			policy := Policy{Configured: true, Window: time.Hour, MainlineTargetMicros: int64(index), OptionalLimitMicros: int64(index + 1), TotalTargetMicros: int64(index + 2)}
			request := costRequest(fmt.Sprintf("parallel-%d", index))
			request.Priority, request.EstimatedMicros = PriorityOptional, int64(index+1)
			reservation, err := controller.Begin(WithPolicy(t.Context(), policy), request)
			if err != nil || !reservation.Allowed {
				t.Errorf("request %d inherited another caller's smaller limit: %+v %v", index, reservation, err)
			}
		}(index)
	}
	group.Wait()
	if len(store.reserves) != 32 || controller.policy != DefaultPolicy() {
		t.Fatal("a context policy mutated the reusable controller", len(store.reserves), controller.policy)
	}
}

func TestInvalidContextPolicyStopsOptionalButKeepsNecessaryDurableAccounting(t *testing.T) {
	for _, invalid := range []Policy{
		{Configured: true, Window: time.Hour, MainlineTargetMicros: -1, OptionalLimitMicros: 10, TotalTargetMicros: 20},
		{Configured: true, Window: time.Hour, MainlineTargetMicros: 11, OptionalLimitMicros: 10, TotalTargetMicros: 20},
		{Configured: true, Window: time.Hour, OptionalLimitMicros: 21, TotalTargetMicros: 20},
		{Configured: true, Window: time.Hour, TotalTargetMicros: 0},
		{Configured: true, Window: 0, OptionalLimitMicros: 10, TotalTargetMicros: 20},
	} {
		store := &contextPolicyStore{}
		base := DefaultPolicy()
		base.SpoolDir = t.TempDir()
		controller := New(store, base)
		ctx := WithPolicy(t.Context(), invalid)
		request := costRequest("invalid-optional")
		request.Priority, request.EstimatedMicros = PriorityOptional, 100
		reservation, err := controller.Begin(ctx, request)
		if !errors.Is(err, ErrAccountingUnavailable) || reservation.Allowed || !reservation.AccountingUnavailable || len(store.reserves) != 0 {
			t.Fatal("invalid context fell back to free/unbounded optional admission", invalid, reservation, err, store)
		}
		if _, err := controller.Summary(ctx, Scope{TenantID: 14, RoomID: 29}); !errors.Is(err, ErrAccountingUnavailable) || len(store.reads) != 0 {
			t.Fatal("invalid policy was presented as a valid zero-spend summary", invalid, err, store)
		}
		request.Key, request.Priority = "necessary-with-invalid-settings", PriorityNecessary
		reservation, err = controller.Begin(ctx, request)
		if !errors.Is(err, ErrAccountingUnavailable) || !reservation.Allowed || !reservation.AccountingUnavailable || !reservation.DegradedRecorded || len(store.reserves) != 0 {
			t.Fatal("bad optional settings interrupted necessary supply or lost its durable claim", invalid, reservation, err)
		}
		entries, err := os.ReadDir(base.SpoolDir)
		if err != nil || len(entries) != 1 {
			t.Fatal("necessary degraded accounting was not written to the original spool", entries, err)
		}
		if again, err := controller.Begin(ctx, request); err != nil || !again.Existing || again.Allowed {
			t.Fatal("bad settings let the same necessary request bypass its durable fence", again, err)
		}
	}
}

func TestRequestPricingSnapshotCopiesAndDoesNotChangeSupplierWorkIdentity(t *testing.T) {
	pricing := DefaultSettings().Snapshot("qwen", "qwen3.7-flash")
	pricing.Contract = &SupplierPrice{Provider: "tts", Model: "m", Kind: KindTTS, Unit: "characters", CharacterMicros: 80}
	request := costRequest("price-snapshot")
	request.EstimatedMicros, request.PricingSnapshot = 3200, &pricing
	normalized, err := NormalizeRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	pricing.ModelTiers[0].InputMicrosPerMillion = 999
	pricing.Contract.CharacterMicros = 999
	pricing.Config.DefaultModelRates[0].InputMicrosPerMillion = 999
	pricing.Config.ModelPrices[0].Tiers[0].InputMicrosPerMillion = 999
	if normalized.PricingSnapshot.ModelTiers[0].InputMicrosPerMillion != 200_000 || normalized.PricingSnapshot.Contract.CharacterMicros != 80 ||
		normalized.PricingSnapshot.Config.DefaultModelRates[0].InputMicrosPerMillion != 200_000 || normalized.PricingSnapshot.Config.ModelPrices[0].Tiers[0].InputMicrosPerMillion != 200_000 {
		t.Fatal("caller mutation changed already-normalized request pricing", normalized.PricingSnapshot)
	}
	repriced := normalized
	nextSettings := DefaultSettings()
	nextSettings.Revision = 9
	next := nextSettings.Snapshot("qwen", "qwen3.7-flash")
	repriced.PricingSnapshot, repriced.EstimatedMicros, repriced.Reason = &next, 9000, "新版参考价审计说明"
	if !SameRequestIdentity(normalized, repriced) {
		t.Fatal("a price edit changed the logical identity of already-sent work")
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.Fingerprint = "other-content" },
		func(r *Request) { r.Provider = "other-provider" },
		func(r *Request) { r.Model = "other-model" },
		func(r *Request) { r.Priority = PriorityOptional },
		func(r *Request) { r.SessionID++ },
		func(r *Request) { r.RoomID++ },
		func(r *Request) { r.BillableCharacters++ },
		func(r *Request) { r.Attribution = &Attribution{Source: "interaction"} },
	} {
		changed := repriced
		change(&changed)
		if SameRequestIdentity(normalized, changed) {
			t.Fatal("price compatibility weakened actual work isolation", reflect.ValueOf(change).Pointer(), changed)
		}
	}
}

func TestNormalizeRequestValidatesPriceSnapshotAndKeepsLegacyRequests(t *testing.T) {
	for _, change := range []func(*PricingSnapshot){
		func(snapshot *PricingSnapshot) { snapshot.Revision++ },
		func(snapshot *PricingSnapshot) { snapshot.TTSPriceMicrosPer10K = 0 },
		func(snapshot *PricingSnapshot) { snapshot.ModelTiers = nil },
		func(snapshot *PricingSnapshot) { snapshot.Config.OptionalHourlyMicros = -1 },
	} {
		request := costRequest("invalid-price-snapshot")
		pricing := DefaultSettings().Snapshot(request.Provider, request.Model)
		change(&pricing)
		request.PricingSnapshot = &pricing
		if _, err := NormalizeRequest(request); err == nil {
			t.Fatal("an invalid price snapshot passed request normalization", pricing)
		}
	}
	legacy := costRequest("legacy-no-price-snapshot")
	if normalized, err := NormalizeRequest(legacy); err != nil || normalized.PricingSnapshot != nil {
		t.Fatal("legacy request without a price snapshot lost compatibility", normalized, err)
	}
}

func TestNormalizeRequestEstimatesMissingTTSAmountAtFrozenConfiguredPrice(t *testing.T) {
	settings := DefaultSettings()
	settings.Revision, settings.TTSPriceMicrosPer10K = 3, 400_000
	request := costRequest("half-price-fallback-estimate")
	pricing := settings.Snapshot(request.Provider, request.Model)
	request.PricingSnapshot = &pricing
	got, err := NormalizeRequest(request)
	// The existing 40 weighted characters at 0.4 yuan / 10,000 cost
	// 1,600 micro-CNY. The legacy 80-micro rate would reserve twice this.
	if err != nil || got.EstimatedMicros != 1600 {
		t.Fatal("missing estimate ignored the frozen lower configured price", got, err)
	}
	request.EstimatedMicros = 1234
	if got, err = NormalizeRequest(request); err != nil || got.EstimatedMicros != 1234 {
		t.Fatal("normalization rewrote an explicit prior reservation estimate", got, err)
	}
	request.EstimatedMicros, request.PricingSnapshot = 0, nil
	if got, err = NormalizeRequest(request); err != nil || got.EstimatedMicros != 3200 {
		t.Fatal("legacy request without pricing lost the existing fallback", got, err)
	}
}
