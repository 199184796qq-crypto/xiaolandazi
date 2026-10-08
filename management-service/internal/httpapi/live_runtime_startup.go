package httpapi

import (
	"context"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"log"
	"time"
)

func (s *Server) reportLiveStartup(ctx context.Context, tenant, room, session int64, stage string) {
	repo, ok := s.anchorCycles.(anchorcycle.LiveStartupRepository)
	if !ok {
		return
	}
	progress := anchorcycle.LiveStartup{SessionID: session, Stage: stage, Percent: anchorcycle.StartupPercent(stage), UpdatedAt: time.Now().UTC()}
	limited, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := repo.SaveLiveStartup(limited, tenant, room, progress); err != nil {
		log.Printf("startup status tenant=%d room=%d session=%d: %v", tenant, room, session, err)
	}
}
func (s *Server) populateLiveRuntimeStartup(ctx context.Context, tenant, room int64, snapshot *model.LiveRuntimeSnapshot) {
	if snapshot.Session == nil || snapshot.Failure != nil || snapshot.Session.Status != "running" {
		return
	}
	repo, ok := s.anchorCycles.(anchorcycle.LiveStartupRepository)
	if !ok {
		return
	}
	p, err := repo.GetLiveStartup(ctx, tenant, room, snapshot.Session.ID)
	if err != nil {
		log.Printf("read startup status tenant=%d room=%d: %v", tenant, room, err)
		return
	}
	if p != nil {
		snapshot.Startup = &model.LiveRuntimeStartup{SessionID: p.SessionID, Stage: p.Stage, Percent: p.Percent, UpdatedAt: p.UpdatedAt}
	}
}
