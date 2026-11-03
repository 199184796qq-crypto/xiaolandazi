package db

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

func TestStrictRoomBudgetSerializesNecessaryCallsAndPreservesInteraction(t *testing.T) {
	store, _ := newCostSQLStore(t)
	b, _ := livecost.RoomBudgetForPool("standard")
	p := b.Policy()
	now := time.Now().UTC()
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := sqlCostRequest(fmt.Sprint("strict-", i))
			r.EstimatedMicros = 100_000
			r.BudgetClass = livecost.BudgetMainline
			r.BudgetPool = "standard"
			out, err := store.ReserveLiveCost(context.Background(), r, p, now)
			if err != nil {
				t.Error(err)
			}
			if out.Allowed {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 8 {
		t.Fatal("concurrent supply escaped 0.8-yuan sublimit", accepted.Load())
	}
	r := sqlCostRequest("interaction-reserve")
	r.EstimatedMicros = b.Interaction
	r.BudgetClass = livecost.BudgetInteraction
	r.BudgetPool = b.Pool
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	r.Key = "rewrite-reserve"
	r.Kind = livecost.KindModel
	r.BudgetClass = livecost.BudgetRewrite
	r.EstimatedMicros = b.Rewrite
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	r.Key = "new-session"
	r.SessionID = 999
	r.BudgetClass = livecost.BudgetMainline
	r.EstimatedMicros = 1
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || out.Allowed {
		t.Fatal("restart reset allowance", out, err)
	}
	summary, err := store.LiveCostSummary(t.Context(), livecost.Scope{TenantID: 14, RoomID: 29}, p, now)
	if err != nil || summary.TotalMicros != 1_200_000 || summary.MainlineMicros != 800_000 || summary.InteractionMicros != 350_000 || summary.RewriteMicros != 50_000 {
		t.Fatal(summary, err)
	}
	r.RoomID = 30
	r.Key = "other-room"
	r.EstimatedMicros = 10
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed {
		t.Fatal("another room inherited this room's spend", out, err)
	}
}

func TestStrictBudgetKeepsUnknownAndLateSettlementsAcrossHour(t *testing.T) {
	store, _ := newCostSQLStore(t)
	b, _ := livecost.RoomBudgetForPool("high_fidelity")
	p := b.Policy()
	now := time.Now().UTC()
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	r := sqlCostRequest("late-audio")
	r.EstimatedMicros = b.Mainline
	r.BudgetClass = livecost.BudgetMainline
	r.BudgetPool = b.Pool
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	if err := store.FinishLiveCost(t.Context(), scope, r.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown}, now); err != nil {
		t.Fatal(err)
	}
	future := now.Add(2 * time.Hour)
	r.Key = "after-hour"
	if out, err := store.ReserveLiveCost(t.Context(), r, p, future); err != nil || out.Allowed {
		t.Fatal("unknown reservation aged into free money", out, err)
	}
	amount := int64(1_000_000)
	if err := store.FinishLiveCost(t.Context(), scope, "late-audio", livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptSuccess, EstimatedMicros: &amount}, future); err != nil {
		t.Fatal(err)
	}
	summary, err := store.LiveCostSummary(t.Context(), scope, p, future)
	if err != nil || summary.TotalMicros != amount || summary.ReservedMicros != 0 {
		t.Fatal("late paid result vanished immediately", summary, err)
	}
	summary, err = store.LiveCostSummary(t.Context(), scope, p, future.Add(time.Hour+time.Second))
	if err != nil || summary.TotalMicros != 0 {
		t.Fatal("settled call never expired", summary, err)
	}
}

func TestStrictBudgetDeferralAndKnownUnsentCanRetrySameOperation(t *testing.T) {
	store, state := newCostSQLStore(t)
	b, _ := livecost.RoomBudgetForPool("standard")
	p := b.Policy()
	now := time.Now().UTC()
	r := sqlCostRequest("same-piece")
	r.BudgetClass = livecost.BudgetMainline
	r.BudgetPool = b.Pool
	r.EstimatedMicros = b.Mainline + 1
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || out.Allowed || out.Existing {
		t.Fatal(out, err)
	}
	if len(state.rows) != 0 {
		t.Fatal("budget decline permanently fenced a never-sent piece")
	}
	r.EstimatedMicros = 100_000
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	if err := store.FinishLiveCost(t.Context(), livecost.Scope{TenantID: 14, RoomID: 29}, r.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptNotSent}, now); err != nil {
		t.Fatal(err)
	}
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || !out.Allowed || out.Existing {
		t.Fatal("definitely unsent operation could not resume", out, err)
	}
	if out, err := store.ReserveLiveCost(t.Context(), r, p, now); err != nil || out.Allowed || !out.Existing {
		t.Fatal("reserved operation sent twice", out, err)
	}
}
