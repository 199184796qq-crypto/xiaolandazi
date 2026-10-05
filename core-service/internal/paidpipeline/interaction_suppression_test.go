package paidpipeline

import (
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/strategycenter"
)

func TestSuppressedInteractionKeepsWindowAndDoesNotConsumeBudget(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 40, 0, 0, time.UTC)
	runtime := agentwork.New()
	if _, err := runtime.Set(15, agentwork.StateWorking); err != nil {
		t.Fatal(err)
	}
	decisions := agentdecision.NewWithClock(func() time.Time { return now }, 5*time.Minute, 90*time.Second, 64)
	p := New(runtime, decisions, strategycenter.New())

	first := decisions.Enqueue(15, agentdecision.Candidate{
		Source:      agentdecision.SourceAgent,
		Topic:       "INTERACTION:FOLLOW",
		Title:       "回应关注",
		Question:    "感谢关注",
		MissionKind: "reply_follow",
		Priority:    20,
	})
	if first.Item == nil {
		t.Fatal("seed decision was not enqueued")
	}
	if _, ok := decisions.Complete(15, first.Item.ID); !ok {
		t.Fatal("seed decision was not completed")
	}

	p.interactionMu.Lock()
	p.interactionWindows[15] = map[string]*interactionWindow{
		"reply_follow": {
			RoomID:        15,
			Key:           "reply_follow",
			PendingCount:  1,
			TotalEvents:   1,
			FirstPending:  now.Add(-46 * time.Second),
			LastEventAt:   now.Add(-time.Second),
			LatestEventID: 301,
			LatestUserID:  "u-301",
			LatestName:    "新关注用户",
			EventIDs:      []int64{301},
			UserIDs:       []string{"u-301"},
			Names:         []string{"新关注用户"},
		},
	}
	p.interactionMu.Unlock()

	p.FlushInteractionWindows(now)

	p.interactionMu.Lock()
	window := p.interactionWindows[15]["reply_follow"]
	pending := window.PendingCount
	retryAfter := window.RetryAfter
	p.interactionMu.Unlock()
	if pending != 1 {
		t.Fatalf("suppressed window must stay pending, got=%d", pending)
	}
	if !retryAfter.Equal(now.Add(90 * time.Second)) {
		t.Fatalf("retry_after=%s want=%s", retryAfter, now.Add(90*time.Second))
	}

	p.interactionBudgetMu.Lock()
	used := len(p.interactionBudgetUsed[15])
	p.interactionBudgetMu.Unlock()
	if used != 0 {
		t.Fatalf("suppressed mission must not consume interaction budget, used=%d", used)
	}
}
