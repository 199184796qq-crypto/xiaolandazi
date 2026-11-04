package httpapi

import (
	"context"
	"encoding/json"

	"net/http"

	"livecompanion/core/internal/audioout"

	"livecompanion/core/internal/messagecaptain"

	"livecompanion/core/internal/speechruntime"
)

type interactionReturnReader interface {
	InteractionReturnSnapshot(context.Context, int64, string) (audioout.InteractionReturnSnapshot, error)
}

// Legacy text-timeline returns/bridges are permanently retired. No payload,
// tenant preference or runtime mode can re-enable semantic skipping.
func (s *Server) getInteractionReturn(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "旧文字时间轴恢复与跳句入口已永久移除")
}
func (s *Server) stageInteractionBridge(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusGone, "旧文字时间轴衔接与跳句入口已永久移除")
}

func (s *Server) handleReturnPlaybackMessage(_ context.Context, m messagecaptain.Message) error {
	if m.Kind != messagecaptain.ReturnBridgeStaged && m.Kind != messagecaptain.ReturnBridgeStarted {
		return nil
	}
	if s.speechRuntime == nil {
		return nil
	}
	current, err := s.speechRuntime.Snapshot(m.RoomID)
	if err != nil || current.Interrupt.SpeechTaskID != m.TaskID {
		return nil
	}
	var input struct {
		Text       string `json:"text"`
		DurationMS int    `json:"duration_ms"`
	}
	if err = json.Unmarshal(m.Payload, &input); err != nil {
		return err
	}
	part := current.Interrupt
	duration := part.DurationMS
	if input.DurationMS > duration {
		duration = input.DurationMS
	}
	started := part.StartedAt
	_, err = s.speechRuntime.Update(m.RoomID, speechruntime.UpdateInput{Track: speechruntime.TrackInterrupt, Status: part.Status, Text: part.Text, QuestionText: part.QuestionText, ReplyText: part.ReplyText, ResumeStrategy: part.ResumeStrategy, BridgeText: input.Text, BridgeUsed: m.Kind == messagecaptain.ReturnBridgeStarted, Source: part.Source, AudioURL: part.AudioURL, DecisionID: part.DecisionID, MissionID: part.MissionID, SpeechTaskID: part.SpeechTaskID, SwitchAtMS: part.SwitchAtMS, DurationMS: duration, StartedAt: started})
	if state := s.audioDevState(); state != nil {
		state.mu.Lock()
		if meta := state.interactions[m.TaskID]; meta != nil {
			meta.BridgeText = input.Text
			meta.BridgeUsed = m.Kind == messagecaptain.ReturnBridgeStarted
		}
		state.mu.Unlock()
	}
	return err
}
