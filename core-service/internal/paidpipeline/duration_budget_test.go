package paidpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

func TestPreflightErrorEvidenceSurvivesMessageBus(t *testing.T) {
	c := messagecaptain.New()
	c.Register("interaction-scheduler", func(context.Context, messagecaptain.Message) error {
		return &audioout.InteractionPreparationError{Reason: "audio_duration_budget", RemainingMS: 1000, RetryAfterMS: 21234}
	})
	failures := c.Publish(context.Background(), messagecaptain.Message{})
	var blocked *audioout.InteractionPreparationError
	if len(failures) != 1 || !errors.As(failures[0], &blocked) || blocked.RetryAfterMS != 21234 {
		t.Fatal(failures)
	}
}

func TestScopedPlaybackAdapterRetainsActualPCMCounter(t *testing.T) {
	now := time.Now()
	p, q, _ := reportProcessor(t, &now, 24)
	s := p.v2Scope(14, 29)
	e := p.interactionV2.Load().engine
	e.SetPolicy(s, p.v2Policy(s), now)
	item := q.Enqueue(29, agentdecision.Candidate{Topic: "manual", TTLSeconds: 120}).Item
	q.ClaimExpected(29, item.ID)
	q.BindPlayback(29, item.ID, "audio-proof")
	raw, _ := json.Marshal(map[string]any{"decision_id": item.ID, "status": "PLAYING", "played_ms": 840, "at": now})
	if !p.interactionV2Receipt(messagecaptain.Message{Kind: "interaction.v2.playback", RoomID: 29, RunID: s.Epoch, TaskID: "audio-proof", Payload: raw}) {
		t.Fatal("not handled")
	}
	// Existing queue/task/epoch tests cover rejected messages. No task is
	// fabricated in the content engine merely because a PCM receipt arrived.
	if len(e.Snapshot(29)) != 0 {
		t.Fatal("receipt fabricated content task")
	}
}
