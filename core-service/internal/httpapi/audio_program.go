package httpapi

import (
	"context"
	"net/http"
	"strings"

	"livecompanion/core/internal/audioout"
)

type audioProgramClient interface {
	StartProgram(context.Context, audioout.StartProgramInput) (audioout.RoomProgramSnapshot, error)
	PauseProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	ResumeProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	StopProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) startRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
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
	client, ok := state.client.(audioProgramClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "播音分发层不支持正式主线节目")
		return
	}
	var input struct {
		SessionID string                  `json:"session_id"`
		Label     string                  `json:"label"`
		VersionID int64                   `json:"version_id"`
		VersionNo int64                   `json:"version_no"`
		Tracks    []audioout.ProgramTrack `json:"tracks"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Tracks) == 0 {
		writeError(w, http.StatusBadRequest, "program tracks are required")
		return
	}
	snapshot, err := client.StartProgram(r.Context(), audioout.StartProgramInput{
		RoomID:    roomID,
		SessionID: strings.TrimSpace(input.SessionID),
		Label:     strings.TrimSpace(input.Label),
		VersionID: input.VersionID,
		VersionNo: input.VersionNo,
		Tracks:    input.Tracks,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "启动正式主线声音失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) controlRoomAudioProgram(w http.ResponseWriter, r *http.Request, action string) {
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
	client, ok := state.client.(audioProgramClient)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "播音分发层不支持正式主线节目控制")
		return
	}
	var (
		snapshot audioout.RoomProgramSnapshot
		err      error
	)
	switch action {
	case "pause":
		snapshot, err = client.PauseProgram(r.Context(), roomID)
	case "resume":
		snapshot, err = client.ResumeProgram(r.Context(), roomID)
	case "stop":
		snapshot, err = client.StopProgram(r.Context(), roomID)
	default:
		writeError(w, http.StatusBadRequest, "unsupported program action")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) pauseRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	s.controlRoomAudioProgram(w, r, "pause")
}

func (s *Server) resumeRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	s.controlRoomAudioProgram(w, r, "resume")
}

func (s *Server) stopRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	s.controlRoomAudioProgram(w, r, "stop")
}
