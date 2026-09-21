package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

type adminResetPasswordRequest struct {
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

type coreRoomListResponse struct {
	Items []struct {
		ID int64 `json:"id"`
	} `json:"items"`
}

func (s *Server) requirePlatformAdmin(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅平台管理员可执行此操作")
		return model.Actor{}, false
	}
	return actor, true
}

func (s *Server) adminListCustomers(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, ok := s.requirePlatformAdmin(w, r)
	if !ok {
		return
	}

	if err := s.audit.Record(r.Context(), model.AdminAuditLog{
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		Action:        "customer.list_view",
		HTTPMethod:    r.Method,
		Path:          r.URL.Path,
		ClientIP:      requestClientIP(r),
		Result:        "http_200",
	}); err != nil {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"审计日志服务暂不可用",
		)
		return
	}

	items, err := s.store.ListAdminCustomers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func (s *Server) adminResetCustomerPassword(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := s.requirePlatformAdmin(w, r); !ok {
		return
	}

	userID, ok := adminUserID(w, r)
	if !ok {
		return
	}

	var input adminResetPasswordRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if input.NewPassword != input.ConfirmPassword {
		writeError(w, http.StatusBadRequest, "两次输入的新密码不一致")
		return
	}

	passwordHash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		if errors.Is(err, auth.ErrWeakPassword) {
			writeError(w, http.StatusBadRequest, "新密码长度需为 8-72 位")
			return
		}
		writeError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	customer, err := s.store.AdminResetCustomerPassword(
		r.Context(),
		userID,
		passwordHash,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "客户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "重置客户密码失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"customer": customer,
	})
}

func (s *Server) adminDeleteCustomer(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, ok := s.requirePlatformAdmin(w, r); !ok {
		return
	}

	userID, ok := adminUserID(w, r)
	if !ok {
		return
	}

	customer, err := s.store.GetAdminCustomer(r.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "客户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取客户失败")
		return
	}

	if err := s.deleteTenantCoreRooms(
		r,
		customer.TenantID,
	); err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"删除客户失败：核心直播间清理未完成",
		)
		return
	}

	if _, err := s.store.DeleteAdminCustomer(
		r.Context(),
		userID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "客户不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "删除客户失败")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) adminListAuditLogs(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, ok := s.requirePlatformAdmin(w, r)
	if !ok {
		return
	}

	if err := s.audit.Record(r.Context(), model.AdminAuditLog{
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		Action:        "audit.list_view",
		HTTPMethod:    r.Method,
		Path:          r.URL.Path,
		ClientIP:      requestClientIP(r),
		Result:        "http_200",
	}); err != nil {
		writeError(
			w,
			http.StatusServiceUnavailable,
			"审计日志服务暂不可用",
		)
		return
	}

	limit := int64(50)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			limit = parsed
		}
	}

	items, err := s.audit.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取审计日志失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
	})
}

func adminUserID(
	w http.ResponseWriter,
	r *http.Request,
) (int64, bool) {
	value, err := strconv.ParseInt(
		r.PathValue("userID"),
		10,
		64,
	)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "客户ID无效")
		return 0, false
	}
	return value, true
}

func (s *Server) deleteTenantCoreRooms(
	r *http.Request,
	tenantID int64,
) error {
	query := url.Values{}
	query.Set("tenant_id", strconv.FormatInt(tenantID, 10))

	resp, err := s.core.Do(
		r.Context(),
		http.MethodGet,
		"/internal/v1/rooms",
		query,
		nil,
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf(
			"list core rooms status=%d body=%s",
			resp.StatusCode,
			string(payload),
		)
	}

	var list coreRoomListResponse
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return fmt.Errorf("decode core rooms: %w", err)
	}

	for _, room := range list.Items {
		deleteResp, err := s.core.Do(
			r.Context(),
			http.MethodDelete,
			fmt.Sprintf("/internal/v1/rooms/%d", room.ID),
			query,
			nil,
		)
		if err != nil {
			return err
		}

		status := deleteResp.StatusCode
		payload, _ := io.ReadAll(
			io.LimitReader(deleteResp.Body, 2048),
		)
		deleteResp.Body.Close()

		if status < 200 || status >= 300 {
			return fmt.Errorf(
				"delete core room %d status=%d body=%s",
				room.ID,
				status,
				string(payload),
			)
		}
	}
	return nil
}
