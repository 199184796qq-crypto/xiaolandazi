package httpapi

import (
	"livecompanion/core/internal/agentdecision"
	"net/http"
	"time"
)

func (s *Server) validateInteractionDecision(w http.ResponseWriter, roomID int64, input roomAudioInteractionInput) bool {
	if input.DecisionID == "" || s.agentDecisions == nil {
		return true
	}
	item, ok := s.agentDecisions.Get(roomID, input.DecisionID)
	if !ok || item.Status != agentdecision.StatusClaimed {
		writeError(w, http.StatusConflict, "互动事件已过期或已撤销")
		return false
	}
	if item.OriginPlanID > 0 && s.agentWork != nil && s.agentWork.Get(roomID).PlanID != item.OriginPlanID {
		writeError(w, http.StatusConflict, "互动事件所属方案已切换，请重新判断")
		return false
	}
	if item.AudioPrepared && !item.PreparedUntil.IsZero() && !time.Now().Before(item.PreparedUntil) {
		writeError(w, http.StatusConflict, "已准备声音保护期结束")
		return false
	}
	s.agentDecisions.SetContentPlan(roomID, item.ID, input.ContentGoal, input.ContentAngle)
	return true
}
