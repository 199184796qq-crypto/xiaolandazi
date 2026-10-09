package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
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
	selected, err := store.GetLiveSellingPointWheel(ctx, 14, version.PlanID, scheme.WheelID)
	if err != nil {
		t.Fatal(err)
	}
	var wheel anchorStyleSellingPointWheel
	if err = json.Unmarshal([]byte(selected.EffectiveWheelJSON()), &wheel); err != nil {
		t.Fatal(err)
	}
	queue := savedLegacyQueue(wheel, focus)
	state, err := anchorcycle.New(queue, scheme.Config)
	if err != nil {
		t.Fatal(err)
	}
	state.ProductFocus = focus
	focus.ResetRun()
	state.SelectProductTask()
	if len(queue) != 40 || state.Status != "ready" || state.Task().FocusLink == "" {
		t.Fatalf("actual old version first selection failed: tasks=%d status=%s", len(queue), state.Status)
	}
	t.Logf("saved original wheel=%d matching_tasks=%d first_mainline_selection=ready no_model_no_audio=true", scheme.WheelID, len(queue))
	if os.Getenv("CONTENT_POOL_WORKSPACE_PROBE") == "1" {
		before := contentPoolSetID(version.GenerationContext)
		status, e := s.contentPoolStatus(ctx, 14, version.PlanID, 26, true)
		if e != nil {
			t.Fatal(e)
		}
		source, e := s.captureContentSources(ctx, 14, version.PlanID)
		if e != nil {
			t.Fatal(e)
		}
		rows := map[string]bool{}
		for _, r := range source.Rows {
			rows[r.ID] = true
		}
		facts := contentFactsFromSafety(source, safety.SafeFacts)
		if len(facts) == 0 {
			t.Fatal("source atomization empty")
		}
		for _, f := range facts {
			for _, id := range f.Sources {
				if !rows[id] {
					t.Fatalf("safe fact %s has unknown original source %s", f.ID, id)
				}
			}
		}
		after, e := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, 26)
		if e != nil || after.ID != version.ID || contentPoolSetID(after.GenerationContext) != before {
			t.Fatal("workspace altered saved version", e)
		}
		t.Logf("workspace_state=%s active_products=%d benefits=%d original_source_atoms=%d saved_version_unchanged=%d no_model_no_audio=true", status.State, len(status.Products), len(status.Benefits), len(facts), after.ID)
	}
	checks := s.liveAgentPlanWorkspaceChecks(ctx, 14, version.PlanID, &version)
	raw, _ := json.Marshal(checks)
	t.Logf("tenant=14 room=26 version=%d scoped_facts=%d compatible=true immutable=true checks=%s", version.ID, len(focus.Facts), raw)
}
