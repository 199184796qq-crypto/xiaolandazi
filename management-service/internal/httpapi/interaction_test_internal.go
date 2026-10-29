package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionpurpose"
)

// internalTestInteraction runs the real pre-TTS chain for a simulated viewer:
// published facts -> real intent -> fact selection -> answer text. It does not
// create a room event, enqueue audio or touch the live session, so it is safe
// when the room is offline.
func (s *Server) internalTestInteraction(w http.ResponseWriter, r *http.Request) {
	if s.purposeInternalToken == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Core-Token")), []byte(s.purposeInternalToken)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid internal token")
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, _ := strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("tenant_id")), 10, 64)
	if tenantID <= 0 {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	var input struct {
		PlanID   int64  `json:"plan_id"`
		UserID   string `json:"user_id"`
		Nickname string `json:"nickname"`
		Text     string `json:"text"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	input.Text = strings.TrimSpace(input.Text)
	if input.PlanID <= 0 || input.Text == "" {
		writeError(w, http.StatusBadRequest, "plan_id and text are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()
	plan, err := s.store.GetLiveAgentPlan(ctx, tenantID, input.PlanID)
	if err != nil || plan.Status != "active" {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	// 1. Real intent resolution, explicitly in test mode (no live session).
	body, _ := json.Marshal(map[string]any{
		"tenant_id": tenantID, "room_id": roomID, "plan_id": input.PlanID,
		"user_id": input.UserID, "nickname": input.Nickname,
		"test_simulation": true, "text": input.Text,
	})
	req := httptest.NewRequest(http.MethodPost, "/internal/v1/interaction-purpose/resolve", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Core-Token", s.purposeInternalToken)
	rec := httptest.NewRecorder()
	s.resolveInteractionPurpose(rec, req)
	if rec.Code != http.StatusOK {
		writeError(w, http.StatusBadGateway, "intent resolution failed: "+strings.TrimSpace(rec.Body.String()))
		return
	}
	var intentResponse struct {
		Knowledge json.RawMessage `json:"knowledge"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &intentResponse); err != nil || len(intentResponse.Knowledge) == 0 {
		writeError(w, http.StatusBadGateway, "intent response invalid")
		return
	}
	var intent interactionpurpose.Result
	if err := json.Unmarshal(intentResponse.Knowledge, &intent); err != nil {
		writeError(w, http.StatusBadGateway, "intent knowledge invalid")
		return
	}
	effectiveQuestion := strings.TrimSpace(intent.Input)
	if effectiveQuestion == "" {
		effectiveQuestion = input.Text
	}
	if intent.Ambiguous && strings.TrimSpace(intent.Clarify) != "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"question": effectiveQuestion, "intent": intent, "reply": intent.Clarify,
			"execution_mode": "clarify", "plan_name": plan.Name,
		})
		return
	}
	// 2. Real answer generation using the same plan-scoped test store as the
	// existing simulation endpoint, now carrying the real intent.
	worker := decisionexecutor.New(&planSpeechTestStore{Store: s.store, tenantID: tenantID, roomID: roomID, plan: plan}, nil, s.speechGateway(), nil)
	result, err := worker.SimulateInteractionWithIntent(ctx, tenantID, roomID, effectiveQuestion, "", nil, intent, input.Nickname)
	if err != nil {
		writeError(w, http.StatusBadGateway, "answer generation failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"question": input.Text, "intent": intent, "reply": result.Reply,
		"execution_mode": result.ExecutionMode, "plan_name": result.PlanName,
	})
}
