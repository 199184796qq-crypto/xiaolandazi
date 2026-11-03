package db

import (
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

func TestInteractionUnknownNeverConsumesMainlineButRewriteDoes(t *testing.T) {
	store, _ := newCostSQLStore(t)
	b, _ := livecost.RoomBudgetForPool("standard")
	now := time.Now().UTC()
	r := sqlCostRequest("historical-interaction")
	r.BudgetPool, r.BudgetClass, r.EstimatedMicros = b.Pool, livecost.BudgetInteraction, 4_000_000
	if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now.Add(-2*time.Hour)); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	scope := livecost.Scope{TenantID: 14, RoomID: 29}
	if err := store.FinishLiveCost(t.Context(), scope, r.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown}, now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	r.Key, r.Kind, r.BudgetClass, r.EstimatedMicros = "rewrite", livecost.KindModel, livecost.BudgetRewrite, 400_000
	if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	r.Key, r.Kind, r.BudgetClass, r.EstimatedMicros = "mainline", livecost.KindTTS, livecost.BudgetMainline, 900_000
	if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	for _, class := range []string{livecost.BudgetRewrite, livecost.BudgetMainline} {
		r.Key, r.BudgetClass, r.EstimatedMicros = "over-"+class, class, 1
		if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now); err != nil || !out.Allowed {
			t.Fatal("combined mainline allowance escaped", out, err)
		}
	}
	r.Key, r.BudgetClass, r.EstimatedMicros = "another-interaction", livecost.BudgetInteraction, 4_000_000
	if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now); err != nil || !out.Allowed {
		t.Fatal("interaction capped", out, err)
	}
	if out, err := store.ReserveLiveCost(t.Context(), r, b.Policy(), now); err != nil || out.Allowed || !out.Existing {
		t.Fatal("same interaction purchased twice", out, err)
	}
	summary, err := store.LiveCostSummary(t.Context(), scope, b.Policy(), now)
	if err != nil || summary.InteractionMicros != 8_000_000 || summary.UnknownMicros != 4_000_000 || summary.AgedUnresolvedMicros != 4_000_000 {
		t.Fatal("history lost", summary, err)
	}
	if used, err := summary.MainlineBudgetMicros(); err != nil || used != 1_300_002 {
		t.Fatal(used, err)
	}
}
