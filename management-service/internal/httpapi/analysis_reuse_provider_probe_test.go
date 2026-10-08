package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Explicitly scoped operational check. Only immutable analysis assets/model
// traces may be saved; never edits a wheel, version, voice or running session.
func TestCurrentPlanAnalysisReuseProbe(t *testing.T) {
	if os.Getenv("PLAN_ANALYSIS_REUSE_PROBE") != "1" {
		t.Skip("explicit scoped provider verification required")
	}
	tenant, _ := strconv.ParseInt(os.Getenv("PROBE_TENANT_ID"), 10, 64)
	plan, _ := strconv.ParseInt(os.Getenv("PROBE_PLAN_ID"), 10, 64)
	room, _ := strconv.ParseInt(os.Getenv("PROBE_ROOM_ID"), 10, 64)
	if tenant <= 0 || plan <= 0 || room <= 0 {
		t.Fatal("explicit probe scope required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	input, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	wheel, err := store.GetActiveLiveSellingPointWheel(ctx, tenant, plan)
	if err != nil {
		t.Fatal(err)
	}
	if wheel.Status != "confirmed" || wheel.FactPackageSHA256 != derivedHash(input.Facts) || emptySellingPointPool(wheel.EffectiveWheelJSON()) || len(sellingPointFactBoundaryViolations(wheel.EffectiveWheelJSON(), input.Facts)) > 0 {
		t.Fatal("current pool is not an unchanged reusable pool; will not generate or edit it")
	}
	contract, _, err := loadSellingPointWheelFinalAuditPrompt()
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	handled, points, angles, err := s.reuseOrUpdateWheel(ctx, tenant, plan, 0, input.Facts, contract, "", settings.SellingPointProfileID)
	if err != nil || !handled {
		t.Fatal("unchanged pool reuse failed", err)
	}
	t.Logf("unchanged_pool_reuse=%s points=%d angles=%d; pool unchanged", time.Since(started), points, angles)
	version, err := store.GetPublishedLiveAgentPlanVersionForRoom(ctx, tenant, room)
	if err != nil {
		t.Fatal(err)
	}
	ref := asSupportMap(version.GenerationContext["derived_safety"])
	hash, _ := ref["source_sha256"].(string)
	safety, err := store.GetLiveDerivedSafety(ctx, tenant, plan, hash)
	if err != nil {
		t.Fatal(err)
	}
	var safeWheel anchorStyleSellingPointWheel
	if err = json.Unmarshal([]byte(safety.SafeWheelJSON), &safeWheel); err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{TenantID: tenant, PlanID: plan, RoomID: room, Facts: safety.SafeFacts, Phase: "opening", CycleNumber: 1}
	for _, item := range flattenAnchorStyleWheelTasks(safeWheel) {
		state.Queue = append(state.Queue, anchorcycle.Task{SellingPoint: item.SellingPoint, Angle: item.Angle, Key: item.Key})
	}
	links, err := store.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	first, err := s.buildCycleProductFocus(ctx, state, links)
	if err != nil {
		t.Fatal(err)
	}
	firstDuration := time.Since(started)
	started = time.Now()
	second, err := s.buildCycleProductFocus(ctx, state, links)
	if err != nil {
		t.Fatal(err)
	}
	secondDuration := time.Since(started)
	if anchorcycle.ProductDigest(first) != anchorcycle.ProductDigest(second) {
		t.Fatal("reused scope changed")
	}
	if secondDuration > 2*time.Second {
		t.Fatal("repeat classification did not use stored asset", secondDuration)
	}
	t.Logf("classification_first=%s classification_repeat=%s; no plan save/publish/playback", firstDuration, secondDuration)
}
