package speechruntime

import (
	"testing"
	"time"
)

func TestExpiredInterruptOnlyFailsSameActiveTask(t *testing.T) {
	now := time.Date(2026, 10, 23, 3, 30, 0, 0, time.UTC)
	start := now.Add(-10 * time.Second)
	for _, test := range []struct {
		name, observedTask, observedDecision string
		status                               Status
		want                                 bool
	}{
		{"same playing", "task", "decision", StatusPlaying, true},
		{"same ready without start receipt", "task", "decision", StatusReady, true},
		{"replacement task", "old", "decision", StatusPlaying, false},
		{"replacement decision", "task", "old", StatusPlaying, false},
		{"manual pause", "task", "decision", StatusPaused, false},
		{"already complete", "task", "decision", StatusCompleted, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newRegistry(func() time.Time { return now })
			_, err := r.Update(29, UpdateInput{Track: TrackInterrupt, Status: test.status, SpeechTaskID: "task", DecisionID: "decision", DurationMS: 1000, StartedAt: &start})
			if err != nil {
				t.Fatal(err)
			}
			got, changed := r.FailExpiredInterrupt(29, test.observedTask, test.observedDecision, start, 4*time.Second)
			if changed != test.want {
				t.Fatalf("changed=%t want=%t", changed, test.want)
			}
			if changed && got.Interrupt.Status != StatusFailed {
				t.Fatal(got)
			}
			if !changed && got.Interrupt.Status != test.status {
				t.Fatal("watchdog changed another lifecycle", got)
			}
		})
	}
}

func TestExpiredInterruptHonorsLaterStartAndExtendedDuration(t *testing.T) {
	now := time.Date(2026, 10, 23, 3, 30, 0, 0, time.UTC)
	r := newRegistry(func() time.Time { return now })
	old := now.Add(-10 * time.Second)
	start := now.Add(-time.Second)
	_, _ = r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, SpeechTaskID: "task", DecisionID: "decision", DurationMS: 10000, StartedAt: &start})
	if _, changed := r.FailExpiredInterrupt(29, "task", "decision", old, 4*time.Second); changed {
		t.Fatal("expired a buffered or extended answer")
	}
	now = now.Add(14 * time.Second)
	if _, changed := r.FailExpiredInterrupt(29, "task", "decision", old, 4*time.Second); !changed {
		t.Fatal("did not expire stalled answer")
	}
	if _, changed := r.FailExpiredInterrupt(29, "task", "decision", old, 4*time.Second); changed {
		t.Fatal("expired twice")
	}
}
