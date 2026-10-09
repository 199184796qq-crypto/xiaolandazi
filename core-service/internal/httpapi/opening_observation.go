package httpapi

import (
	"context"
	"livecompanion/core/internal/agentwork"
	"strings"
	"time"
)

func observationExempt(origin string) bool {
	return strings.EqualFold(origin, "test_simulation") || strings.EqualFold(origin, "agent_input_preview")
}

func (s *Server) inOpeningObservation(ctx context.Context, roomID, tenantID int64) bool {
	if s.agentWork != nil && s.agentWork.Mode(roomID) == agentwork.ModeControl {
		return false
	}
	// Independent legacy/test consumers do not have the continuous stream.
	state := s.audioDevState()
	if state == nil || state.client == nil {
		return false
	}
	program, err := state.client.ProgramSnapshot(ctx, roomID)
	if err != nil {
		return true
	}
	if !program.Sequential {
		return false
	}
	seconds := 120
	if s.strategyPolicies != nil {
		cfg, err := s.strategyPolicies.InteractionScheduleForPlan(roomID, tenantID, s.agentWork.Get(roomID).PlanID)
		if err != nil {
			return true
		}
		if cfg.OpeningObservationSeconds != nil {
			seconds = *cfg.OpeningObservationSeconds
		}
	}
	return program.ActualStartedAt.IsZero() || time.Since(program.ActualStartedAt) < time.Duration(seconds)*time.Second
}
