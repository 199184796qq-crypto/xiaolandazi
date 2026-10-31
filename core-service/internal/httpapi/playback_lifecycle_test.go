package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"testing"
	"time"
)

func TestPCMStartRechecksFinalReservationAndRejectsOldEpoch(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	s.agentDecisions.BindPlayback(94, item.ID, "voice")
	s.SetAudioClient(&fakeAudioTaskClient{}, "")
	state := s.audioDevState()
	meta := &audioInteractionMeta{RoomID: 94, RuntimeEpoch: s.currentInteractionEpoch(94), DecisionID: item.ID}
	state.interactions["voice"] = meta
	seen := 0
	s.messages.Register("budget", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.reserve-audio" {
			return nil
		}
		seen++
		var p struct {
			Duration int `json:"duration_ms"`
		}
		json.Unmarshal(m.Payload, &p)
		if p.Duration != 4000 {
			t.Error(p)
		}
		return errors.New("another interaction just ended")
	})
	event := audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "voice", ReceiverID: "core-pcm-output", Status: "STARTING", DurationMS: 4000, OccurredAt: time.Now()}
	raw, _ := json.Marshal(event)
	if errs := s.messages.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.AudioPlayback, RoomID: 94, TaskID: "voice", Payload: raw}); len(errs) == 0 || seen != 1 {
		t.Fatal("final PCM admission ignored budget", errs, seen)
	}
	meta.RuntimeEpoch = "old-run"
	if err := s.validatePCMInteractionStart(context.Background(), event); err == nil || seen != 1 {
		t.Fatal("old epoch reached playback reservation", err, seen)
	}
}

func TestPCMPlayingReceiptKeepsRuntimeEpochAndTaskIdentity(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	s.agentDecisions.BindPlayback(94, item.ID, "voice")
	s.SetAudioClient(&fakeAudioTaskClient{}, "")
	epoch := s.currentInteractionEpoch(94)
	s.audioDevState().interactions["voice"] = &audioInteractionMeta{RoomID: 94, RuntimeEpoch: epoch, DecisionID: item.ID}
	count := 0
	s.messages.Register("receipt", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == "interaction.v2.playback" {
			count++
			if m.RunID != epoch || m.TaskID != "voice" {
				t.Error("lost playback fence", m)
			}
		}
		return nil
	})
	event := audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "voice", ReceiverID: "core-pcm-output", Status: "PLAYING", OccurredAt: time.Now()}
	raw, _ := json.Marshal(event)
	s.messages.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.AudioPlayback, RoomID: 94, TaskID: "voice", Payload: raw})
	if count != 1 {
		t.Fatal("PCM start receipt missing", count)
	}
}
