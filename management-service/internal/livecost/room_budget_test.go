package livecost

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoomBudgetCategoriesProtectInteractionAndSafety(t *testing.T) {
	for _, pool := range []string{"standard", "high_fidelity"} {
		b, err := RoomBudgetForPool(pool)
		if err != nil {
			t.Fatal(err)
		}
		s := Summary{TotalMicros: b.Mainline, MainlineMicros: b.Mainline}
		if b.Allows(Request{BudgetClass: BudgetMainline, EstimatedMicros: 1}, s) {
			t.Fatal("mainline borrowed interaction reserve")
		}
		if !b.Allows(Request{BudgetClass: BudgetInteraction, EstimatedMicros: b.Interaction}, s) {
			t.Fatal("interaction reserve unavailable")
		}
		s.TotalMicros = b.Total - b.Safety
		if b.Allows(Request{BudgetClass: BudgetRewrite, EstimatedMicros: 1}, s) {
			t.Fatal("new generation spent safety buffer")
		}
	}
}

func TestStrictLedgerFailureNeverPermitsNecessaryDegradedSend(t *testing.T) {
	b, _ := RoomBudgetForPool("standard")
	p := b.Policy()
	p.SpoolDir = t.TempDir()
	c := New(nil, p)
	r := costRequest("strict-no-db")
	r.BudgetPool = "standard"
	result, err := c.Begin(t.Context(), r)
	if result.Allowed || !errors.Is(err, ErrAccountingUnavailable) {
		t.Fatal(result, err)
	}
	// An invalid context must not lose its strict flag while normalizing.
	p.TotalTargetMicros++
	if result, err := c.Begin(WithPolicy(context.Background(), p), r); result.Allowed || err == nil {
		t.Fatal(result, err)
	}
}

func TestBudgetClassLegacyRecordsAndLateUnknown(t *testing.T) {
	for _, tc := range []struct {
		r    Request
		want string
	}{
		{Request{Kind: KindTTS, Reason: "话术后台改写"}, BudgetMainline},
		{Request{Kind: KindModel, Reason: "话术后台改写"}, BudgetRewrite},
		{Request{Kind: KindTTS, Attribution: &Attribution{Source: "interaction"}}, BudgetInteraction},
		{Request{Kind: KindModel, Reason: "问题归类"}, BudgetInteraction},
	} {
		if got := BudgetClass(tc.r); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	s := Summary{}
	r := costRequest("aged")
	r.BudgetClass = BudgetMainline
	if err := s.AddBudgetEntry(Entry{Request: r, Status: StatusUnknown, ReservedMicros: 100_000}, true); err != nil {
		t.Fatal(err)
	}
	if s.TotalMicros != 100_000 || s.AgedUnresolvedMicros != 100_000 || s.MainlineMicros != 100_000 {
		t.Fatal(s)
	}
	if err := s.AddBudgetEntry(Entry{Request: r, Status: StatusUnknown}, false); err == nil {
		t.Fatal("unknown price admitted as zero")
	}
	b, _ := RoomBudgetForPool("standard")
	if p, err := NormalizePolicy(b.Policy()); err != nil || p.Window != time.Hour {
		t.Fatal(p, err)
	}
}
