package speechruntime

import (
	"testing"
	"time"
)

func TestReadyWaitAndNewTaskNeverInheritPlaybackClock(t *testing.T) {
	now := time.Now()
	old := now.Add(-time.Minute)
	r := newRegistry(func() time.Time { return now })
	r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusCompleted, DecisionID: "old", SpeechTaskID: "old-audio", DurationMS: 1000, StartedAt: &old})
	until := now.Add(15 * time.Second)
	s, _ := r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusReady, DecisionID: "new", SpeechTaskID: "new-audio", DurationMS: 120, PreparedUntil: &until})
	if s.Interrupt.StartedAt != nil {
		t.Fatal("old playback start inherited")
	}
	now = now.Add(10 * time.Second)
	if _, bad := r.FailExpiredInterrupt(29, "new-audio", "new", old, 4*time.Second); bad {
		t.Fatal("ready failed using duration")
	}
	if _, bad := r.FailUnstartedInterrupt(29, "new-audio", "new", until); bad {
		t.Fatal("startup deadline not reached")
	}
	start := now
	r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, DecisionID: "new", SpeechTaskID: "new-audio", DurationMS: 120, StartedAt: &start})
	if _, bad := r.FailExpiredInterrupt(29, "new-audio", "new", old, 4*time.Second); bad {
		t.Fatal("real PCM start ignored")
	}
	now = now.Add(4120 * time.Millisecond)
	if _, bad := r.FailExpiredInterrupt(29, "new-audio", "new", old, 4*time.Second); !bad {
		t.Fatal("true stalled playback not bounded")
	}
}
func TestDelayedReadyCannotRegressPCMAndOldReceiptCannotReplaceTask(t *testing.T) {
	r := New()
	start := time.Now()
	r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, DecisionID: "d", SpeechTaskID: "a", DurationMS: 2000, StartedAt: &start})
	s, _ := r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusReady, DecisionID: "d", SpeechTaskID: "a", KeepPlaybackState: true})
	if s.Interrupt.Status != StatusPlaying {
		t.Fatal("dispatch overwrote first PCM")
	}
	r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusReady, DecisionID: "new", SpeechTaskID: "b"})
	if _, err := r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusCompleted, DecisionID: "d", SpeechTaskID: "a", ExpectedDecisionID: "d", ExpectedTaskID: "a"}); err == nil {
		t.Fatal("old event replaced newer task")
	}
}

func TestPlaybackCASCannotReviveResetOrPausedRuntime(t *testing.T) {
	for _, status := range []Status{StatusIdle, StatusPaused} {
		r := New()
		_, _ = r.Update(29, UpdateInput{Track: TrackInterrupt, Status: status, DecisionID: "decision", SpeechTaskID: "task"})
		if status == StatusIdle {
			r.Reset(29)
		}
		if _, err := r.Update(29, UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, DecisionID: "decision", SpeechTaskID: "task", ExpectedDecisionID: "decision", ExpectedTaskID: "task", RequireIdentity: true}); err == nil {
			t.Fatal("revived", status)
		}
	}
}
