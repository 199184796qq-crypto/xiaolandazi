package httpapi

import (
	"context"
	"errors"
	"log"
	"time"
)

func (s *Server) finishRoomDeletion(ctx context.Context, tenantID, roomID int64) error {
	coreCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	coreErr := s.core.DeleteRoomEverywhere(coreCtx, tenantID, roomID)
	cancel()
	// Release bindings/registrations as soon as the shared room row is gone,
	// even if another Core replica is offline. Keep the job pending until every
	// replica has acknowledged stopping its process-local execution.
	err := errors.Join(coreErr, s.store.CleanupDeletedRoomDerivedState(ctx, tenantID, roomID))
	if err == nil {
		err = s.store.CompleteRoomDeletion(ctx, tenantID, roomID)
	}
	if err != nil {
		// A canceled browser request must not erase the durable retry evidence.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if recordErr := s.store.RecordRoomDeletionFailure(recordCtx, tenantID, roomID, err); recordErr != nil {
			log.Printf("record room deletion failure tenant=%d room=%d: %v", tenantID, roomID, recordErr)
		}
	}
	return err
}

func (s *Server) RunRoomDeletionCleanup(ctx context.Context) {
	if s == nil || s.store == nil || s.core == nil {
		return
	}
	run := func() {
		if s.leader != nil && !s.leader.IsLeader() {
			return
		}
		queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		items, err := s.store.PendingRoomDeletions(queryCtx)
		cancel()
		if err != nil {
			log.Printf("list pending room deletions: %v", err)
			return
		}
		for _, item := range items {
			if ctx.Err() != nil {
				return
			}
			jobCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := s.finishRoomDeletion(jobCtx, item.TenantID, item.RoomID)
			cancel()
			if err != nil {
				log.Printf("retry room deletion tenant=%d room=%d: %v", item.TenantID, item.RoomID, err)
			}
		}
	}
	run()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
