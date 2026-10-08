package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/model"
)

// Voice-only re-publications still pass the same derived safety gate. Original
// scripts/recordings and source facts remain untouched.
func (s *Server) refreshVersionDerivedSafety(ctx context.Context, tenant, plan, actor int64, input *model.CreateLiveAgentPlanVersionInput) error {
	raw, err := json.Marshal(input.GenerationContext)
	if err != nil {
		return err
	}
	var snapshot map[string]any
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	input.GenerationContext = snapshot
	safety, err := s.prepareDerivedSafety(ctx, tenant, plan, actor)
	if err != nil {
		return err
	}
	snapshot["derived_safety"] = map[string]any{"asset_id": safety.ID, "source_sha256": safety.SourceSHA256, "rewritten_count": safety.RewrittenCount, "notice": safety.Notice}
	if isRealtimeScheme(snapshot) {
		return s.validateRealtimeScheme(ctx, tenant, plan, snapshot)
	}
	return nil
}
