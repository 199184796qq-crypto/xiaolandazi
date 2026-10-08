package httpapi

import (
	"context"
	"errors"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	"net/http"
)

type audioProgramAppendClient interface {
	AppendProgram(context.Context, audioout.AppendProgramInput) (audioout.RoomProgramSnapshot, error)
}

func (s *Server) appendRoomAudioProgram(w http.ResponseWriter, r *http.Request) {
	room, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenant, ok := requiredTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), &tenant, room); err != nil {
		writeError(w, 404, "room not found")
		return
	}
	state := s.audioDevState()
	if state == nil || state.client == nil || !state.client.Enabled() {
		writeError(w, 503, "播音分发层尚未配置")
		return
	}
	client, ok := state.client.(audioProgramAppendClient)
	if !ok {
		writeError(w, 503, "播音分发层不支持逐段追加")
		return
	}
	var input audioout.AppendProgramInput
	if readJSON(w, r, &input) != nil {
		writeError(w, 400, "invalid ready unit")
		return
	}
	input.RoomID = room
	result, err := client.AppendProgram(r.Context(), input)
	if err != nil {
		status := 400
		if errors.Is(err, coreaudio.ErrProgramRefreshConflict) {
			status = 409
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, 200, result)
}
