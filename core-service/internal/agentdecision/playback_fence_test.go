package agentdecision

import (
	"testing"
	"time"
)

func TestRetriedDecisionHasDistinctPlaybackIdentity(t *testing.T) {
	now := time.Now().UTC()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	item := q.Enqueue(7, Candidate{Topic: "question", Scene: "product_question", MaxRetries: 2}).Item
	q.ClaimNext(7)
	if !q.BindPlayback(7, item.ID, "old-audio") {
		t.Fatal("bind first")
	}
	q.Release(7, item.ID)
	now = now.Add(5 * time.Second)
	q.ClaimNext(7)
	q.BindPlayback(7, item.ID, "new-audio")
	current, ok := q.Get(7, item.ID)
	if !ok || current.PlaybackTaskID != "new-audio" {
		t.Fatal("retry audio identity not fenced")
	}
	if len(q.Snapshot(7).PlayedHistory) != 0 {
		t.Fatal("bind counted as playback")
	}
}
