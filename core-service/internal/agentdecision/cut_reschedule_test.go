package agentdecision

import (
	"testing"
	"time"
)

func TestCutReschedulePreservesPreparedAnswerWithoutSpendingGenerationRetry(t *testing.T) {
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 10)
	item := q.Enqueue(27, Candidate{Topic: "shipping", Scene: "product_question", MaxRetries: 0, PreparedTaskID: "old-track", PreparedCutMS: 26376}).Item
	expires := item.ExpiresAt
	for attempt := 0; attempt < 5; attempt++ {
		claimed, ok := q.ClaimNext(27)
		if !ok || claimed.Attempts != 1 {
			t.Fatalf("failed to reclaim prepared answer: %+v", claimed)
		}
		pending, ok := q.RescheduleCut(27, item.ID)
		if !ok || !pending.AudioPrepared || pending.Status != StatusPending || pending.Attempts != 0 || !pending.ExpiresAt.Equal(expires) {
			t.Fatalf("cut wait changed execution retry/deadline: %+v", pending)
		}
		if pending.PreparedTaskID != "" || pending.PreparedCutMS != 0 {
			t.Fatal("old mainline cut survived rescheduling")
		}
		if _, ok := q.ClaimNext(27); ok {
			t.Fatal("cut reschedule should yield briefly to other queued work")
		}
		now = now.Add(time.Second)
	}
	now = expires.Add(time.Millisecond)
	if _, ok := q.ClaimNext(27); ok {
		t.Fatal("cut reschedule extended the event lifetime")
	}
}

func TestGenerationFailureStillUsesConfiguredRetryLimit(t *testing.T) {
	q := New()
	item := q.Enqueue(27, Candidate{Topic: "shipping", Scene: "product_question", MaxRetries: 0}).Item
	q.ClaimNext(27)
	q.Release(27, item.ID)
	if _, exists := q.Get(27, item.ID); exists {
		t.Fatal("ordinary generation failure bypassed its retry limit")
	}
}
