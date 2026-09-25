package questionqueue

import (
	"testing"
	"time"
)

func TestQueueExpiresAfterTwoMinutes(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 45*time.Second)

	item, merged := q.Enqueue(1001, "这个鸡怎么吃？", "HOW_TO_EAT", "u1")
	if merged || item.ID == "" {
		t.Fatalf("unexpected enqueue: merged=%v item=%#v", merged, item)
	}
	now = now.Add(119 * time.Second)
	if got := q.List(1001); len(got) != 1 {
		t.Fatalf("before ttl len=%d want=1", len(got))
	}
	now = now.Add(2 * time.Second)
	if got := q.List(1001); len(got) != 0 {
		t.Fatalf("expired question remained: %#v", got)
	}
}

func TestRepeatedQuestionRefreshesRelevanceWindow(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 45*time.Second)

	first, _ := q.Enqueue(1001, "上海什么时候到？", "DELIVERY", "u1")
	now = now.Add(90 * time.Second)
	second, merged := q.Enqueue(1001, " 上海什么时候到？ ", "DELIVERY", "u2")
	if !merged || second.ID != first.ID || second.Count != 2 {
		t.Fatalf("repeat not merged: %#v", second)
	}
	now = now.Add(90 * time.Second)
	if got := q.List(1001); len(got) != 1 {
		t.Fatalf("refreshed question expired too early: %#v", got)
	}
}

func TestClaimLeaseReturnsQuestionToPending(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 45*time.Second)

	item, _ := q.Enqueue(1001, "多少钱？", "PRICE", "u1")
	claimed, ok := q.ClaimNext(1001)
	if !ok || claimed.ID != item.ID || claimed.Status != StatusClaimed {
		t.Fatalf("claim=%#v ok=%v", claimed, ok)
	}
	if _, ok := q.ClaimNext(1001); ok {
		t.Fatal("claimed question was double claimed")
	}
	now = now.Add(46 * time.Second)
	reclaimed, ok := q.ClaimNext(1001)
	if !ok || reclaimed.ID != item.ID {
		t.Fatalf("claim lease did not recover: %#v ok=%v", reclaimed, ok)
	}
}

func TestClearRoomDropsPendingQuestions(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 45*time.Second)
	q.Enqueue(1001, "这个鸡怎么吃？", "HOW_TO_EAT", "u1")
	q.ClearRoom(1001)
	if got := q.List(1001); len(got) != 0 {
		t.Fatalf("room queue not cleared: %#v", got)
	}
}

func TestReleaseUsesRetryDelay(t *testing.T) {
	now := time.Date(2026, 9, 25, 3, 0, 0, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, 2*time.Minute, 45*time.Second)

	item, _ := q.Enqueue(1001, "什么时候发货？", "DELIVERY", "u1")
	if _, ok := q.ClaimNext(1001); !ok {
		t.Fatal("claim failed")
	}
	if _, ok := q.Release(1001, item.ID, 15*time.Second); !ok {
		t.Fatal("release failed")
	}
	if _, ok := q.ClaimNext(1001); ok {
		t.Fatal("retry delay ignored")
	}
	now = now.Add(16 * time.Second)
	if _, ok := q.ClaimNext(1001); !ok {
		t.Fatal("question not claimable after retry delay")
	}
}

func TestDropByUserRemovesOnlyMatchingUser(t *testing.T) {
	q := New()
	q.Enqueue(1001, "多少钱？", "PRICE", "u-1")
	q.Enqueue(1001, "怎么发货？", "LOGISTICS", "u-2")
	q.Enqueue(1001, "还有吗？", "STOCK", "u-1")

	if dropped := q.DropByUser(1001, "u-1"); dropped != 2 {
		t.Fatalf("dropped=%d want=2", dropped)
	}
	items := q.List(1001)
	if len(items) != 1 || items[0].UserID != "u-2" {
		t.Fatalf("remaining=%#v", items)
	}
}
