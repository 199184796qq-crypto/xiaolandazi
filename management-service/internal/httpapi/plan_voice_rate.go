package httpapi

import (
	"errors"
	"net/http"

	appdb "livecompanion/management/internal/db"
)

func (s *Server) liveAgentPlanVoiceRate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), r.Method == http.MethodPut)
	if !ok {
		return
	}
	result, err := s.store.GetPlanVoiceRate(r.Context(), tenant, plan)
	if r.Method == http.MethodPut && err == nil {
		var input struct {
			Rate float64 `json:"rate"`
		}
		if readJSON(w, r, &input) != nil {
			writeError(w, http.StatusBadRequest, "语速参数格式错误")
			return
		}
		result, err = s.store.SetPlanVoiceRate(r.Context(), tenant, plan, input.Rate)
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "直播方案不存在")
		return
	}
	if errors.Is(err, appdb.ErrInvalidPlanVoiceRate) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取或保存方案语速失败，请重试")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
