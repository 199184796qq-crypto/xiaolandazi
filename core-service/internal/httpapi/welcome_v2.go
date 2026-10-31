package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/messagecaptain"
)

func readyWelcomeItem(item agentdecision.Item) bool {
	var m struct {
		Welcome bool      `json:"welcome_quota"`
		Ready   time.Time `json:"welcome_ready_at"`
	}
	return hasInteractionV2Body(item) && json.Unmarshal(item.PurposeKnowledge, &m) == nil && m.Welcome && !m.Ready.IsZero()
}

// Internal, authenticated two-phase admission. A generation claim is not yet a
// playable welcome. Only the executor with complete text and audio calls here.
func (s *Server) admitWelcomeV2(w http.ResponseWriter, r *http.Request) {
	room, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if s.agentDecisions == nil || s.messages == nil {
		writeError(w, 503, "欢迎调度尚未就绪")
		return
	}
	id := r.PathValue("decisionID")
	item, ok := s.agentDecisions.Get(room, id)
	var marker struct {
		Welcome bool `json:"welcome_quota"`
	}
	if !ok || json.Unmarshal(item.PurposeKnowledge, &marker) != nil || !marker.Welcome || !s.validInteractionV2Item(*item, room, tenant, time.Now()) {
		writeError(w, 409, "欢迎任务已经过期或场次变化")
		return
	}
	raw, _ := json.Marshal(map[string]string{"decision_id": id})
	if failures := s.messages.Publish(r.Context(), messagecaptain.Message{Kind: "interaction.v2.welcome-ready", RoomID: room, Payload: raw}); len(failures) > 0 {
		writeError(w, 409, "欢迎准备已失效，请重新调度")
		return
	}
	item, ok = s.agentDecisions.Get(room, id)
	if !ok || !item.AudioPrepared {
		writeError(w, 409, "欢迎成品尚未入队")
		return
	}
	writeJSON(w, 200, map[string]any{"expires_at": item.ExpiresAt, "knowledge": item.PurposeKnowledge})
}
