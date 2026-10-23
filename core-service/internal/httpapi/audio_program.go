package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
)

type audioProgramClient interface {
	StartProgram(context.Context, audioout.StartProgramInput) (audioout.RoomProgramSnapshot, error)
	PauseProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	ResumeProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
	StopProgram(context.Context, int64) (audioout.RoomProgramSnapshot, error)
}

type guardedAudioProgramResumer interface {
	ResumeProgramIfCurrent(context.Context, int64, audioout.ProgramResumeGuard, func() bool) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) resumeRoomAudioProgramChecked(w http.ResponseWriter, r *http.Request, roomID int64, client audioProgramClient) (audioout.RoomProgramSnapshot, error) {
	var input struct {
		ProgramID       *string `json:"expected_program_id"`
		VersionID       *int64  `json:"expected_version_id"`
		ControlRevision *uint64 `json:"expected_control_revision"`
		BootID          *string `json:"expected_boot_id"`
		RunGeneration   *uint64 `json:"expected_run_generation"`
	}
	if err := readJSON(w, r, &input); err != nil && !errors.Is(err, io.EOF) {
		return audioout.RoomProgramSnapshot{}, coreaudio.ErrProgramResumeBaselineConflict
	}
	if input.ProgramID == nil && input.VersionID == nil && input.ControlRevision == nil && input.BootID == nil && input.RunGeneration == nil {
		return client.ResumeProgram(r.Context(), roomID)
	}
	if input.ProgramID == nil || input.VersionID == nil || input.ControlRevision == nil || input.BootID == nil || input.RunGeneration == nil || strings.TrimSpace(*input.ProgramID) == "" || *input.VersionID <= 0 || strings.TrimSpace(*input.BootID) == "" || *input.RunGeneration == 0 {
		return audioout.RoomProgramSnapshot{}, coreaudio.ErrProgramResumeBaselineConflict
	}
	guard := audioout.ProgramResumeGuard{ExpectedProgramID: strings.TrimSpace(*input.ProgramID), ExpectedVersionID: *input.VersionID, ExpectedControlRevision: *input.ControlRevision, ExpectedBootID: strings.TrimSpace(*input.BootID), ExpectedRunGeneration: *input.RunGeneration}
	allowed := func() bool {
		if s.agentWork == nil {
			return false
		}
		current := s.agentWork.Get(roomID)
		return current.State == agentwork.StateWorking && current.BootID == guard.ExpectedBootID && current.RunGeneration == guard.ExpectedRunGeneration
	}
	guarded, ok := client.(guardedAudioProgramResumer)
	if !ok || !allowed() {
		return audioout.RoomProgramSnapshot{}, coreaudio.ErrProgramResumeBaselineConflict
	}
	return guarded.ResumeProgramIfCurrent(r.Context(), roomID, guard, allowed)
}

func (s *Server) startRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	defer recoverRoomAudioDelivery(w)
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
		Sequential bool                    `json:"sequential"`
		SessionID  string                  `json:"session_id"`
		Label      string                  `json:"label"`
		VersionID  int64                   `json:"version_id"`
		VersionNo  int64                   `json:"version_no"`
		Tracks     []audioout.ProgramTrack `json:"tracks"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(input.Tracks) == 0 {
		writeError(w, http.StatusBadRequest, "program tracks are required")
		return
	}
	if !s.formalAudioStartAllowed(roomID, input.VersionID) {
		writeJSON(w, http.StatusConflict, audioDeliveryError{Error: "本场已暂停、停止或被新的操作替代，取消旧声音启动。", Code: "audio_start_cancelled", Stage: "场次状态校验", Retryable: false})
		return
	}
	snapshot, err := client.StartProgram(r.Context(), audioout.StartProgramInput{
		Sequential: input.Sequential,
		RoomID:     roomID,
		SessionID:  strings.TrimSpace(input.SessionID),
		Label:      strings.TrimSpace(input.Label),
		VersionID:  input.VersionID,
		VersionNo:  input.VersionNo,
		Tracks:     input.Tracks,
	})
	if err != nil {
		if errors.Is(err, coreaudio.ErrProgramUnitNotAccepted) {
			writeJSON(w, http.StatusBadRequest, audioDeliveryError{Error: "本段声音未通过播放准备校验，可继续尝试其它已就绪内容。", Code: "audio_unit_not_accepted", Stage: "声音单元校验", Retryable: false})
			return
		}
		if errors.Is(err, coreaudio.ErrProgramStartCancelled) || errors.Is(err, coreaudio.ErrProgramRefreshConflict) {
			writeJSON(w, http.StatusConflict, audioDeliveryError{Error: "本次播放启动已取消或被新的操作替代。", Code: "audio_start_cancelled", Stage: "播放状态校验", Retryable: false})
			return
		}
		writeError(w, http.StatusBadGateway, "启动正式主线声音失败: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) formalAudioStartAllowed(roomID, versionID int64) bool {
	// Versionless developer tone tests do not consume a paid live lifecycle.
	return versionID <= 0 || (s.agentWork != nil && s.agentWork.Get(roomID).State == agentwork.StateWorking)
}

func (s *Server) controlRoomAudioProgram(w http.ResponseWriter, r *http.Request, action string) {
	defer recoverRoomAudioDelivery(w)
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
		snapshot, err = s.resumeRoomAudioProgramChecked(w, r, roomID, client)
	case "stop":
		snapshot, err = client.StopProgram(r.Context(), roomID)
	default:
		writeError(w, http.StatusBadRequest, "unsupported program action")
		return
	}
	if err != nil {
		if errors.Is(err, coreaudio.ErrProgramResumeBaselineConflict) {
			writeJSON(w, http.StatusConflict, audioDeliveryError{Error: "本次声音恢复已被新的暂停、停止或场次替代。", Code: "audio_resume_baseline_conflict", Stage: "播放恢复状态校验", Retryable: false})
			return
		}
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
