package httpapi

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"log"
	"time"
)

// A crashed management process leaves no recoverable run. A shared, short-lived
// liveness lease allows another node to stop the orphan without recovering its
// progress or mistaking a live producer on a different node for an orphan.
func (s *Server) RunLiveAnchorOwnerWatch(ctx context.Context) {
	owners, ok := s.anchorCycles.(anchorcycle.LiveOwnerRepository)
	if !ok || s.store == nil || s.core == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.leader != nil && !s.leader.IsLeader() {
			continue
		}
		sessions, err := s.store.ListRunningLiveRuntimeSessions(ctx)
		if err != nil {
			log.Printf("formal owner watch list failed: %v", err)
			continue
		}
		for _, session := range sessions {
			if session.Status != "running" || !session.BelongsToExecutionRealm(s.executionRealm) || time.Since(session.StartedAt) < 40*time.Second {
				continue
			}
			check, stop := context.WithTimeout(ctx, 15*time.Second)
			version, err := s.store.GetPublishedLiveAgentPlanVersionForRoom(check, session.TenantID, session.RoomID)
			if err != nil || !isRealtimeScheme(version.GenerationContext) {
				stop()
				continue
			}
			alive, err := owners.LiveOwnerActive(check, session.TenantID, session.RoomID, session.ID)
			if err != nil || alive {
				stop()
				continue
			} // Redis read failures never imply death.
			fresh, err := s.store.GetLiveRuntimeByRoom(check, session.TenantID, session.RoomID)
			if err != nil || fresh.ID != session.ID || fresh.Status != "running" {
				stop()
				continue
			}
			// Presence is a lease, not a state restoration or permission to restart.
			_ = s.requestCorePublishedPlanProgramControl(check, session.TenantID, session.RoomID, "pause")
			coreState, err := s.requestCoreAgentStop(check, session.TenantID, session.RoomID, fresh.TotalBilledSeconds, "system_error")
			if err == nil {
				_, err = s.store.PauseLiveRuntimeSessionMeter(check, session.TenantID, session.RoomID, 0, coreState.WorkingSeconds, time.Now().UTC())
			}
			stop()
			if err != nil {
				log.Printf("formal orphan pause tenant=%d room=%d failed: %v", session.TenantID, session.RoomID, err)
			}
		}
	}
}

func (s *Server) liveAnchorProducerAlive(ctx context.Context, tenant, room, runtimeID int64) (bool, error) {
	owners, ok := s.anchorCycles.(anchorcycle.LiveOwnerRepository)
	if !ok {
		return false, nil
	}
	return owners.LiveOwnerActive(ctx, tenant, room, runtimeID)
}
