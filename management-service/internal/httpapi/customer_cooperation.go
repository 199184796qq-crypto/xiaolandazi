package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

type customerCooperationUpdateRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

func (s *Server) adminUpdateCustomerCooperation(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, access, ok := s.requireStaffPermission(
		w,
		r,
		"customer.cooperation.manage",
	)
	if !ok {
		return
	}

	tenantID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}
	userID, err := s.store.GetCustomerUserIDByTenantID(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "终端不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取终端失败")
		return
	}

	scope := staffBusinessScope(actor, access, "customer.cooperation.manage")
	visible, err := s.store.AdminCustomerVisibleToScope(
		r.Context(),
		scope,
		userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "校验终端范围失败")
		return
	}
	if !visible {
		writeError(w, http.StatusForbidden, "当前账号不能修改该终端合作状态")
		return
	}

	var input customerCooperationUpdateRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Status = strings.TrimSpace(input.Status)
	input.Note = strings.TrimSpace(input.Note)
	if input.Status != model.CustomerCooperationStatusCooperating &&
		input.Status != model.CustomerCooperationStatusNonCooperating {
		writeError(w, http.StatusBadRequest, "合作状态无效")
		return
	}
	if input.Status == model.CustomerCooperationStatusNonCooperating &&
		input.Note == "" {
		writeError(w, http.StatusBadRequest, "标记不合作时请填写原因")
		return
	}

	item, err := s.store.SetCustomerCooperationStatus(
		r.Context(),
		tenantID,
		input.Status,
		input.Note,
		actor.UserID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "终端不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "更新合作状态失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
