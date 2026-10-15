package httpapi

import (
	"errors"
	"net/http"

	appdb "livecompanion/management/internal/db"
)

func (s *Server) planBizDomainGet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, requestTenantID(r), false)
	if !ok {
		return
	}
	domain, err := s.store.GetPlanBizDomain(r.Context(), tenantID, planID)
	s.planBizDomainReply(w, domain, err)
}

func (s *Server) planBizDomainSet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID  int64  `json:"tenant_id,omitempty"`
		BizDomain string `json:"biz_domain"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "业务模式请求格式错误")
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	domain, err := s.store.SetPlanBizDomain(r.Context(), tenantID, planID, input.BizDomain)
	s.planBizDomainReply(w, domain, err)
}

func (s *Server) planBizDomainReply(w http.ResponseWriter, domain string, err error) {
	if errors.Is(err, appdb.ErrInvalidPlanBizDomain) {
		writeError(w, http.StatusBadRequest, "请选择电商带货或本地生活")
		return
	}
	if errors.Is(err, appdb.ErrLiveAgentPlanNotFound) {
		writeError(w, http.StatusNotFound, "智能体方案不存在或已归档")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "业务模式读取或保存失败，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"biz_domain": domain})
}
