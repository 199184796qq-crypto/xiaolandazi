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

// Opt-in paid text test of actual production planning/rendering code. Playback
// receipts below are simulated, explicitly recorded as such, never live audio.
func TestAngleContentProductionProviderSmoke(t *testing.T) {
	if os.Getenv("ANGLE_CONTENT_PROVIDER_SMOKE") != "1" {
		t.Skip("explicit paid smoke required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	raw, e := os.ReadFile(os.Getenv("ANGLE_CONTENT_SMOKE_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &input); e != nil {
		t.Fatal(e)
	}
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("model configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	settings, e := store.AnchorGenerationSettings(ctx)
	if e != nil {
		t.Fatal(e)
	}
	profile, e := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if e != nil || profile == nil {
		t.Fatal("speech model missing")
	}
	plannerID, e := store.ResolveTextRole(ctx, "mainline_angle_plan")
	if e != nil {
		t.Fatal(e)
	}
	planner, e := store.ResolveSpeechModel(ctx, plannerID)
	if e != nil || planner == nil {
		t.Fatal("planner missing")
	}
	rows := make([]map[string]any, len(input))
	var wg sync.WaitGroup
	for i, c := range input {
		wg.Add(1)
		go func(i int, state anchorcycle.State, name string) {
			defer wg.Done()
			server := &Server{store: store}
			state.RunID = "offline-angle-content-" + anchorcycle.ID()
			state.PlanID = 0 // No business plan asset writes.
			state.Config.TextProfileID = profile.ID
			state.TextModel = profile
			state.Used = map[string]anchorcycle.Use{}
			state.Attempts = nil
			state.Accepted = nil
			state.Surface = "formal_live"
			state.Delivery = &anchorcycle.MainlineDelivery{ProgramID: "simulated-" + state.RunID}
			server.ensureCycleAnglePlan(ctx, &state, *profile)
			p := state.CurrentAnglePlan()
			row := map[string]any{"name": name, "initial_plan": p, "warnings": state.Warnings}
			rows[i] = row
			if p == nil || len(p.Cards) == 0 || p.PlanError != "" {
				t.Errorf("%s initial plan failed", name)
				return
			}
			texts := []map[string]any{}
			for visit := 0; visit < 6; visit++ {
				response, messages, err := server.completeCycleText(ctx, &state)
				h := state.CurrentAngleHook() // A due supplement may change the selection.
				texts = append(texts, map[string]any{"visit": visit + 1, "hook": h, "response": response, "messages": messages, "success": err == nil})
				row["texts"] = texts
				if err != nil {
					row["error"] = err.Error()
				}
				if err != nil || response.Text == "" || textControlLeak(response.Text) {
					t.Errorf("%s writing failed", name)
					return
				}
				segment := anchorcycle.Segment{Phase: state.Phase, Task: state.Task(), Selected: anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, AnglePlan: state.CurrentAnglePlan(), AngleHook: h}}
				state.Accepted = append(state.Accepted, segment)
				u := state.Used[state.Task().Key]
				u.Count++
				state.Used[state.Task().Key] = u
				state.RegisterAngleTrack(segment)
				state.ApplyAnglePlayback(state.Delivery.ProgramID, []anchorcycle.AnglePlaybackReceipt{{Sequence: uint64(visit + 1), TrackID: segment.Selected.ID, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}})
			}
			row["final_plan"] = state.CurrentAnglePlan()
			row["simulated_usage"] = state.AngleProgress
			row["warnings"] = state.Warnings
			if state.CurrentAnglePlan().GenerationCount != 2 || len(state.CurrentAnglePlan().Cards) <= len(p.Cards) {
				t.Errorf("%s supplement lost old generation", name)
			}
		}(i, c.State, c.Name)
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"passed": !t.Failed(), "planning_model": planner.Model, "speech_model": profile.Model, "text_only": true, "playback_count_simulated": true, "plan_assets_unchanged": true, "rows": rows}, "", "  ")
	if e = os.WriteFile(os.Getenv("ANGLE_CONTENT_SMOKE_OUTPUT"), raw, 0600); e != nil {
		t.Fatal(e)
	}
}
