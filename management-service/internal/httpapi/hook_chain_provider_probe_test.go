package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"sync"
	"testing"
	"time"
)

// Explicit paid, text-only smoke of the production planning/rendering path.
// Model exchanges are archived, but no run/plan/queue/TTS/audio is created.
func TestHookChainProductionProviderSmoke(t *testing.T) {
	if os.Getenv("HOOK_CHAIN_PROVIDER_SMOKE") != "1" {
		t.Skip("explicit paid smoke required")
	}
	raw, e := os.ReadFile(os.Getenv("HOOK_CHAIN_SMOKE_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	if e = json.Unmarshal(raw, &input); e != nil {
		t.Fatal(e)
	}
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	settings, e := store.AnchorGenerationSettings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	profile, e := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if e != nil || profile == nil || profile.Model != "qwen3.7-flash" {
		t.Fatal("configured speech role is not Flash")
	}
	rows := make([]map[string]any, len(input))
	var wg sync.WaitGroup
	for i, c := range input {
		wg.Add(1)
		go func(i int, state anchorcycle.State, name string) {
			defer wg.Done()
			server := &Server{store: store}
			state.RunID = "offline-hook-chain-" + name
			state.Config.TextProfileID = profile.ID
			state.TextModel = profile
			state.Used = map[string]anchorcycle.Use{}
			state.Attempts = nil
			state.Accepted = nil
			server.ensureCycleAnglePlan(ctx, &state, *profile)
			plan := state.CurrentAnglePlan()
			row := map[string]any{"name": name, "plan": plan, "warnings": state.Warnings}
			rows[i] = row
			if plan == nil || plan.PlanError != "" || len(plan.Cards) == 0 || plan.Model != "qwen3.8-max" {
				t.Errorf("%s planning did not return Max cards", name)
				return
			}
			before := plan
			server.ensureCycleAnglePlan(ctx, &state, *profile)
			if state.CurrentAnglePlan() != before {
				t.Errorf("%s repeated planning", name)
				return
			}
			texts := []map[string]any{}
			for visit := 0; visit < 2*len(plan.Cards); visit++ {
				h := state.CurrentAngleHook()
				response, messages, e := server.completeCycleText(ctx, &state)
				texts = append(texts, map[string]any{"visit": visit + 1, "hook": h, "response": response, "messages": messages, "success": e == nil})
				row["texts"] = texts
				if e != nil {
					row["error"] = e.Error()
				}
				if e != nil || response.Text == "" || textControlLeak(response.Text) {
					t.Errorf("%s text generation/protocol failed", name)
					return
				}
				state.Accepted = append(state.Accepted, anchorcycle.Segment{Phase: state.Phase, Task: state.Task(), Selected: anchorcycle.Candidate{Text: response.Text, AnglePlan: plan, AngleHook: h}})
				key := state.Task().Key
				u := state.Used[key]
				u.Count++
				state.Used[key] = u
			}
		}(i, c.State, c.Name)
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"passed": !t.Failed(), "speech_model": profile.Model, "text_only": true, "rows": rows}, "", "  ")
	if e = os.WriteFile(os.Getenv("HOOK_CHAIN_SMOKE_OUTPUT"), raw, 0600); e != nil {
		t.Fatal(e)
	}
}
