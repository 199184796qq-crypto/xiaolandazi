package httpapi

import (
	"context"
	"time"
)

// Accounting-only recovery also runs after the last room stops. Previously a
// durable late receipt could wait indefinitely for the next paid operation.
// This never invokes a supplier, starts a room, or turns unknown usage into zero.
func (s *Server) RunLiveCostReconciliation(ctx context.Context) {
	s.runLiveCostReconciliation(ctx, 30*time.Second)
}

func (s *Server) runLiveCostReconciliation(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.replayLiveCostSpool(ctx, s.liveCostController())
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
