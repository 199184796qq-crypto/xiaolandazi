package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Paid, text-only comparison: same facts/style/P0/model, with and without
// direction planning. No live, TTS or business plan asset mutations. Receipts
// and the second-session pool reload are explicitly simulated.
func TestAngleExpansionProviderComparison(t *testing.T) {
	if os.Getenv("ANGLE_EXPANSION_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid expansion comparison required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	raw, err := os.ReadFile(os.Getenv("ANGLE_EXPANSION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	policy, source, err := loadP0Mother()
	if err != nil {
		t.Fatal("effective P0 unavailable")
	}
	for i := range input {
		input[i].State.P0PolicyMD = ""
		if err = input[i].State.InstallP0(policy, source); err != nil {
			t.Fatal(err)
		}
		if err = requireCycleP0(&input[i].State); err != nil {
			t.Fatal(err)
		}
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if err != nil || profile == nil {
		t.Fatal("speech model missing")
	}
	plannerID, err := store.ResolveTextRole(ctx, "mainline_angle_plan")
	if err != nil {
		t.Fatal(err)
	}
	planner, err := store.ResolveSpeechModel(ctx, plannerID)
	if err != nil || planner == nil {
		t.Fatal("planning model missing")
	}
	rows := make([]map[string]any, len(input)*2)
	for variant := 0; variant < 2; variant++ {
		mode := "original_angle"
		enabled := "0"
		if variant == 1 {
			mode, enabled = "accumulating_directions", "1"
		}
		t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", enabled)
		var wg sync.WaitGroup
		for i, item := range input {
			wg.Add(1)
			go func(i, variant int, name, mode string, state anchorcycle.State) {
				defer wg.Done()
				server := &Server{store: store}
				state.PlanID = 0
				state.RunID = "offline-angle-expansion-" + anchorcycle.ID()
				state.Config.TextProfileID = profile.ID
				state.TextModel = profile
				state.Config.AngleRefreshAfterPlays = 3
				state.Used = map[string]anchorcycle.Use{}
				state.Accepted, state.Attempts = nil, nil
				state.Surface = "formal_live"
				state.Delivery = &anchorcycle.MainlineDelivery{ProgramID: "simulated-" + state.RunID}
				row := map[string]any{"name": name, "variant": mode, "run_id": state.RunID}
				rows[variant*len(input)+i] = row
				texts := []map[string]any{}
				generations := []map[string]any{}
				lastGeneration := ""
				reusedGenerationCount := 0
				visits := 12
				if variant == 1 {
					visits = 15
				}
				sequence := uint64(0)
				for visit := 0; visit < visits; visit++ {
					if visit == 12 {
						// Round-trip just the durable pool, then create fresh run progress.
						p := state.CurrentAnglePlan()
						if p == nil {
							t.Errorf("%s missing saved pool", name)
							return
						}
						reusedGenerationCount = p.GenerationCount
						row["first_session_actual_plays_simulated"] = state.AngleUsage(state.AnglePlanKey()).Plays
						p.LifetimePlays = map[string]int{}
						for id, n := range state.AngleUsage(state.AnglePlanKey()).DirectionPlays {
							p.LifetimePlays[id] = n
						}
						pool, _ := json.Marshal(p)
						var restored anchorcycle.AnglePlan
						if json.Unmarshal(pool, &restored) != nil {
							t.Errorf("pool reload failed")
							return
						}
						state.AnglePlans = map[string]*anchorcycle.AnglePlan{state.AnglePlanKey(): &restored}
						state.AngleProgress, state.AngleTrackBindings, state.AngleRegisteredTracks = nil, nil, nil
						state.AngleReceiptSequence = 0
						state.Used = map[string]anchorcycle.Use{}
						state.Accepted, state.Attempts = nil, nil
						state.RunID = "offline-angle-expansion-reuse-" + anchorcycle.ID()
						state.Delivery = &anchorcycle.MainlineDelivery{ProgramID: "simulated-" + state.RunID}
						row["reuse_run_id"] = state.RunID
						row["new_session_usage_before"] = state.AngleUsage(state.AnglePlanKey()).Plays
						sequence = 0
					}
					response, messages, callErr := server.completeCycleText(ctx, &state)
					hook, plan := state.CurrentAngleHook(), state.CurrentAnglePlan()
					if plan != nil && plan.GenerationID != lastGeneration {
						generations = append(generations, map[string]any{"before_visit": visit + 1, "plan": *plan})
						lastGeneration = plan.GenerationID
					}
					result := map[string]any{"visit": visit + 1, "session": 1, "run_id": state.RunID, "messages": messages, "response": response, "request_success": callErr == nil, "hook": hook}
					if visit >= 12 {
						result["session"] = 2
					}
					texts = append(texts, result)
					row["texts"], row["generations"] = texts, generations
					if callErr != nil {
						result["error"] = callErr.Error()
						t.Errorf("%s/%s request failed", name, mode)
						continue
					}
					candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, OutputText: response.Text, AngleHook: hook, AnglePlan: plan}
					cleanCycleOpeningInstruction(&state, &candidate, "")
					if textControlLeak(candidate.OutputText) {
						candidate.P0PatchError = "正文含明确内部控制字段"
					}
					if err := state.CheckP0Candidate(&candidate); err != nil {
						result["review_error"] = err.Error()
						t.Errorf("P0 check failed")
						continue
					}
					result["p0_report"], result["final_text"] = candidate.P0Report, candidate.OutputText
					passed := candidate.P0Report != nil && candidate.P0Report.Passed
					result["review_passed"], result["simulated_playback"] = passed, passed
					if !passed {
						continue
					}
					// Only approved drafts receive a simulated PCM receipt. Failed
					// drafts cannot trigger the three-play supplement threshold.
					segment := anchorcycle.Segment{Phase: state.Phase, Task: state.Task(), Selected: candidate}
					state.Accepted = append(state.Accepted, segment)
					u := state.Used[state.Task().Key]
					u.Count++
					state.Used[state.Task().Key] = u
					state.RegisterAngleTrack(segment)
					sequence++
					state.ApplyAnglePlayback(state.Delivery.ProgramID, []anchorcycle.AnglePlaybackReceipt{{Sequence: sequence, TrackID: candidate.ID, StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}})
					if (visit+1)%3 == 0 {
						fmt.Printf("%s / %s: %d drafts complete\n", mode, name, visit+1)
					}
				}
				row["final_plan"], row["simulated_progress"], row["warnings"] = state.CurrentAnglePlan(), state.AngleProgress, state.Warnings
				if variant == 1 && (state.CurrentAnglePlan().GenerationCount != reusedGenerationCount || state.AngleUsage(state.AnglePlanKey()).Plays > 3) {
					t.Errorf("%s pool reuse or session reset failed", name)
				}
			}(i, variant, item.Name, mode, item.State)
		}
		wg.Wait()
	}
	raw, _ = json.MarshalIndent(map[string]any{"request_test_passed": !t.Failed(), "text_only": true, "business_plan_assets_unchanged": true, "playback_counts_simulated": true, "new_session_reload_simulated": true,
		"rejected_drafts_receive_playback_receipts": false, "planning_model": planner.Model, "speech_model": profile.Model, "p0_policy_sha": derivedHash(policy), "rows": rows}, "", "  ")
	if err = os.WriteFile(os.Getenv("ANGLE_EXPANSION_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
