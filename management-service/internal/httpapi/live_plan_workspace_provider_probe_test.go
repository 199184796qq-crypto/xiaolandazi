package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Read-only verification of the actual affected saved version. No producer,
// model, review synchronization, session or version save is invoked.
func TestSavedPlanWorkspaceReadOnlyProbe(t *testing.T) {
	if os.Getenv("SAVED_PLAN_WORKSPACE_PROBE") != "1" {
		t.Skip("explicit read-only production probe required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	version, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, 26)
	if err != nil {
		t.Fatal(err)
	}
	scheme, err := realtimeScheme(version.GenerationContext)
	if err != nil || scheme.ProductFocus == nil {
		t.Fatal("saved product snapshot unavailable", err)
	}
	ref := asSupportMap(version.GenerationContext["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	safety, err := store.GetLiveDerivedSafety(ctx, 14, version.PlanID, hash)
	if err != nil {
		t.Fatal(err)
	}
	focus, err := savedProductFocusForFacts(scheme.ProductFocus, safety.SafeFacts)
	if err != nil || len(focus.Facts) != len(scheme.ProductFocus.Facts) || focus.FactsSHA != derivedHash(safety.SafeFacts) {
		t.Fatal("actual saved facts could not be compatibly bound", err)
	}
	checks := s.liveAgentPlanWorkspaceChecks(ctx, 14, version.PlanID, &version)
	raw, _ := json.Marshal(checks)
	t.Logf("tenant=14 room=26 version=%d scoped_facts=%d compatible=true immutable=true checks=%s", version.ID, len(focus.Facts), raw)
}
