package agentdecision

import (
	"testing"
	"time"
)

func TestComputedPreparationDelayAndTerminalIdentity(t *testing.T) {
	now := time.Date(2026, 11, 5, 1, 38, 22, 0, time.UTC)
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	item := q.Enqueue(29, Candidate{Topic: "welcome-source", Title: "欢迎", MissionKind: "welcome_batch", TTLSeconds: 30}).Item
	q.ClaimExpected(29, item.ID)
	yielded, ok := q.YieldPreparation(29, item.ID, "interaction-scheduler: audio_duration_budget", 12*time.Second)
	if !ok || !yielded.RetryAfter.Equal(now.Add(12*time.Second)) || !yielded.ExpiresAt.Equal(item.ExpiresAt) || yielded.Attempts != 0 {
		t.Fatal(yielded)
	}
	if q.ClaimableHead(29) != nil {
		t.Fatal("claimed before computed recovery")
	}
	now = now.Add(12 * time.Second)
	if _, ok := q.ClaimExpected(29, item.ID); !ok {
		t.Fatal("not resumed at recovery")
	}
	q.YieldPreparation(29, item.ID, "interaction-scheduler: audio_duration_budget", 18*time.Second)
	view := q.Snapshot(29)
	if len(view.Queue) != 0 || len(view.TerminalHistory) != 1 {
		t.Fatal(view)
	}
	x := view.TerminalHistory[0]
	if x.Reason != "audio_duration_budget_deadline" || x.MissionKind != "welcome_batch" || x.Topic != item.Topic || x.Title != "欢迎" || x.PreparationReason == "" || !x.ExpiresAt.Equal(item.ExpiresAt) {
		t.Fatal(x)
	}
}

func TestBoundLongSpeechLifetimeBelongsToPCMStateMachine(t *testing.T) {
	now := time.Now()
	q := NewWithClock(func() time.Time { return now }, time.Minute, time.Minute, 24)
	item := q.Enqueue(29, Candidate{Topic: "long-audio", TTLSeconds: 30}).Item
	q.ClaimExpected(29, item.ID)
	if !q.BindPlayback(29, item.ID, "real-pcm-task") {
		t.Fatal("bind")
	}
	now = now.Add(ClaimExecutionTimeout + time.Second)
	if got, ok := q.Get(29, item.ID); !ok || got.PlaybackTaskID != "real-pcm-task" {
		t.Fatal("queue timer cut active PCM", got)
	}
	if !q.FailPlayback(29, item.ID, "real-pcm-task", "actual_source_failure") {
		t.Fatal("real source failure must still release")
	}
}
