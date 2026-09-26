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

func (s *Server) ensureAgentRuntimeConfig(ctx context.Context, roomID int64) {
	if roomID <= 0 || s.agentWork == nil {
		return
	}
	if !s.agentWork.Has(roomID) {
		mode := agentwork.ModeControl
		if s.events != nil {
			if persisted, err := s.events.GetAgentMode(ctx, roomID); err == nil {
				mode = agentwork.Mode(persisted)
			}
		}
		_, _ = s.agentWork.SetMode(roomID, mode)
	}
	if s.events != nil {
		if plan, err := s.events.GetAgentPlan(ctx, roomID); err == nil {
			_, _ = s.agentWork.SetPlan(roomID, plan.ID, plan.Name)
		}
	}
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
	s.ensureAgentRuntimeConfig(r.Context(), roomID)
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
	s.ensureAgentRuntimeConfig(r.Context(), roomID)
	var input struct {
		State              agentwork.State `json:"state"`
		Mode               agentwork.Mode  `json:"mode"`
		PlanID             *int64          `json:"plan_id"`
		PlanName           string          `json:"plan_name"`
		BaseWorkingSeconds uint64          `json:"base_working_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.State == "" && input.Mode == "" && input.PlanID == nil {
		writeError(w, http.StatusBadRequest, "state, mode or plan_id is required")
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
	if input.PlanID != nil {
		if *input.PlanID < 0 {
			writeError(w, http.StatusBadRequest, "plan_id must not be negative")
			return
		}
		if s.events != nil {
			if persistErr := s.events.SetAgentPlan(r.Context(), roomID, *input.PlanID, input.PlanName); persistErr != nil {
				writeError(w, http.StatusServiceUnavailable, "persist agent plan failed")
				return
			}
		}
		snapshot, err = s.agentWork.SetPlan(roomID, *input.PlanID, input.PlanName)
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
