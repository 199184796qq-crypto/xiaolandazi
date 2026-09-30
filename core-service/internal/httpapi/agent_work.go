package httpapi

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"livecompanion/core/internal/agentwork"
)

func (s *Server) SetAgentWorkRegistry(registry *agentwork.Registry) {
	if registry == nil {
		registry = agentwork.New()
	}
	s.agentWork = registry
}

func (s *Server) cleanupPaidAgentRuntime(ctx context.Context, roomID int64, reason agentwork.StopReason) {
	if roomID <= 0 {
		return
	}
	if s.agentDecisions != nil {
		s.agentDecisions.ClearRoom(roomID)
	}
	if s.speechRuntime != nil {
		s.speechRuntime.Reset(roomID)
	}
	if reason != agentwork.StopReasonManualPause {
		if err := s.stopRoomAudio(ctx, roomID); err != nil {
			log.Printf("stop paid audio output room=%d: %v", roomID, err)
		}
	}
}

// RunAgentRuntimeWatch keeps room-local execution cleanup inside Core after an
// explicit lifecycle stop. Account quota is owned by Management; Core never
// turns a transient control-plane delay into a room stop by itself.
func (s *Server) RunAgentRuntimeWatch(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	last := make(map[int64]agentwork.State)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.agentWork == nil {
				continue
			}
			for _, snapshot := range s.agentWork.Snapshots() {
				previous, known := last[snapshot.RoomID]
				last[snapshot.RoomID] = snapshot.State
				if !known || previous != snapshot.State {
					log.Printf(
						"agent runtime transition room=%d previous=%q state=%q reason=%q working_seconds=%d",
						snapshot.RoomID,
						previous,
						snapshot.State,
						snapshot.StopReason,
						snapshot.WorkingSeconds,
					)
				}
				if snapshot.State != agentwork.StateStopped || snapshot.StopReason == "" {
					continue
				}
				if !known || previous != agentwork.StateStopped {
					s.cleanupPaidAgentRuntime(ctx, snapshot.RoomID, snapshot.StopReason)
				}
			}
		}
	}
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
	room, err := s.rooms.Get(r.Context(), &tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusNotFound, "room not found")
		return
	}
	if s.agentWork == nil {
		writeError(w, http.StatusServiceUnavailable, "agent work registry is not configured")
		return
	}
	s.ensureAgentRuntimeConfig(r.Context(), roomID)
	var input struct {
		Command            string               `json:"command"`
		State              agentwork.State      `json:"state"`
		StopReason         agentwork.StopReason `json:"stop_reason"`
		Mode               agentwork.Mode       `json:"mode"`
		PlanID             *int64               `json:"plan_id"`
		PlanName           string               `json:"plan_name"`
		HotReload          []string             `json:"hot_reload"`
		BaseWorkingSeconds uint64               `json:"base_working_seconds"`
		LeaseSeconds       uint64               `json:"lease_seconds"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	command := strings.ToLower(strings.TrimSpace(input.Command))
	if command != "" && input.State != "" {
		writeError(w, http.StatusBadRequest, "command and state are mutually exclusive")
		return
	}
	if command != "" && command != "start" && command != "stop" {
		writeError(w, http.StatusBadRequest, "command must be start or stop")
		return
	}
	if command == "" && input.State == "" && input.Mode == "" && input.PlanID == nil && input.LeaseSeconds == 0 && len(input.HotReload) == 0 {
		writeError(w, http.StatusBadRequest, "command, state, mode, plan_id, hot_reload or lease_seconds is required")
		return
	}
	if input.StopReason != "" && command != "stop" && input.State == "" {
		writeError(w, http.StatusBadRequest, "stop_reason requires stop command or state")
		return
	}
	if (command == "start" || input.State == agentwork.StateStarting || input.State == agentwork.StateWorking) &&
		!strings.EqualFold(strings.TrimSpace(room.Status), "live") {
		writeError(w, http.StatusConflict, "room is not live")
		return
	}
	snapshot := s.agentWork.Get(roomID)
	err = nil
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
	if len(input.HotReload) > 0 {
		snapshot, err = s.agentWork.TouchHotReload(roomID, input.HotReload)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		log.Printf("agent hot reload room=%d tenant=%d revision=%d modules=%v plan=%d", roomID, tenantID, snapshot.HotRevision, snapshot.HotModules, snapshot.PlanID)
	}
	if input.LeaseSeconds > 0 {
		snapshot, err = s.agentWork.GrantLease(roomID, input.LeaseSeconds)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	lifecycleUpdated := command != "" || input.State != ""
	if command != "" {
		switch command {
		case "start":
			snapshot, err = s.agentWork.StartAgent(roomID, input.BaseWorkingSeconds)
		case "stop":
			reason := agentwork.NormalizeStopReason(input.StopReason)
			if reason == "" {
				reason = agentwork.StopReasonManual
			}
			snapshot, err = s.agentWork.StopAgent(roomID, reason)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if input.State != "" {
		switch input.State {
		case agentwork.StateStarting, agentwork.StateWorking:
			snapshot, err = s.agentWork.StartAgent(roomID, input.BaseWorkingSeconds)
		case agentwork.StateStopping, agentwork.StateStopped:
			reason := agentwork.NormalizeStopReason(input.StopReason)
			if reason == "" {
				reason = agentwork.StopReasonManual
			}
			snapshot, err = s.agentWork.StopAgent(roomID, reason)
		default:
			err = fmt.Errorf("unsupported agent state %q", input.State)
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if lifecycleUpdated && snapshot.State == agentwork.StateStopped {
		// Stopping paid AI must not touch the free base pipeline.
		// Collection, question clustering and public-screen data keep running.
		s.cleanupPaidAgentRuntime(r.Context(), roomID, snapshot.StopReason)
	}
	writeJSON(w, http.StatusOK, snapshot)
}
