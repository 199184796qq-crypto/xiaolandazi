package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"time"
)

func (s *Server) currentInteractionEpoch(roomID int64) string {
	if s.agentWork == nil {
		return ""
	}
	r := s.agentWork.Get(roomID)
	return fmt.Sprintf("%s:%d:%d", r.BootID, r.PlanID, r.RunGeneration)
}

// The same actual PCM producer that emits PLAYING checks the reservation just
// before the first voice frame. Synthesized/queued audio is not proof of playback.
func (s *Server) validatePCMInteractionStart(ctx context.Context, event audioout.PlaybackEvent) error {
	state := s.audioDevState()
	if state == nil {
		return fmt.Errorf("audio state unavailable")
	}
	state.mu.Lock()
	meta := state.interactions[event.SpeechTaskID]
	state.mu.Unlock()
	if meta == nil {
		return fmt.Errorf("playback identity not registered")
	}
	if meta.DecisionID == "" {
		return nil
	} // Dedicated development audio has no live decision.
	item, ok := s.agentDecisions.Get(meta.RoomID, meta.DecisionID)
	if !ok || item.Status != agentdecision.StatusClaimed || item.PlaybackTaskID != event.SpeechTaskID || meta.RoomID != event.RoomID || meta.RuntimeEpoch == "" || meta.RuntimeEpoch != s.currentInteractionEpoch(meta.RoomID) || !s.agentWork.IsWorking(meta.RoomID) {
		return fmt.Errorf("playback decision/run changed")
	}
	var envelope interactionV2Envelope
	json.Unmarshal(item.PurposeKnowledge, &envelope)
	if envelope.Protocol == "interaction_v2" && !s.validInteractionV2Item(*item, meta.RoomID, envelope.Tenant, time.Now()) {
		return fmt.Errorf("interaction deadline/run changed before PCM")
	}
	if s.messages != nil {
		raw, _ := json.Marshal(map[string]any{"decision_id": item.ID, "duration_ms": event.DurationMS, "start_at": event.OccurredAt})
		if errs := s.messages.Publish(ctx, messagecaptain.Message{Kind: "interaction.v2.reserve-audio", RoomID: meta.RoomID, RunID: meta.RuntimeEpoch, TaskID: event.SpeechTaskID, Payload: raw}); len(errs) > 0 {
			return fmt.Errorf("playback reservation refused: %v", errs[0])
		}
	}
	return nil
}
