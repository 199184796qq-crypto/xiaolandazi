package httpapi

import (
	"errors"
	"net/http"

	"livecompanion/management/internal/db"
)

func (s *Server) liveAgentPlanRuntimePromptPut(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, true)
	if !ok {
		return
	}
	var body struct {
		ExpectedRevision string  `json:"expected_revision"`
		Speech           *string `json:"speech_requirements_md"`
		Interaction      *string `json:"interaction_requirements_md"`
		Planning         *string `json:"rewrite_planning_md"`
	}
	if readJSON(w, r, &body) != nil || body.Speech == nil || body.Interaction == nil || body.Planning == nil {
		writeError(w, http.StatusBadRequest, "三项提示词保存格式错误")
		return
	}
	input := db.RuntimePromptUpdate{ExpectedRevision: body.ExpectedRevision, SpeechRequirementsMD: *body.Speech, InteractionRequirementsMD: *body.Interaction, RewritePlanningMD: *body.Planning}
	doc, err := s.store.SaveLiveAgentPlanRuntimePrompts(r.Context(), tenant, plan, actor.UserID, input)
	if err != nil {
		status, message := http.StatusInternalServerError, "三项提示词保存失败，草稿已保留，请稍后重试"
		if errors.Is(err, db.ErrPlanPromptChanged) || errors.Is(err, db.ErrPlanPromptHotUnavailable) {
			status = http.StatusConflict
			message = err.Error()
		} else if errors.Is(err, db.ErrLiveAgentPlanNotFound) {
			status, message = http.StatusNotFound, "方案不存在"
		} else if errors.Is(err, db.ErrRuntimePromptInvalid) {
			status, message = http.StatusBadRequest, err.Error()
		}
		writeError(w, status, message)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
