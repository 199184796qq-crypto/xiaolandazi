package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

type afterSalesRequestInput struct {
	SN           string `json:"sn"`
	ServiceType  string `json:"service_type"`
	CustomerName string `json:"customer_name"`
	ContactPhone string `json:"contact_phone"`
	Issue        string `json:"issue"`
}

func (s *Server) afterSalesListRequests(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAfterSalesPortalActor(w, r)
	if !ok {
		return
	}
	page := 1
	pageSize := 20
	if value := strings.TrimSpace(r.URL.Query().Get("page")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if value := strings.TrimSpace(r.URL.Query().Get("page_size")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}
	if pageSize > 100 {
		pageSize = 100
	}

	items, total, err := s.store.ListPortalRMAs(r.Context(), actor, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取售后维修单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

func (s *Server) afterSalesCreateRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAfterSalesPortalActor(w, r)
	if !ok {
		return
	}
	var input afterSalesRequestInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.SN = strings.TrimSpace(input.SN)
	input.ServiceType = strings.ToLower(strings.TrimSpace(input.ServiceType))
	input.CustomerName = strings.TrimSpace(input.CustomerName)
	input.ContactPhone = strings.TrimSpace(input.ContactPhone)
	input.Issue = strings.TrimSpace(input.Issue)
	if input.SN == "" {
		writeError(w, http.StatusBadRequest, "必须填写设备 SN")
		return
	}
	switch input.ServiceType {
	case "repair", "exchange", "return":
	default:
		writeError(w, http.StatusBadRequest, "售后类型仅支持维修、换机或退货")
		return
	}
	if input.Issue == "" {
		writeError(w, http.StatusBadRequest, "请填写故障或问题描述")
		return
	}
	if input.CustomerName == "" {
		input.CustomerName = strings.TrimSpace(actor.DisplayName)
	}
	if input.ContactPhone == "" {
		input.ContactPhone = strings.TrimSpace(actor.Phone)
	}

	device, sourceTenantID, sourceAgentOrgID, err := s.store.ResolveAfterSalesDevice(
		r.Context(),
		actor,
		input.SN,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "未找到属于当前账号范围的设备，请核对 SN")
			return
		}
		writeError(w, http.StatusInternalServerError, "校验设备归属失败")
		return
	}

	sourceType := "customer"
	if actor.IsAgentAdmin() {
		sourceType = "agent"
	}
	sourceUserID := actor.UserID
	item, err := s.store.CreateRMA(r.Context(), actor.UserID, model.CreateRMAInput{
		DeviceID:         device.ID,
		ServiceType:      input.ServiceType,
		SourceType:       sourceType,
		SourceUserID:     &sourceUserID,
		SourceTenantID:   sourceTenantID,
		SourceAgentOrgID: sourceAgentOrgID,
		CustomerName:     input.CustomerName,
		ContactPhone:     input.ContactPhone,
		Issue:            input.Issue,
	})
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "already has active RMA"):
			writeError(w, http.StatusConflict, "该设备已有未完成的售后维修单")
		case strings.Contains(err.Error(), "cannot open RMA"):
			writeError(w, http.StatusConflict, "当前设备状态不能发起售后")
		default:
			writeError(w, http.StatusBadRequest, "提交售后维修单失败："+inventorySafeError(err))
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) afterSalesRequestEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAfterSalesPortalActor(w, r)
	if !ok {
		return
	}
	rmaID, ok := inventoryPathID(w, r, "rmaID")
	if !ok {
		return
	}
	allowed, err := s.store.ActorCanAccessRMA(r.Context(), actor, rmaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验售后维修单范围失败")
		return
	}
	if !allowed {
		writeError(w, http.StatusNotFound, "售后维修单不存在")
		return
	}
	items, err := s.store.ListRMAEvents(r.Context(), rmaID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取维修进度失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) requireAfterSalesPortalActor(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if actor.TenantID == nil || (actor.Role != "customer" && !actor.IsAgentAdmin()) {
		writeError(w, http.StatusForbidden, "仅终端或代理账号可使用售后维修入口")
		return model.Actor{}, false
	}
	return actor, true
}
