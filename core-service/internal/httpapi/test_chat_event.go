package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"livecompanion/core/internal/model"
)

// injectTestChatEvent creates a real room event through the same store/hub as
// the collector. It is intentionally separate from manual decisions: downstream
// purpose resolution, fact selection and answer generation see a normal chat
// event, but the payload marks it as a test so operators can tell it apart.
func (s *Server) injectTestChatEvent(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	var input struct {
		UserID    string          `json:"user_id"`
		Nickname  string          `json:"nickname"`
		Content   string          `json:"content"`
		EventType string          `json:"event_type"`
		Payload   json.RawMessage `json:"payload"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Content = strings.TrimSpace(input.Content)
	if input.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if input.EventType = strings.TrimSpace(input.EventType); input.EventType == "" {
		input.EventType = "chat"
	}
	if input.Nickname = strings.TrimSpace(input.Nickname); input.Nickname == "" {
		input.Nickname = "测试观众"
	}
	if input.UserID = strings.TrimSpace(input.UserID); input.UserID == "" {
		input.UserID = "test-simulation"
	}
	if len(input.Payload) == 0 || string(input.Payload) == "null" {
		input.Payload = json.RawMessage(`{"test_simulation":true}`)
	}
	event, err := s.events.Create(r.Context(), room.TenantID, roomID, model.CreateEventInput{
		EventType: input.EventType,
		UserID:    input.UserID,
		Nickname:  input.Nickname,
		Content:   input.Content,
		Payload:   input.Payload,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create test event failed")
		return
	}
	if s.hub != nil {
		s.hub.Publish(event)
	}
	writeJSON(w, http.StatusCreated, event)
}
