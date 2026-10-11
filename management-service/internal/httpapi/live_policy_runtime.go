package httpapi

import "context"

func (s *Server) refreshRunningPolicySnapshots(ctx context.Context) {
	// Called by legacy catalog publish/rollback handlers. Those operations no
	// longer change live rules, nor rewrite an already running plan snapshot.
	// A new live session captures its public P0 in live_runtime.go; business
	// assets continue to belong to the saved agent plan.
}
