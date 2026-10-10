package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Replays the exact 24 fresh candidates through the production filtering path.
// Program bindings and raw history survive; newly kept cards enter the next
// comparison immediately. All durable plan assets remain untouched.
func TestAngleSupplementFilteringProviderProbe(t *testing.T) {
	if os.Getenv("ANGLE_FILTER_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid filtering probe required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	var supplement struct {
		Rows []struct {
			Name   string   `json:"name"`
			Old    []string `json:"initial_direction_texts"`
			Rounds []struct {
				Cards []anchorcycle.AngleHook `json:"new_cards"`
			} `json:"rounds"`
		} `json:"rows"`
	}
	raw, err := os.ReadFile(os.Getenv("ANGLE_EXPANSION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("input invalid")
	}
	raw, err = os.ReadFile(os.Getenv("ANGLE_SUPPLEMENT_OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &supplement) != nil {
		t.Fatal("supplement invalid")
	}
	policy, source, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	id, err := store.ResolveTextRole(context.Background(), "mainline_angle_plan")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(context.Background(), id)
	if err != nil || profile == nil {
		t.Fatal("planner missing")
	}
	server := &Server{store: store}
	rows := make([]map[string]any, len(supplement.Rows))
	var wg sync.WaitGroup
	for i, topic := range supplement.Rows {
		var state anchorcycle.State
		for _, c := range input {
			if c.Name == topic.Name {
				state = c.State
			}
		}
		state.PlanID = 0
		state.RunID = "offline-angle-expansion-filter-" + anchorcycle.ID()
		state.P0PolicyMD = ""
		if state.InstallP0(policy, source) != nil {
			t.Fatal("P0 unavailable")
		}
		old := &anchorcycle.AnglePlan{}
		for _, text := range topic.Old {
			old.Cards = append(old.Cards, anchorcycle.AngleHook{ID: anchorcycle.ID(), Action: text})
		}
		wg.Add(1)
		go func(i int, name string, state anchorcycle.State, old *anchorcycle.AnglePlan, rounds []struct {
			Cards []anchorcycle.AngleHook `json:"new_cards"`
		}) { defer wg.Done(); records := []map[string]any{}; kept := []anchorcycle.AngleHook{}; row := map[string]any{"name": name, "run_id": state.RunID, "initial_count": len(old.Cards)}; rows[i] = row; for ri, r := range rounds {
			plan := &anchorcycle.AnglePlan{Cards: append([]anchorcycle.AngleHook(nil), r.Cards...)}
			started := time.Now()
			server.filterAngleSupplement(context.Background(), &state, *profile, plan, old)
			if len(plan.GeneratedCards) != len(r.Cards) {
				t.Errorf("raw candidates lost")
			}
			records = append(records, map[string]any{"round": ri + 1, "plan": plan, "latency_ms": time.Since(started).Milliseconds()})
			kept = append(kept, plan.Cards...)
			old.Cards = anchorcycle.DistinctDirectionCards(old.Cards, plan.Cards)
		}; row["rounds"], row["kept_cards"], row["final_usable_count"] = records, kept, len(old.Cards) }(i, topic.Name, state, old, topic.Rounds)
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"request_test_passed": !t.Failed(), "model": profile.Model, "text_only": true, "plan_assets_unchanged": true, "p0_policy_sha": derivedHash(policy), "rows": rows}, "", "  ")
	if err = os.WriteFile(os.Getenv("ANGLE_FILTER_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
