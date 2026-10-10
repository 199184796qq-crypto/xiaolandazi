package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
)

func TestAngleSupplementProviderProbe(t *testing.T) {
	if os.Getenv("ANGLE_SUPPLEMENT_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid supplement probe required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	var previous struct {
		Rows []struct {
			Name    string                 `json:"name"`
			Variant string                 `json:"variant"`
			Plan    *anchorcycle.AnglePlan `json:"final_plan"`
		} `json:"rows"`
	}
	raw, err := os.ReadFile(os.Getenv("ANGLE_EXPANSION_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("input invalid")
	}
	raw, err = os.ReadFile(os.Getenv("ANGLE_EXPANSION_OUTPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &previous) != nil {
		t.Fatal("previous pool invalid")
	}
	mother, err := interactionpolicy.Read("angle-direction-supplement-probe.md")
	if err != nil {
		t.Fatal(err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	speech, err := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if err != nil || speech == nil {
		t.Fatal("speech missing")
	}
	id, err := store.ResolveTextRole(ctx, "mainline_angle_plan")
	if err != nil {
		t.Fatal(err)
	}
	planner, err := store.ResolveSpeechModel(ctx, id)
	if err != nil || planner == nil {
		t.Fatal("planner missing")
	}
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "1")
	rows := make([]map[string]any, len(input))
	var wg sync.WaitGroup
	for i, item := range input {
		var saved *anchorcycle.AnglePlan
		for _, r := range previous.Rows {
			if r.Name == item.Name && r.Variant == "accumulating_directions" {
				saved = r.Plan
			}
		}
		if saved == nil {
			t.Fatal("previous pool missing")
		}
		wg.Add(1)
		go func(i int, name string, state anchorcycle.State, saved *anchorcycle.AnglePlan) {
			defer wg.Done()
			server := &Server{store: store}
			state.PlanID = 0
			state.RunID = "offline-angle-expansion-supplement-" + anchorcycle.ID()
			state.TextModel = speech
			state.Config.TextProfileID = speech.ID
			state.P0PolicyMD = ""
			if state.InstallP0(policy, source) != nil {
				t.Errorf("P0 invalid")
				return
			}
			state.Used = map[string]anchorcycle.Use{}
			state.Accepted = nil
			state.Attempts = nil
			state.AnglePlanMotherSHA = saved.PromptSHA
			old := []string{}
			seen := map[string]bool{}
			for _, c := range saved.Cards {
				if !seen[c.Action] {
					seen[c.Action] = true
					old = append(old, c.Action)
				}
			}
			row := map[string]any{"name": name, "run_id": state.RunID, "initial_direction_texts": append([]string(nil), old...)}
			rows[i] = row
			rounds := []map[string]any{}
			for round := 0; round < 3; round++ {
				if requireCycleP0(&state) != nil {
					t.Errorf("P0 missing")
					return
				}
				packet, _ := json.Marshal(map[string]any{"本次卖点": state.Task().SellingPoint, "本次角度": state.Task().Angle, "观众顾虑": state.Task().PainPoint, "当前事实": generationFactView(state.HookFacts()), "有效P0": generationP0View(policy, state.Config.ComplianceScope), "已有方向": old})
				request := agentgateway.Request{Stage: "angle_supplement_expansion_probe", Model: planner.Model, Messages: []agentgateway.Message{{Role: "system", Content: string(mother)}, {Role: "user", Content: string(packet)}}, ResponseFormat: agentgateway.ResponseText, MaxTokens: 700, EnableThinking: false, Timeout: 20 * time.Second}
				callCtx := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 0, RunID: state.RunID})
				exchange, e := server.beginModelExchange(callCtx, request.Stage, planner.ID, request)
				if e != nil {
					t.Errorf("archive failed")
					return
				}
				started := time.Now()
				response, e := agentgateway.CompleteSpeechEndpoint(callCtx, *planner, request)
				response.Provider, response.Model = "anchor:"+planner.ID, planner.Model
				e = server.finishModelExchange(callCtx, exchange, response, response.Text, started, response.InputTokens, response.OutputTokens, e)
				r := map[string]any{"round": round + 1, "exchange_id": exchange, "request": request, "response": response, "request_success": e == nil}
				rounds = append(rounds, r)
				row["rounds"] = rounds
				if e != nil {
					t.Errorf("supplement request failed")
					continue
				}
				plan := anchorcycle.ParseDirectionContent(response.Text, anchorcycle.ID(), state.HookFacts(), state.HookProducts(), state.Task().SellingPoint)
				r["new_cards"] = plan.Cards
				if len(plan.Cards) == 0 {
					continue
				}
				for _, c := range plan.Cards {
					old = append(old, c.Action)
				}
				// Give the speech model one new direction, as production does.
				plan.Cards = plan.Cards[:1]
				plan.SourceSHA = state.AnglePlanKey()
				plan.PromptSHA = state.AnglePlanMotherSHA
				state.AnglePlans = map[string]*anchorcycle.AnglePlan{plan.SourceSHA: plan}
				written, messages, e := server.completeCycleText(ctx, &state)
				r["writing_response"], r["writing_messages"], r["writing_success"] = written, messages, e == nil
				if e != nil {
					t.Errorf("writing failed")
					continue
				}
				candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: written.Text, OutputText: written.Text, AnglePlan: plan, AngleHook: &plan.Cards[0]}
				if state.CheckP0Candidate(&candidate) != nil {
					t.Errorf("P0 scan failed")
					continue
				}
				server.reviewCycleP0Context(ctx, &state, &candidate)
				r["p0_report"], r["final_text"] = candidate.P0Report, candidate.OutputText
				if candidate.P0Report.Passed {
					state.Accepted = append(state.Accepted, anchorcycle.Segment{Task: state.Task(), Selected: candidate})
					u := state.Used[state.Task().Key]
					u.Count++
					state.Used[state.Task().Key] = u
				}
			}
			row["all_direction_texts"] = old
		}(i, item.Name, item.State, saved)
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"request_test_passed": !t.Failed(), "planning_model": planner.Model, "speech_model": speech.Model, "text_only": true, "business_plan_assets_unchanged": true, "p0_policy_sha": derivedHash(policy), "prompt": string(mother), "rows": rows}, "", "  ")
	if err = os.WriteFile(os.Getenv("ANGLE_SUPPLEMENT_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
