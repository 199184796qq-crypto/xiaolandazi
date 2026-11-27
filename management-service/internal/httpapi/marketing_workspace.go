package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"livecompanion/management/internal/db"
)

func (s *Server) commercialMarketingWorkspace(w http.ResponseWriter, r *http.Request) {
	_, access, ok := s.requireStaffPermission(w, r, "commercial.marketing.view")
	if !ok {
		return
	}
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "month"
	}
	start, end, ok := workspaceTimeCardPeriod(time.Now(), period)
	if !ok {
		writeError(w, http.StatusBadRequest, "参与统计时段无效")
		return
	}
	items, err := s.store.ListMarketingWorkspace(r.Context(), start, end)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取营销计划和参与统计失败")
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "can_manage": staffHasPermission(access, "commercial.marketing.manage"),
	})
}

func (s *Server) commercialSortMarketingCampaigns(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	var input struct {
		IDs      []int64 `json:"ids"`
		Previous []int64 `json:"previous"`
	}
	if err := readJSON(w, r, &input); err != nil || len(input.IDs) < 2 || len(input.IDs) != len(input.Previous) {
		writeError(w, http.StatusBadRequest, "营销计划排序格式有误")
		return
	}
	err := s.store.ReorderWorkspaceMarketingCampaigns(r.Context(), actor.UserID, input.IDs, input.Previous)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrMarketingCampaignOrderChanged):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "保存营销计划排序失败")
	}
}

func (s *Server) commercialSetMarketingCampaignStock(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	id, ok := marketingCampaignPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		Limit    *uint32 `json:"stock_limit"`
		Previous *uint32 `json:"previous"`
	}
	if err := readJSONWithLimit(w, r, &input, 4096); err != nil || input.Limit == nil || input.Previous == nil {
		writeError(w, http.StatusBadRequest, "活动库存请求无效")
		return
	}
	if err := s.store.UpdateWorkspaceMarketingStock(r.Context(), id, actor.UserID, *input.Limit, *input.Previous); err != nil {
		if errors.Is(err, db.ErrMarketingCampaignStockBelowUsage) || errors.Is(err, db.ErrMarketingCampaignStockChanged) {
			writeError(w, http.StatusConflict, err.Error())
		} else {
			writeMarketingWorkspaceStateError(w, err)
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) commercialSetMarketingCampaignStatus(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.marketing.manage")
	if !ok {
		return
	}
	id, ok := marketingCampaignPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := readJSONWithLimit(w, r, &input, 4096); err != nil {
		writeError(w, http.StatusBadRequest, "活动状态请求无效")
		return
	}
	switch input.Status {
	case "active", "inactive", "archived":
	default:
		writeError(w, http.StatusBadRequest, "活动状态不支持")
		return
	}
	if err := s.store.SetMarketingCampaignStatus(r.Context(), id, actor.UserID, input.Status); err != nil {
		writeMarketingWorkspaceStateError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeMarketingWorkspaceStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "营销活动不存在")
	case errors.Is(err, db.ErrMarketingCampaignUnavailable):
		writeError(w, http.StatusConflict, "请先修改已过期的活动时间")
	case errors.Is(err, db.ErrMarketingCampaignStockInsufficient):
		writeError(w, http.StatusConflict, "活动库存已用完，请先调整库存")
	case errors.Is(err, db.ErrMarketingCampaignArchived):
		writeError(w, http.StatusConflict, "该活动已归档")
	case errors.Is(err, db.ErrMarketingCampaignMustUnlist):
		writeError(w, http.StatusConflict, "请先下架活动")
	case errors.Is(err, db.ErrMarketingCampaignArchivePaymentUnconfirmed):
		writeError(w, http.StatusConflict, "有订单的支付结果尚未确认，请先核对支付或收款凭据，再重试归档")
	case errors.Is(err, db.ErrMarketingCampaignPendingOrders):
		writeError(w, http.StatusConflict, "活动有未处理的订单占用，请核对订单后重试归档")
	default:
		writeError(w, http.StatusInternalServerError, "更新营销活动失败")
	}
}
