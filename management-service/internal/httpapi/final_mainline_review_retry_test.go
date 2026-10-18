package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
)

func TestFinalReviewRetriesOnlyAuditAndRetainsBothReceipts(t *testing.T) {
	a := &anchorcycle.FinalContentAudit{}
	calls := 0
	err := runFinalMainlineReview(context.Background(), time.Second, a, func(context.Context) (anchorcycle.FinalReviewAttempt, error) {
		calls++
		raw := "没有给出明确结论"
		if calls == 2 {
			raw = `{"decision":"违规","violations":[{"词":"手慢就没了","原因":"没有库存依据"}]}`
		}
		return anchorcycle.FinalReviewAttempt{ExchangeID: int64(100 + calls), Raw: raw, InputTokens: 12, OutputTokens: 5}, nil
	})
	if err != nil || calls != 2 || a.Decision != "有问题" || !strings.Contains(a.Reason, "手慢就没了") || !strings.Contains(a.Reason, "没有库存依据") || a.Quote != "手慢就没了" {
		t.Fatal("verdict or repair reason lost", a, calls, err)
	}
	if len(a.Attempts) != 2 || a.Attempts[0].ExchangeID != 101 || a.Attempts[0].Error == "" || a.Attempts[0].Raw != "没有给出明确结论" || a.Attempts[1].ExchangeID != 102 || a.Attempts[1].Error != "" || a.InputTokens != 24 || a.OutputTokens != 10 {
		t.Fatal("retry hid original exchange or token usage", a)
	}
}

func TestFinalReviewEachAttemptGetsIndependentDeadline(t *testing.T) {
	a := &anchorcycle.FinalContentAudit{}
	calls := 0
	err := runFinalMainlineReview(context.Background(), 80*time.Millisecond, a, func(ctx context.Context) (anchorcycle.FinalReviewAttempt, error) {
		calls++
		if calls == 1 {
			<-ctx.Done()
			return anchorcycle.FinalReviewAttempt{ExchangeID: 1}, ctx.Err()
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) < 40*time.Millisecond {
			t.Error("first request consumed second deadline")
		}
		return anchorcycle.FinalReviewAttempt{ExchangeID: 2, Raw: "通过。"}, nil
	})
	if err != nil || calls != 2 || a.Decision != "通过" || len(a.Attempts) != 2 || a.Attempts[0].Error == "" {
		t.Fatal(a, calls, err)
	}
}

func TestFinalReviewCancellationStopsRetriesAndCannotApprove(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := &anchorcycle.FinalContentAudit{}
	calls := 0
	err := runFinalMainlineReview(ctx, time.Second, a, func(context.Context) (anchorcycle.FinalReviewAttempt, error) {
		calls++
		cancel()
		return anchorcycle.FinalReviewAttempt{Raw: "通过。"}, nil
	})
	if !errors.Is(err, context.Canceled) || calls != 1 || a.Decision != "" || a.Error == "" {
		t.Fatal("cancelled audit approved or retried", a, calls, err)
	}
}

func TestFinalReviewExhaustionDoesNotBanAngleOrReachRewrite(t *testing.T) {
	s := cycleTestState(t)
	s.FinalContentReviewRequired = true
	a := &anchorcycle.FinalContentAudit{}
	calls := 0
	err := runFinalMainlineReview(context.Background(), time.Second, a, func(context.Context) (anchorcycle.FinalReviewAttempt, error) {
		calls++
		return anchorcycle.FinalReviewAttempt{Raw: "请稍后再试"}, nil
	})
	if err == nil || a.Error == "" || calls != 2 {
		t.Fatal(a, calls, err)
	}
	c := anchorcycle.Candidate{ID: "same-draft", Text: "套餐包含已确认的菜品。", FinalContentAudit: a}
	if adopted, err := s.AddCandidate(c, anchorstylescore.Baseline{}); err != nil || adopted {
		t.Fatal(adopted, err)
	}
	if len(s.P0SkippedKeys) != 0 || s.P0PendingRepair != nil || len(s.Accepted) != 0 || s.Cursor != 0 || s.Phase != "opening" || len(s.GenerationSkips) != 1 || len(s.GenerationSkips[0].Attempts[0].FinalContentAudit.Attempts) != 2 {
		t.Fatal("technical audit failure poisoned eligibility, quota, or trace", s)
	}
}

func TestFinalReviewClearVerdictsDoNotRetryAndRejectionNeedsReason(t *testing.T) {
	for _, raw := range []string{"通过。", "有问题：团购价应为187元，正文说成了197元。", "不确定：没有给出使用期限。"} {
		a := &anchorcycle.FinalContentAudit{}
		calls := 0
		err := runFinalMainlineReview(context.Background(), time.Second, a, func(context.Context) (anchorcycle.FinalReviewAttempt, error) {
			calls++
			return anchorcycle.FinalReviewAttempt{Raw: raw}, nil
		})
		if err != nil || calls != 1 || a.Decision == "" {
			t.Fatal(raw, a, calls, err)
		}
	}
	a := &anchorcycle.FinalContentAudit{}
	calls := 0
	err := runFinalMainlineReview(context.Background(), time.Second, a, func(context.Context) (anchorcycle.FinalReviewAttempt, error) {
		calls++
		return anchorcycle.FinalReviewAttempt{Raw: "有问题。"}, nil
	})
	if err == nil || calls != 2 || a.Decision != "" {
		t.Fatal("empty rejection reason reached repair", a, calls, err)
	}
}

func TestAdvanceCannotBypassReviewBackoffOrOutagePause(t *testing.T) {
	for _, recovery := range []*anchorcycle.FinalReviewRecovery{
		{Paused: true, Failures: 3},
		{RetryAfter: time.Now().Add(time.Minute), Failures: 1},
	} {
		state := cycleTestState(t)
		state.FinalReviewRecovery = recovery
		repo := &supplyRepository{state: &state}
		server := &Server{anchorCycles: repo}
		// The isolated server has no DB/model provider. Entering generation
		// instead of respecting the guard would fail before a model can be used.
		got, err := server.advanceAnchorCycle(context.Background(), state.TenantID, state.PlanID, state.RoomID, state.RunID, state.Revision)
		if err != nil || got == nil || got.Revision != state.Revision || got.Cursor != state.Cursor || len(got.Attempts) != 0 || len(got.GenerationSkips) != 0 {
			t.Fatal("direct advance bypassed review backoff or changed eligibility", got, err)
		}
	}
}
