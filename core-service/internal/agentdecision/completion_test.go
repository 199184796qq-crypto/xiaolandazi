package agentdecision

import (
	"testing"
	"time"
)

func TestAdmittedQuestionsSurviveEventTTLAndYieldDuringRetry(t *testing.T) {
	now := time.Now()
	q := NewWithClock(func() time.Time { return now }, time.Second, time.Second, 2)
	a := q.Enqueue(1, Candidate{Topic: "PURPOSE:A", Scene: "product_question", TTLSeconds: 1, Priority: 90, MaxRetries: 2, EventID: 1}).Item
	b := q.Enqueue(1, Candidate{Topic: "PURPOSE:B", Scene: "product_question", TTLSeconds: 1, Priority: 50, MaxRetries: 2, EventID: 2}).Item
	now = now.Add(2 * time.Second)
	claimed, ok := q.ClaimNext(1)
	if !ok || claimed.ID != a.ID {
		t.Fatal("admitted question vanished at candidate freshness deadline")
	}
	q.Release(1, a.ID)
	claimed, ok = q.ClaimNext(1)
	if !ok || claimed.ID != b.ID {
		t.Fatal("retrying head blocked ready work")
	}
	q.Complete(1, b.ID)
	now = now.Add(6 * time.Second)
	q.ClaimNext(1)
	q.Release(1, a.ID)
	now = now.Add(6 * time.Second)
	q.ClaimNext(1)
	q.Release(1, a.ID)
	s := q.Snapshot(1)
	if s.Completion.Admitted != 2 || s.Completion.Completed != 1 || s.Completion.Failed != 1 || len(s.Queue) != 0 || len(s.TerminalHistory) != 2 {
		t.Fatal("incomplete outcome accounting", s.Completion, s.TerminalHistory)
	}
}
