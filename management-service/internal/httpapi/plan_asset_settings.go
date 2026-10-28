package httpapi

import (
	"livecompanion/management/internal/model"
	"net/http"
)

func (s *Server) registerPlanAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/live-agent-plans/{planID}/content-mode", s.planAssetMode)
	mux.HandleFunc("PUT /api/v1/live-agent-plans/{planID}/content-mode", s.planAssetMode)
	mux.HandleFunc("GET /api/v1/live-agent-plans/{planID}/reading-rules", s.roomReadingRules)
	mux.HandleFunc("PUT /api/v1/live-agent-plans/{planID}/reading-rules", s.roomReadingRules)
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		mux.HandleFunc(method+" /api/v1/live-agent-plans/{planID}/question-library", s.roomQuestionLibrary)
	}
	mux.HandleFunc("POST /api/v1/live-agent-plans/{planID}/question-library/classify", s.classifyQuestionLibrary)
	mux.HandleFunc("GET /api/v1/live-agent-plans/{planID}/purposes", s.roomPlanPurposes)
}

// Explicit plan routes authorize the account asset. Legacy room routes resolve
// the selected plan once, then use exactly the same plan-owned storage.
func (s *Server) planAssetScope(w http.ResponseWriter, r *http.Request) (model.Actor, int64, int64, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	if r.PathValue("planID") != "" {
		plan, ok := liveAgentPlanPathID(w, r)
		if !ok {
			return actor, 0, 0, false
		}
		tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method != http.MethodGet)
		if !ok {
			return actor, 0, 0, false
		}
		if _, err := s.store.GetLiveAgentPlan(r.Context(), tenant, plan); err != nil {
			writeError(w, 404, "方案不存在")
			return actor, 0, 0, false
		}
		return actor, tenant, plan, true
	}
	room, ok := pathID(w, r)
	if !ok {
		return actor, 0, 0, false
	}
	tenant, ok := s.tenantForRoom(w, r, actor, room)
	if !ok {
		return actor, 0, 0, false
	}
	if !s.requireLiveStrategyRoomAccess(w, r, actor, tenant, room) {
		return actor, 0, 0, false
	}
	plan, err := s.store.GetLiveAgentPlanForRoom(r.Context(), tenant, room)
	if err != nil || plan.ID <= 0 {
		writeError(w, 404, "请先选择一个方案")
		return actor, 0, 0, false
	}
	return actor, tenant, plan.ID, true
}

func (s *Server) planAssetMode(w http.ResponseWriter, r *http.Request) {
	actor, tenant, plan, ok := s.planAssetScope(w, r)
	if !ok {
		return
	}
	settings, err := s.store.GetPlanAssetSettings(r.Context(), tenant, plan)
	if err == nil && r.Method == http.MethodPut {
		var input struct {
			Mode string `json:"content_mode"`
		}
		if readJSON(w, r, &input) != nil {
			writeError(w, 400, "方案模式格式错误")
			return
		}
		settings, err = s.store.SetPlanAssetMode(r.Context(), tenant, plan, actor.UserID, input.Mode)
	}
	if err != nil {
		writeError(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, model.ResolveLiveContentMode(settings.ContentMode, true, "plan"))
}
