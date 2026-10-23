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
			// A lease expiry proves only missing liveness evidence. Existing Core
			// audio must continue; it does not authorize a technical stop, a new
			// producer, or recovery of a prior process's cursor. A surviving local
			// producer renews/reclaims its exact same-session lease itself.
			log.Printf("formal owner lease missing tenant=%d room=%d session=%d local_producer=%t; playback retained", session.TenantID, session.RoomID, fresh.ID, s.hasLiveAnchorProducer(session.TenantID, session.RoomID))
			stop()
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
