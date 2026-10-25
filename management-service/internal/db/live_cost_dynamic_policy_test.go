package db

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

func TestConfiguredZeroOptionalBudgetDoesNotRestoreLegacyDatabaseDefault(t *testing.T) {
	store, _ := newCostSQLStore(t)
	policy := livecost.Policy{Configured: true, Window: time.Hour, TotalTargetMicros: 500_000}
	request := sqlCostRequest("zero-optional")
	request.EstimatedMicros = 1
	now := time.Now().UTC()
	result, err := store.ReserveLiveCost(context.Background(), request, policy, now)
	if err != nil || result.Allowed || result.Status != livecost.StatusDeclined {
		t.Fatal("configured optional zero restored the legacy 1.2-yuan allowance", result, err)
	}
	request.Key, request.Priority, request.Reason, request.EstimatedMicros = "necessary-over-lowered-budget", livecost.PriorityNecessary, "必须补充当前事实声音", 600_000
	if result, err = store.ReserveLiveCost(context.Background(), request, policy, now); err != nil || !result.Allowed || !result.OverBudget {
		t.Fatal("lowered configured budget interrupted necessary supply", result, err)
	}
}

func TestRepricingUnknownRequestKeepsOriginalReservationAndSnapshot(t *testing.T) {
	store, state := newCostSQLStore(t)
	ctx, now := context.Background(), time.Now().UTC()
	request := sqlCostRequest("reprice-unknown")
	request.EstimatedMicros = 400_000
	oldSettings := livecost.DefaultSettings()
	oldSettings.Revision = 1
	oldPrice := oldSettings.Snapshot(request.Provider, request.Model)
	request.PricingSnapshot = &oldPrice
	if result, err := store.ReserveLiveCost(ctx, request, livecost.DefaultPolicy(), now); err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	if err := store.FinishLiveCost(ctx, livecost.Scope{TenantID: 14, RoomID: 29}, request.Key,
		livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown}, now); err != nil {
		t.Fatal(err)
	}
	old := state.rows[costSQLKey(int64(14), int64(29), request.Key)]
	repriced := request
	newSettings := livecost.DefaultSettings()
	newSettings.Revision, newSettings.TTSPriceMicrosPer10K = 2, 100_000
	newPrice := newSettings.Snapshot(request.Provider, request.Model)
	repriced.EstimatedMicros, repriced.PricingSnapshot, repriced.Reason = 50_000, &newPrice, "后台降价后的新预估"
	zero := livecost.Policy{Configured: true, Window: time.Hour, TotalTargetMicros: 1}
	result, err := store.ReserveLiveCost(ctx, repriced, zero, now.Add(2*time.Hour))
	if err != nil || !result.Existing || result.Allowed || result.Status != livecost.StatusUnknown || result.EstimatedMicros != request.EstimatedMicros {
		t.Fatal("edited price reauthorized an unknown request or returned a new reservation amount", result, err)
	}
	if len(state.rows) != 1 || !reflect.DeepEqual(state.rows[costSQLKey(int64(14), int64(29), request.Key)], old) {
		t.Fatal("price edit rewrote historical request JSON or unknown reservation", state.rows, old)
	}
	repriced.Fingerprint = "changed-paid-content"
	if _, err := store.ReserveLiveCost(ctx, repriced, zero, now); !errors.Is(err, livecost.ErrRequestConflict) {
		t.Fatal("same-key reprice compatibility allowed different supplier work", err)
	}
}
