package speechruntime

import (
	"sync"
	"testing"
)

func TestReturningCompletionRequiresSameTaskAndPreservesTrace(t *testing.T) {
	r := New()
	input := UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, SpeechTaskID: "task", DecisionID: "decision", MissionID: "mission", ReplyText: "原回答", BridgeText: "衔接", BridgeUsed: true, AudioURL: "https://example.test/voice.wav", DurationMS: 1000}
	if _, err := r.Update(44, input); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.CompleteReturning(44, "task", "decision"); ok {
		t.Fatal("playing task completed early")
	}
	input.Status = StatusReturning
	if _, err := r.Update(44, input); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.CompleteReturning(44, "wrong", "decision"); ok {
		t.Fatal("wrong task accepted")
	}
	if _, ok := r.CompleteReturning(44, "task", "wrong"); ok {
		t.Fatal("wrong decision accepted")
	}
	snapshot, ok := r.CompleteReturning(44, "task", "decision")
	i := snapshot.Interrupt
	if !ok || i.Status != StatusCompleted || i.ReplyText != input.ReplyText || i.MissionID != input.MissionID || i.BridgeText != input.BridgeText || !i.BridgeUsed || i.AudioURL != input.AudioURL || i.DurationMS != 1000 {
		t.Fatal("lost trace", snapshot, ok)
	}
	if _, ok := r.CompleteReturning(44, "task", "decision"); ok {
		t.Fatal("duplicate completion accepted")
	}
	r.Reset(44)
	if _, ok := r.CompleteReturning(44, "task", "decision"); ok {
		t.Fatal("stopped state recreated")
	}
}

func TestLateReturningCompletionCannotOverwriteConcurrentReplacement(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		r := New()
		_, err := r.Update(44, UpdateInput{Track: TrackInterrupt, Status: StatusReturning, SpeechTaskID: "old", DecisionID: "old-decision"})
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); <-start; r.CompleteReturning(44, "old", "old-decision") }()
		go func() {
			defer wg.Done()
			<-start
			_, _ = r.Update(44, UpdateInput{Track: TrackInterrupt, Status: StatusPlaying, SpeechTaskID: "new", DecisionID: "new-decision", ReplyText: "新回答"})
		}()
		close(start)
		wg.Wait()
		snapshot, _ := r.Snapshot(44)
		if snapshot.Interrupt.SpeechTaskID != "new" || snapshot.Interrupt.DecisionID != "new-decision" || snapshot.Interrupt.Status != StatusPlaying || snapshot.Interrupt.ReplyText != "新回答" {
			t.Fatal("new interaction overwritten", snapshot)
		}
	}
}
