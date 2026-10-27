package agentdecision

import (
	"testing"
	"time"
)

func TestUrgentEventWaitsForCurrentAnswerAndLeadsNext(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	a := q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "a", Scene: "product_question", MissionKind: "reply_question", PriorityClass: "P1", Weight: 90}).Item
	if _, ok := q.ClaimNext(7); !ok {
		t.Fatal("claim A")
	}
	q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "c", Scene: "smalltalk", PriorityClass: "P3", Weight: 100})
	b := q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "b", Scene: "complaint", PriorityClass: "P0", Weight: 1}).Item
	if _, ok := q.ClaimNext(7); ok {
		t.Fatal("preempted active answer")
	}
	q.SetContentPlan(7, a.ID, "answer_question", "直接答案")
	q.Complete(7, a.ID)
	next, ok := q.ClaimNext(7)
	if !ok || next.ID != b.ID {
		t.Fatal("urgent event not next")
	}
	if h := q.Snapshot(7).PlayedHistory; len(h) != 1 || h[0].ContentGoal != "answer_question" {
		t.Fatal("missing actual content record")
	}
	if len(q.Snapshot(8).Queue) != 0 {
		t.Fatal("cross-room leakage")
	}
}
func TestFailedAndExpiredWorkNeverSpendAnswerFrequency(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	a := q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "a", Scene: "product_question", RateLimitPerMinute: 1, MaxRetries: 1, TTLSeconds: 20}).Item
	q.ClaimNext(7)
	q.Release(7, a.ID)
	if len(q.Snapshot(7).PlayedHistory) != 0 || len(q.Snapshot(7).RecentlyAnswered) != 0 {
		t.Fatal("failed work counted as spoken")
	}
	q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "b", Scene: "product_question", RateLimitPerMinute: 1, MaxRetries: 1})
	b, ok := q.ClaimNext(7)
	if !ok || b.Topic != "b" {
		t.Fatal("retry blocked unrelated work")
	}
	q.Complete(7, b.ID)
	now = now.Add(21 * time.Second)
	q.Sweep()
	if _, ok := q.Get(7, a.ID); !ok {
		t.Fatal("selected question vanished at candidate TTL")
	}
	q.Remove(7, a.ID)
	q.Enqueue(7, Candidate{Source: SourceAgent, Topic: "c", Scene: "product_question", RateLimitPerMinute: 1, TTLSeconds: 180})
	if _, ok := q.ClaimNext(7); ok {
		t.Fatal("actual answer did not spend frequency")
	}
	now = now.Add(time.Minute)
	if _, ok := q.ClaimNext(7); !ok {
		t.Fatal("frequency window did not release")
	}
}
func TestNewDuplicatesDoNotExtendShortEventTTL(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	a := q.Enqueue(7, Candidate{Topic: "welcome", Scene: "welcome", TTLSeconds: 10}).Item
	now = now.Add(8 * time.Second)
	q.Enqueue(7, Candidate{Topic: "welcome", Scene: "welcome", TTLSeconds: 10})
	if got, _ := q.Get(7, a.ID); !got.ExpiresAt.Equal(a.ExpiresAt) {
		t.Fatal("duplicate extended TTL")
	}
	now = now.Add(3 * time.Second)
	if len(q.Snapshot(7).Queue) != 0 {
		t.Fatal("stale welcome retained")
	}
}

func TestManualQuickGetsDefaultMissionKind(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	withQuestion := q.Enqueue(9, Candidate{Source: SourceManual, Topic: "manual-q", Question: "团购不都是这样", ManualAction: "quick"})
	if withQuestion.Item.MissionKind != "reply_question" {
		t.Fatalf("manual question kind=%q want reply_question", withQuestion.Item.MissionKind)
	}
	chatOnly := q.Enqueue(9, Candidate{Source: SourceManual, Topic: "manual-c", ManualAction: "quick"})
	if chatOnly.Item.MissionKind != "reply_chat" {
		t.Fatalf("manual chat kind=%q want reply_chat", chatOnly.Item.MissionKind)
	}
}
