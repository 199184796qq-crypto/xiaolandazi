package httpapi

import "livecompanion/core/internal/agentdecision"

// The room's saved configuration stays frozen; live heat and population are
// refreshed when execution starts, rather than retained from event arrival.
func (s *Server) refreshClaimInteractionContext(roomID int64, item *agentdecision.Item) {
	if item == nil || s.brain == nil {
		return
	}
	view, err := s.brain.Snapshot(roomID)
	if err != nil {
		return
	}
	decision := &item.InteractionDecision
	decision.Heat = view.Intelligence.Heat
	if s.strategyPolicies != nil && !s.strategyPolicies.RoomInteractionPreferences(roomID).AutoHeat {
		decision.Heat = "WARM"
	}
	decision.OnlineCount = view.Intelligence.OnlineCount
	decision.UniqueChatters30s = view.Intelligence.UniqueChatters30s
	decision.QuestionPressure = view.Intelligence.QuestionPressure
}
