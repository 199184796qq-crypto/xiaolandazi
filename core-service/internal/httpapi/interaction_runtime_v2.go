package httpapi

import (
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"time"
)

type interactionV2Envelope struct {
	Protocol, Task, Epoch string
	Tenant                int64     `json:"tenant_id"`
	Room                  int64     `json:"room_id"`
	Plan                  int64     `json:"plan_id"`
	Expires               time.Time `json:"expires_at"`
}

func decodeInteractionV2Item(item agentdecision.Item) (interactionV2Envelope, bool) {
	var e interactionV2Envelope
	err := json.Unmarshal(item.PurposeKnowledge, &e)
	return e, err == nil && e.Protocol == "interaction_v2"
}
func hasInteractionV2Body(item agentdecision.Item) bool {
	_, ok := decodeInteractionV2Item(item)
	return ok
}
func (s *Server) validInteractionV2Item(item agentdecision.Item, roomID, tenantID int64, now time.Time) bool {
	e, v2 := decodeInteractionV2Item(item)
	if !v2 {
		return true
	}
	if e.Task == "" || e.Room != roomID || e.Plan <= 0 || e.Tenant <= 0 || e.Tenant != tenantID || e.Expires.IsZero() || !now.Before(e.Expires) || s.agentWork == nil {
		return false
	}
	run := s.agentWork.Get(roomID)
	epoch := fmt.Sprintf("%s:%d:%d", run.BootID, run.PlanID, run.RunGeneration)
	return e.Epoch == epoch && run.PlanID == e.Plan && s.agentWork.IsWorking(roomID)
}
