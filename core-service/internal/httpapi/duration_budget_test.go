package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/speechruntime"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPreflightUsesCoreEstimatorAndPreservesComputedRecovery(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	s.SetAudioClient(&preparationWindowClient{cuts: []audioout.AcousticPoint{{Grade: "B", CutMS: 5000}}}, "")
	s.agentDecisions.ClaimNext(94)
	s.messages.Register("interaction-scheduler", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.preflight" {
			return nil
		}
		var input audioout.InteractionPreflightRequest
		json.Unmarshal(m.Payload, &input)
		if input.DurationMS != 23000 || input.Rate != 1.4 {
			t.Error(input.DurationMS, input.Rate)
		}
		return &audioout.InteractionPreparationError{Reason: "audio_duration_budget", RemainingMS: 10000, RetryAfterMS: 12500}
	})
	raw, _ := json.Marshal(map[string]any{"text": strings.Repeat("字", 140), "rate": 1.4, "stage": "tts", "duration_ms": 3})
	req := httptest.NewRequest("POST", "/preflight?tenant_id=7", bytes.NewReader(raw))
	req.SetPathValue("roomID", "94")
	req.SetPathValue("decisionID", item.ID)
	out := httptest.NewRecorder()
	s.preflightRoomAgentDecision(out, req)
	var result audioout.InteractionPreflightResult
	json.Unmarshal(out.Body.Bytes(), &result)
	if out.Code != 200 || result.Allowed || result.DurationMS != 23000 || result.RetryAfterMS != 12500 || result.RemainingMS != 10000 {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestPCMProgressCounterTravelsWithScopedPlaybackReceipt(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentDecisions.ClaimNext(94)
	s.agentDecisions.BindPlayback(94, item.ID, "voice")
	s.SetAudioClient(&fakeAudioTaskClient{}, "")
	epoch := s.currentInteractionEpoch(94)
	s.audioDevState().interactions["voice"] = &audioInteractionMeta{RoomID: 94, RuntimeEpoch: epoch, DecisionID: item.ID}
	s.speechRuntime.Update(94, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: speechruntime.StatusReady, DecisionID: item.ID, SpeechTaskID: "voice", DurationMS: 32200})
	seen := false
	s.messages.Register("counter-proof", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == "interaction.v2.playback" {
			var p struct {
				PlayedMS *int `json:"played_ms"`
			}
			json.Unmarshal(m.Payload, &p)
			if p.PlayedMS == nil || *p.PlayedMS != 840 || m.TaskID != "voice" || m.RunID != epoch {
				t.Error("lost PCM counter/fence", string(m.Payload))
			}
			seen = true
		}
		return nil
	})
	event := audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: "voice", ReceiverID: "core-pcm-output", Status: "PLAYING", ProgressMS: 840, DurationMS: 32200, OccurredAt: time.Now()}
	raw, _ := json.Marshal(event)
	s.messages.Publish(context.Background(), messagecaptain.Message{Kind: messagecaptain.AudioPlayback, RoomID: 94, TaskID: "voice", Payload: raw})
	if !seen {
		t.Fatal("no PCM receipt")
	}
}
