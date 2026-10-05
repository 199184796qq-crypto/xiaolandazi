package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"

	"livecompanion/management/internal/model"
)

func (s *Server) roomCustomerContact(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() && !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅有直播运维权限的工作人员可查看客户联系方式")
		return
	}
	// Use the same permission gate as the operations room list, not customer directory access.
	scope, ok := s.roomScopeQuery(w, r, actor)
	if !ok {
		return
	}
	roomID, err := strconv.ParseInt(r.PathValue("roomID"), 10, 64)
	if err != nil || roomID <= 0 {
		writeError(w, http.StatusBadRequest, "无效的直播间 ID")
		return
	}
	// Internal accounts must resolve the actual room, not a tenant shortcut.
	roomActor := actor
	roomActor.TenantID = nil
	tenantID, ok := s.tenantForRoom(w, r, roomActor, roomID)
	if !ok {
		return
	}
	if selected := scope.Get("tenant_id"); selected != "" && selected != strconv.FormatInt(tenantID, 10) {
		writeError(w, http.StatusNotFound, "当前客户范围内没有该直播间")
		return
	}
	contact, err := s.store.GetRoomCustomerContact(r.Context(), tenantID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "客户不存在")
		} else {
			writeError(w, http.StatusServiceUnavailable, "读取客户联系方式失败")
		}
		return
	}
	// Audit the lookup, never the actual phone number.
	if err := s.audit.Record(r.Context(), model.AdminAuditLog{
		ActorUserID: actor.UserID, ActorUsername: actor.Username,
		Action: "liveops.customer_contact_view", TargetRoomID: roomID, TargetTenantID: tenantID,
		HTTPMethod: r.Method, Path: r.URL.Path, ClientIP: requestClientIP(r), Result: "http_200",
	}); err != nil {
		writeError(w, http.StatusServiceUnavailable, "审计日志服务暂不可用")
		return
	}
	writeJSON(w, http.StatusOK, contact)
}
