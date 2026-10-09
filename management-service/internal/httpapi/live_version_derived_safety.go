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
	if ref := asSupportMap(snapshot["content_pools"]); len(ref) > 0 {
		ref["voice_only"] = true
		snapshot["content_pools"] = ref
	}
	// Changing only voice/emotion must carry the same immutable content version.
	ref := asSupportMap(snapshot["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	if _, err = s.store.GetLiveDerivedSafety(ctx, tenant, plan, hash); err != nil {
		return err
	}
	if isRealtimeScheme(snapshot) {
		return s.validateRealtimeScheme(ctx, tenant, plan, snapshot)
	}

	return nil
}
