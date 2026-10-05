package httpapi

import (
	"context"
	"errors"
	"net/http"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
)

type audioProgramRefreshClient interface {
	RefreshProgram(context.Context, audioout.RefreshProgramInput) (audioout.RoomProgramSnapshot, error)
	CancelProgramRefresh(context.Context, audioout.CancelProgramRefreshInput) (audioout.RoomProgramSnapshot, error)
	RenewProgramRefresh(context.Context, audioout.RenewProgramRefreshInput) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) renewRoomAudioProgramRefresh(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return
	}
	client, ok := state.client.(audioProgramRefreshClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "播音分发层不支持主线刷新续期")
		return
	}
	var input audioout.RenewProgramRefreshInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid refresh lease renewal")
		return
	}
	input.RoomID = roomID
	snapshot, err := client.RenewProgramRefresh(r.Context(), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, coreaudio.ErrProgramRefreshConflict) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) cancelRoomAudioProgramRefresh(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return
	}
	client, ok := state.client.(audioProgramRefreshClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "播音分发层不支持主线刷新撤回")
		return
	}
	var input audioout.CancelProgramRefreshInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid refresh cancellation")
		return
	}
	input.RoomID = roomID
	snapshot, err := client.CancelProgramRefresh(r.Context(), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, coreaudio.ErrProgramRefreshConflict) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) refreshRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return
	}
	client, ok := state.client.(audioProgramRefreshClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "播音分发层不支持安全主线刷新")
		return
	}
	var input audioout.RefreshProgramInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid refresh request")
		return
	}
	input.RoomID = roomID
	snapshot, err := client.RefreshProgram(r.Context(), input)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, coreaudio.ErrProgramRefreshConflict) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) getRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "播音分发层尚未配置")
		return
	}
	snapshot, err := state.client.ProgramSnapshot(r.Context(), roomID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
