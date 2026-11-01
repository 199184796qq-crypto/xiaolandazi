package httpapi

import (
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/strategycenter"
)

func manualInteraction(item agentdecision.Item) bool {
	for _, src := range item.Sources {
		if src == agentdecision.SourceManual {
			return true
		}
	}
	return false
}
func (s *Server) interactionControlAllowed(room int64, item agentdecision.Item) bool {
	if s.strategyPolicies == nil || manualInteraction(item) {
		return true
	}
	kind := item.InteractionDecision.PrimaryEvent
	if kind == "" {
		kind = item.MissionKind
	}
	pref := s.strategyPolicies.RoomInteractionPreferences(room)
	if pref.ControlEnabled(kind) && pref.DisabledAt[strategycenter.ControlKind(kind)] <= item.ControlsRevision {
		return true
	}
	// Only the matching actual PCM receipt permits an already audible item to finish.
	if item.PlaybackTaskID != "" {
		if state := s.audioDevState(); state != nil {
			state.mu.Lock()
			event := state.events[item.PlaybackTaskID]
			meta := state.interactions[item.PlaybackTaskID]
			playing := meta != nil && meta.DecisionID == item.ID && meta.RoomID == room && event.ReceiverID == "core-pcm-output" && (event.Status == "PLAYING" || event.Status == "PROGRESS")
			state.mu.Unlock()
			return playing
		}
	}
	return false
}
func (s *Server) pruneDisabledInteractions(room int64) {
	if s.agentDecisions == nil {
		return
	}
	for _, item := range s.agentDecisions.Snapshot(room).Queue {
		if !s.interactionControlAllowed(room, item) {
			s.agentDecisions.Remove(room, item.ID)
		}
	}
}
