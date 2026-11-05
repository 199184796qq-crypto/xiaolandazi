package httpapi

import (
	"database/sql"
	"errors"
	roomstore "livecompanion/core/internal/room"
	"net/http"
)

func (s *Server) updateRoomName(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}
	if tenantID == nil || *tenantID <= 0 {
		writeError(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	var input roomstore.NameUpdate
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	name, err := s.rooms.UpdateName(r.Context(), *tenantID, roomID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "直播间不存在")
		case errors.Is(err, roomstore.ErrRoomNameChanged), errors.Is(err, roomstore.ErrCollectorReplaced):
			writeError(w, http.StatusConflict, "直播间已发生变化，请刷新后重试")
		case errors.Is(err, roomstore.ErrRoomNameInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "修改直播间名称失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": roomID, "name": name})
}
