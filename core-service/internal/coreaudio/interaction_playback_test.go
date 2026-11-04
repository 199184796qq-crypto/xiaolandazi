package coreaudio

import (
	"context"
	"livecompanion/core/internal/audioout"
	"testing"
	"time"
)

func TestPCMReceiptKeepsFirstVoiceOriginAndTerminalEvidence(t *testing.T) {
	now := time.Now()
	c := &Client{now: func() time.Time { return now }}
	program := &programState{RoomID: 29, ID: "run"}
	task := audioout.SpeechTask{ID: "voice", RoomID: 29, DurationMS: 2880, SessionID: "session"}
	until := now.Add(15 * time.Second)
	c.prepareInteractionPlayback(program, task, "decision", until)
	event := audioout.PlaybackEvent{RoomID: 29, SpeechTaskID: "voice", ReceiverID: "core-pcm-output", Status: "STARTING", OccurredAt: now}
	c.recordInteractionPCM("voice", event)
	r, _ := c.InteractionPlaybackSnapshot(context.Background(), 29, "voice")
	if !r.StartedAt.IsZero() || !interactionPlaybackDeadline(r).Equal(until) {
		t.Fatal("reservation counted as voice")
	}
	now = now.Add(10 * time.Second)
	event.Status = "PLAYING"
	event.OccurredAt = now
	c.recordInteractionPCM("voice", event)
	first := now
	now = now.Add(time.Second)
	event.OccurredAt = now
	c.recordInteractionPCM("voice", event)
	r, _ = c.InteractionPlaybackSnapshot(context.Background(), 29, "voice")
	if !r.StartedAt.Equal(first) || !interactionPlaybackDeadline(r).Equal(first.Add(6880*time.Millisecond)) {
		t.Fatal("duplicate refreshed voice origin", r)
	}
	event.Status = "COMPLETED"
	c.recordInteractionPCM("voice", event)
	event.Status = "FAILED"
	c.recordInteractionPCM("voice", event)
	r, _ = c.InteractionPlaybackSnapshot(context.Background(), 29, "voice")
	if r.Event.Status != "COMPLETED" {
		t.Fatal("late failure replaced proven EOF")
	}
	if _, err := c.InteractionPlaybackSnapshot(context.Background(), 30, "voice"); err == nil {
		t.Fatal("cross-room receipt exposed")
	}
}
