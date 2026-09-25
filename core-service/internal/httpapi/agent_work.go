package httpapi

import (
	"context"
	"log"
	"net/http"

	"livecompanion/core/internal/agentwork"
)

func (s *Server) SetAgentWorkRegistry(registry *agentwork.Registry) {
	if registry == nil {
		registry = agentwork.New()
	}
	s.agentWork = registry
}

func (s *Server) ensureAgentMode(ctx context.Context, roomID int64) {
	if roomID <= 0 || s.agentWork == nil || s.agentWork.Has(roomID) {
		return
	}
	mode := agentwork.ModeControl
	if s.events != nil {
		if persisted, err := s.events.GetAgentMode(ctx, roomID); err == nil {
			mode = agentwork.Mode(persisted)
		}
	}
	_, _ = s.agentWork.SetMode(roomID, mode)
}

func (s *Server) getRoomAgentWork(w http.ResponseWriter, r *http.Request) {
	roomID, ok := pathID(w, r, "roomID")
	if !ok {
		return
	}
	if s.agentWork == nil {
		writeError(w, http.StatusServiceUnavailable, "agent work registry is not configured")
		return
	}
	s.ensureAgentMode(r.Context(), roomID)
	writeJSON(w, http.StatusOK, s.agentWork.Get(roomID))
}

func (s *Server) updateRoomAgentWork(w http.ResponseWriter, r *http.Request) {
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
	if s.agentWork == nil {
		writeError(w, http.StatusServiceUnavailable, "agent work registry is not configured")
		return
	}
	s.ensureAgentMode(r.Context(), roomID)
	var input struct {
		State              agentwork.State `json:"state"`
		Mode               agentwork.Mode  `json:"mode"`
		BaseWorkingSeconds uint64          `json:"base_working_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.State == "" && input.Mode == "" {
		writeError(w, http.StatusBadRequest, "state or mode is required")
		return
	}
	snapshot := s.agentWork.Get(roomID)
	var err error
	if input.Mode != "" {
		if s.events != nil {
			if persistErr := s.events.SetAgentMode(r.Context(), roomID, string(input.Mode)); persistErr != nil {
				writeError(w, http.StatusServiceUnavailable, "persist agent mode failed")
				return
			}
		}
		snapshot, err = s.agentWork.SetMode(roomID, input.Mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	stateUpdated := input.State != ""
	if stateUpdated {
		snapshot, err = s.agentWork.SetWithBase(roomID, input.State, input.BaseWorkingSeconds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if stateUpdated && snapshot.State == agentwork.StateStopped {
		// Stopping paid AI must not touch the free base pipeline.
		// Collection, question clustering and public-screen data keep running.
		if s.agentDecisions != nil {
			s.agentDecisions.ClearRoom(roomID)
		}
		if s.speechRuntime != nil {
			s.speechRuntime.Reset(roomID)
		}
		if err := s.stopRoomAudio(r.Context(), roomID); err != nil {
			log.Printf("stop paid audio output room=%d: %v", roomID, err)
		}
	}
	writeJSON(w, http.StatusOK, snapshot)
}
