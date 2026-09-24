package httpapi

import (
	"context"
	"log"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

func (s *Server) refreshRunningPolicySnapshots(ctx context.Context) {
	sessions, err := s.store.ListRunningLiveRuntimeSessions(ctx)
	if err != nil {
		log.Printf("refresh running policy snapshots: list sessions: %v", err)
		return
	}

	for _, session := range sessions {
		industryCode, l1, l2, l3, loadErr := s.store.LoadLivePolicyLayers(
			ctx,
			session.TenantID,
			session.RoomID,
		)
		if loadErr != nil {
			log.Printf(
				"refresh running policy snapshots: load tenant=%d room=%d session=%d: %v",
				session.TenantID,
				session.RoomID,
				session.ID,
				loadErr,
			)
			continue
		}
		effective := policy.BuildEffective(industryCode, l1, l2, l3)
		if saveErr := s.store.SaveLiveRuntimePolicySnapshot(
			ctx,
			model.LiveRuntimePolicySnapshot{
				SessionID:    session.ID,
				TenantID:     session.TenantID,
				RoomID:       session.RoomID,
				IndustryCode: industryCode,
				L1VersionID:  policyVersionID(l1),
				L2VersionID:  policyVersionID(l2),
				L3VersionID:  policyVersionID(l3),
				Effective:    effective,
			},
		); saveErr != nil {
			log.Printf(
				"refresh running policy snapshots: save tenant=%d room=%d session=%d: %v",
				session.TenantID,
				session.RoomID,
				session.ID,
				saveErr,
			)
		}
	}
}
