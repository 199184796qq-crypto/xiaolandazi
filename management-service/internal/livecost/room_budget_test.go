package livecost

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRoomAccountingAllowsEveryClassWithoutHourlyLimit(t *testing.T) {
	for _, pool := range []string{"standard", "high_fidelity"} {
		b, err := RoomBudgetForPool(pool)
		if err != nil {
			t.Fatal(err)
		}
		if !b.Policy().AccountingOnly {
			t.Fatal("room reactivated hourly budget")
		}
		for _, class := range []string{BudgetMainline, BudgetInteraction, BudgetRewrite} {
			if !b.Allows(Request{BudgetClass: class, EstimatedMicros: 20_000_000}, Summary{TotalMicros: 90_000_000, MainlineMicros: 40_000_000, RewriteMicros: 20_000_000, InteractionMicros: 30_000_000}) {
				t.Fatal(pool, class)
			}
		}
	}
}

func TestMainlineBudgetUsageRejectsNegativeAndOverflow(t *testing.T) {
	for _, s := range []Summary{{MainlineMicros: -1}, {RewriteMicros: -1}, {MainlineMicros: 1<<63 - 1, RewriteMicros: 1}} {
		if _, err := s.MainlineBudgetMicros(); err == nil {
			t.Fatal("invalid mainline sum accepted", s)
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
	p.RoomBudget = &RoomBudget{Pool: "invalid"}
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
