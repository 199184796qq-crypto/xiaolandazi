package httpapi

import (
	"database/sql"
	"errors"
	"net/http"

	"livecompanion/core/internal/agentwork"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/roomaudio"
)

func (s *Server) updateRoomNumber(w http.ResponseWriter, r *http.Request) {
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
	var input roomstore.NumberUpdate
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// Resolve tenant ownership before inspecting any room-local runtime state.
	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "直播间不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取直播间失败")
		}
		return
	}
	// No implicit stop or reconnect: an active/paused paid program must finish first.
	if s.agentWork != nil {
		runtime := s.agentWork.Get(roomID)
		if runtime.State != agentwork.StateStopped || runtime.StopReason == agentwork.StopReasonManualPause {
			writeError(w, http.StatusConflict, roomstore.ErrRoomNumberBusy.Error())
			return
		}
	}
	if s.roomAudio.Snapshot(roomID).Phase != roomaudio.PhaseIdle {
		writeError(w, http.StatusConflict, roomstore.ErrRoomNumberBusy.Error())
		return
	}
	item, err := s.rooms.UpdateNumber(r.Context(), *tenantID, roomID, input)
	if err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "直播间不存在")
		case errors.Is(err, roomstore.ErrConflict):
			writeError(w, http.StatusConflict, "这个房间号已存在，请勿重复添加")
		case errors.Is(err, roomstore.ErrRoomNumberBusy), errors.Is(err, roomstore.ErrRoomNumberChanged):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, roomstore.ErrRoomNumberInvalid):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "修改房间号失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}
