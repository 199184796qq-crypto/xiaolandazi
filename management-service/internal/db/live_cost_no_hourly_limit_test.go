package db

import (
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
)

func TestAccountingOnlyReopensUnsentLegacyBudgetDeclineAndKeepsPurchaseFence(t *testing.T) {
	s, _ := newCostSQLStore(t)
	r := sqlCostRequest("old-optional-decline")
	r.Priority, r.EstimatedMicros = livecost.PriorityOptional, 20_000_000
	old := livecost.Policy{Configured: true, Window: time.Hour, TotalTargetMicros: 1}
	now := time.Now().UTC()
	if out, err := s.ReserveLiveCost(t.Context(), r, old, now); err != nil || out.Allowed {
		t.Fatal(out, err)
	}
	policy := livecost.DefaultSettings().Policy()
	if out, err := s.ReserveLiveCost(t.Context(), r, policy, now); err != nil || !out.Allowed || out.Existing || out.OverBudget {
		t.Fatal("old decline still blocked", out, err)
	}
	if out, err := s.ReserveLiveCost(t.Context(), r, policy, now); err != nil || out.Allowed || !out.Existing {
		t.Fatal("paid request sent twice", out, err)
	}
	if err := s.FinishLiveCost(t.Context(), livecost.Scope{TenantID: 14, RoomID: 29}, r.Key, livecost.Receipt{TenantID: 14, RoomID: 29, Status: livecost.ReceiptUnknown}, now); err != nil {
		t.Fatal(err)
	}
	if out, err := s.ReserveLiveCost(t.Context(), r, policy, now.Add(2*time.Hour)); err != nil || out.Allowed || !out.Existing {
		t.Fatal("unknown purchase fence lost", out, err)
	}
	r.Key = "new-valid-request"
	if out, err := s.ReserveLiveCost(t.Context(), r, policy, now.Add(2*time.Hour)); err != nil || !out.Allowed {
		t.Fatal("history blocked new request", out, err)
	}
}
