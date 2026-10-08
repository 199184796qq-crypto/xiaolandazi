package httpapi

import (
	"context"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"strconv"
	"testing"
	"time"
)

// Read-only operational check: no models, registry write, user confirmation,
// plan generation or playback. Exact tenant/plan must be explicitly selected.
func TestCurrentPlanRiskSourceOriginsProbe(t *testing.T) {
	if os.Getenv("RISK_SOURCE_ORIGINS_PROBE") != "1" {
		t.Skip("explicit read-only production probe required")
	}
	tenant, _ := strconv.ParseInt(os.Getenv("PROBE_TENANT_ID"), 10, 64)
	plan, _ := strconv.ParseInt(os.Getenv("PROBE_PLAN_ID"), 10, 64)
	if tenant <= 0 || plan <= 0 {
		t.Fatal("exact scope required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := &Server{store: store}
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.loadDerivedInput(ctx, tenant, plan, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.ListLiveRiskReviews(ctx, tenant, plan)
	if err != nil {
		t.Fatal(err)
	}
	statuses := map[string]string{}
	for _, r := range saved {
		statuses[r.ClaimSHA256] = r.Status
	}
	pending := map[string]int{}
	confirmed := 0
	for _, r := range d.Reviews {
		status, exists := statuses[r.ClaimSHA256]
		if !exists {
			t.Fatal("current sources have changed; read-only probe will not refresh")
		}
		if status == "confirmed" {
			confirmed++
			continue
		}
		areas := map[string]bool{}
		for _, source := range r.Sources {
			if source.Area == "" {
				t.Fatal("unroutable current source")
			}
			areas[source.Area] = true
			t.Logf("pending rule=%s area=%s entity=%d field=%s", r.RuleID, source.Area, source.EntityID, source.Field)
		}
		for area := range areas {
			pending[area]++
		}
	}
	if len(statuses) != len(d.Reviews) {
		t.Fatal("current review set changed; no write performed")
	}
	t.Logf("read-only scope tenant=%d plan=%d products=%d benefits=%d knowledge=%d miner=%d confirmed=%d", tenant, plan, pending["products"], pending["benefits"], pending["knowledge"], pending["miner"], confirmed)
}
