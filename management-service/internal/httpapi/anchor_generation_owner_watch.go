package httpapi

import (
	"context"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"log"
	"time"
)

// Resume only a durable, session-bound recording checkpoint after its producer
// lease expires. Missing Redis evidence alone never authorizes a fresh replay.
func (s *Server) RunLiveAnchorOwnerWatch(ctx context.Context) {
	owners, ok := s.anchorCycles.(anchorcycle.LiveOwnerRepository)
	if !ok || s.store == nil || s.core == nil {
		return
	}
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.recoverLiveAnchorOwners(ctx, owners)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) recoverLiveAnchorOwners(ctx context.Context, owners anchorcycle.LiveOwnerRepository) {
	if s.leader != nil && !s.leader.IsLeader() {
		return
	}
	sessions, err := s.store.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		log.Printf("formal owner watch list failed: %v", err)
		return
	}
	for _, session := range sessions {
		if session.Status != "running" || !session.BelongsToExecutionRealm(s.executionRealm) {
			continue
		}
		check, stop := context.WithTimeout(ctx, 15*time.Second)
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
		// Recover the version Core is actually playing, even if another version
		// has since been saved. Never restart from the latest published script.
		snapshot, snapErr := s.core.GetRoomProgramSnapshot(check, session.TenantID, session.RoomID)
		plan, planErr := s.store.GetLiveAgentPlanForRoom(check, session.TenantID, session.RoomID)
		if snapErr != nil || planErr != nil || !snapshot.Running || snapshot.VersionID <= 0 {
			stop()
			continue
		}
		version, err := s.store.GetLiveAgentPlanVersion(check, session.TenantID, plan.ID, snapshot.VersionID)
		if err != nil {
			stop()
			continue
		}
		// A lease expiry proves only missing liveness evidence. A recording
		// stream can resume only from its own durable checkpoint; without one,
		// replaying the producer would restart the first round and duplicate
		// content. Dynamic AI programs remain playback-retained.
		log.Printf("formal owner lease missing tenant=%d room=%d session=%d local_producer=%t; playback retained", session.TenantID, session.RoomID, fresh.ID, s.hasLiveAnchorProducer(session.TenantID, session.RoomID))
		if recordingVoiceMode(version) == recordingStreamMode && !s.hasLiveAnchorProducer(session.TenantID, session.RoomID) {
			programID := ""
			if usesScriptFirstParagraphs(version) {
				programID, _ = recoverScriptSupplyCheckpoint(session.TenantID, session.RoomID, fresh.ID, version.ID, snapshot)
			} else if checkpoint, ok := loadRecordingSupplyCheckpoint(session.TenantID, session.RoomID, fresh.ID); ok && checkpoint.VersionID == version.ID {
				programID = checkpoint.ProgramID
			}
			if programID == "" || programID != snapshot.ProgramID || (usesScriptFirstParagraphs(version) && snapshot.Task.SessionID != fmt.Sprintf("volcengine-long-%d", fresh.ID)) {
				log.Printf("recording supply recovery skipped tenant=%d room=%d session=%d reason=checkpoint_missing_or_stale", session.TenantID, session.RoomID, fresh.ID)
			} else {
				if snapshot.VersionID == version.ID {
					if startErr := s.startLiveAnchorProducer(check, session.TenantID, session.RoomID, fresh.ID, version); startErr != nil {
						log.Printf("recording supply recovery failed tenant=%d room=%d session=%d: %v", session.TenantID, session.RoomID, fresh.ID, startErr)
					} else {
						log.Printf("recording supply recovered tenant=%d room=%d session=%d program=%s", session.TenantID, session.RoomID, fresh.ID, snapshot.ProgramID)
					}
				}
			}
		}
		stop()
	}
}

func (s *Server) liveAnchorProducerAlive(ctx context.Context, tenant, room, runtimeID int64) (bool, error) {
	owners, ok := s.anchorCycles.(anchorcycle.LiveOwnerRepository)
	if !ok {
		return false, nil
	}
	return owners.LiveOwnerActive(ctx, tenant, room, runtimeID)
}
