package httpapi

import (
	"net/http"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/speechruntime"
)

type roomSpeechRuntimeResponse struct {
	speechruntime.Snapshot
	Program *audioout.RoomProgramSnapshot `json:"program,omitempty"`
}

func (s *Server) getRoomSpeechRuntime(w http.ResponseWriter, r *http.Request) {
	if s.speechRuntime == nil {
		writeError(w, http.StatusServiceUnavailable, "speech runtime is not configured")
		return
	}
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	tenantID, ok := optionalTenantID(w, r)
	if !ok {
		return
	}
	if _, err := s.rooms.Get(r.Context(), tenantID, roomID); err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	snapshot, err := s.speechRuntime.Snapshot(roomID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	response := roomSpeechRuntimeResponse{Snapshot: snapshot}
	if audioState := s.audioDevState(); audioState != nil && audioState.client != nil && audioState.client.Enabled() {
		if program, programErr := audioState.client.ProgramSnapshot(r.Context(), roomID); programErr == nil {
			response.Program = &program
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) updateRoomSpeechRuntime(w http.ResponseWriter, r *http.Request) {
	if s.speechRuntime == nil {
		writeError(w, http.StatusServiceUnavailable, "speech runtime is not configured")
		return
	}
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
	var input speechruntime.UpdateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	snapshot, err := s.speechRuntime.Update(roomID, input)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}
