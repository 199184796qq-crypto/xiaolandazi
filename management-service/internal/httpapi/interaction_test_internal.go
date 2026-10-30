package httpapi

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/decisionexecutor"
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
		PlanID      int64  `json:"plan_id"`
		UserID      string `json:"user_id"`
		Nickname    string `json:"nickname"`
		Text        string `json:"text"`
		MissionKind string `json:"mission_kind"`
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
	if strings.EqualFold(strings.TrimSpace(input.MissionKind), "time_report") {
		worker := decisionexecutor.New(&planSpeechTestStore{Store: s.store, tenantID: tenantID, roomID: roomID, plan: plan}, nil, s.speechGateway(), nil)
		result, reportErr := worker.SimulateTimeReport(ctx, tenantID, roomID, input.Text, input.Nickname)
		if reportErr != nil {
			writeError(w, http.StatusBadGateway, "time report generation failed: "+reportErr.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"question": input.Text, "reply": result.Reply,
			"execution_mode": "time_report", "plan_name": plan.Name,
		})
		return
	}
	s.testInteractionV2(w, r, interactionV2Input{Tenant: tenantID, Room: roomID, Plan: input.PlanID, UserID: input.UserID, Nickname: input.Nickname, Text: input.Text, Kind: "semantic", Test: true, Protocol: "interaction_v2", Task: fmt.Sprintf("test-%d", time.Now().UnixNano()), Epoch: "test", Revision: 1, AllowClarify: true, ExpiresAt: time.Now().Add(50 * time.Second)}, plan.Name)
}
