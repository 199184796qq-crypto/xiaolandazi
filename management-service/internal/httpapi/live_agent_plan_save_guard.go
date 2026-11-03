package httpapi

import (
	"errors"
	appdb "livecompanion/management/internal/db"
	"net/http"
)

func writePlanSaveGuardError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanInUse) {
		writeError(w, http.StatusConflict, err.Error())
	} else {
		writeError(w, http.StatusServiceUnavailable, "暂时无法确认方案是否正在直播，本次未保存，请稍后重试。")
	}
	return true
}
func (s *Server) allowPlanSave(w http.ResponseWriter, r *http.Request, tenant, plan int64) bool {
	return !writePlanSaveGuardError(w, s.store.AssertLiveAgentPlanEditable(r.Context(), tenant, plan))
}
func (s *Server) liveAgentPlanSaveCheck(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), true)
	if !ok {
		return
	}
	if !s.allowPlanSave(w, r, tenant, plan) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"editable": true})
}
