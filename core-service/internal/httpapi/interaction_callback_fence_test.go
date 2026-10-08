package httpapi

import (
	"context"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"testing"
	"time"
)

func TestLateCompletionCannotAnswerReleasedInteraction(t *testing.T) {
	q := agentdecision.New()
	item := q.Enqueue(7, agentdecision.Candidate{Topic: "question", Scene: "product_question", MaxRetries: 2}).Item
	q.ClaimNext(7)
	q.BindPlayback(7, item.ID, "old-task")
	q.Release(7, item.ID)
	s := &Server{agentDecisions: q}
	state := &audioDevState{events: map[string]audioout.PlaybackEvent{}, interactions: map[string]*audioInteractionMeta{"old-task": {RoomID: 7, DecisionID: item.ID}}}
	s.applyAudioInteractionPlaybackEvent(context.Background(), state, audioout.PlaybackEvent{RoomID: 7, SpeechTaskID: "old-task", Status: "COMPLETED", OccurredAt: time.Now().UTC()})
	if _, ok := q.Get(7, item.ID); !ok {
		t.Fatal("late old audio completed released decision")
	}
	if len(q.Snapshot(7).PlayedHistory) != 0 {
		t.Fatal("late completion spent cooldown")
	}
}
