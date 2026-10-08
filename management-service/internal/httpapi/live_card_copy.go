package httpapi

import (
	"errors"
	"net/http"
	"strings"

	appdb "livecompanion/management/internal/db"
)

type liveCardCopyInput struct {
	TenantID          int64 `json:"tenant_id,omitempty"`
	ExpectedVersionNo int64 `json:"expected_version_no"`
}

func (s *Server) liveAgentPlanProductLinkCopy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	id, ok := liveAgentPlanProductLinkPathID(w, r)
	if !ok {
		return
	}
	var input liveCardCopyInput
	if readJSON(w, r, &input) != nil || input.ExpectedVersionNo <= 0 {
		writeError(w, http.StatusBadRequest, "复制需要当前商品版本，请刷新重试")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.CopyLiveAgentPlanProductLink(r.Context(), tenant, plan, id, actor.UserID, input.ExpectedVersionNo)
	if writeLiveCardCopyError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func writeLiveCardCopyError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, appdb.ErrLiveAgentPlanNotFound), errors.Is(err, appdb.ErrLiveAgentPlanProductLinkNotFound), errors.Is(err, appdb.ErrLiveAgentPlanBenefitNotFound):
		writeError(w, http.StatusNotFound, "原卡片或当前方案不存在，请刷新")
	case errors.Is(err, appdb.ErrLiveAgentPlanProductLinkVersionConflict), errors.Is(err, appdb.ErrLiveAgentPlanBenefitVersionConflict):
		writeError(w, http.StatusConflict, "原卡片已更新，请刷新后再复制")
	case errors.Is(err, appdb.ErrLiveCardCopyInvalid):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), appdb.ErrLiveCardCopyInvalid.Error()+": "))
	default:
		writeError(w, http.StatusInternalServerError, "复制卡片失败，请刷新确认后重试")
	}
	return true
}

func (s *Server) liveAgentPlanBenefitCopy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	plan, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	id, ok := liveAnchorStylePathID(w, r, "benefitID", "福利")
	if !ok {
		return
	}
	var input liveCardCopyInput
	if readJSON(w, r, &input) != nil || input.ExpectedVersionNo <= 0 {
		writeError(w, http.StatusBadRequest, "复制需要当前福利版本，请刷新重试")
		return
	}
	tenant, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, true)
	if !ok {
		return
	}
	item, err := s.store.CopyLiveAgentPlanBenefit(r.Context(), tenant, plan, id, actor.UserID, input.ExpectedVersionNo)
	if writeLiveCardCopyError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
