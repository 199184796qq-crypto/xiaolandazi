package httpapi

import (
	"net/http"
)

// roomPlanPurposes returns the auto-generated purpose list for the room's plan,
// rebuilding it from facts, links and benefits on each read (pure grouping).
func (s *Server) roomPlanPurposes(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenantID, roomID) {
		return
	}
	plan, err := s.store.GetLiveAgentPlanForRoom(r.Context(), tenantID, roomID)
	if err != nil || plan.ID <= 0 {
		writeError(w, http.StatusNotFound, "方案不存在")
		return
	}
	specs, err := s.store.BuildPlanPurposes(r.Context(), tenantID, plan.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成目的清单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plan_id": plan.ID, "items": specs})
}
