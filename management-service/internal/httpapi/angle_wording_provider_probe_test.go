package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

// Hold one direction fixed and independently generate three wordings. All
// model selection, facts and P0 use the actual production compilation path.
func TestFixedAngleThreeWordingsProviderProbe(t *testing.T) {
	if os.Getenv("ANGLE_WORDING_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid wording test required")
	}
	var input []struct {
		Name  string            `json:"name"`
		State anchorcycle.State `json:"state"`
	}
	var prior struct {
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
	if json.Unmarshal(raw, &prior) != nil {
		t.Fatal("saved test pool invalid")
	}
	policy, source, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ANGLE_HOOK_PLAN_PROMPT_PATH", "configs/prompts/anchor-style/angle-hook-plan.md", "角度母版")
	if err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	settings, err := store.AnchorGenerationSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	speech, err := store.ResolveSpeechModel(context.Background(), settings.Cycle.TextProfileID)
	if err != nil || speech == nil {
		t.Fatal("speech missing")
	}
	id, err := store.ResolveTextRole(context.Background(), "mainline_angle_plan")
	if err != nil {
		t.Fatal(err)
	}
	planner, err := store.ResolveSpeechModel(context.Background(), id)
	if err != nil || planner == nil {
		t.Fatal("planner missing")
	}
	t.Setenv("ANCHOR_STYLE_ANGLE_HOOK_PLAN_ENABLED", "1")
	rows := make([]map[string]any, len(input))
	var wg sync.WaitGroup
	for i, item := range input {
		var fixed anchorcycle.AngleHook
		for _, r := range prior.Rows {
			if r.Name == item.Name && r.Variant == "accumulating_directions" {
				fixed = r.Plan.Cards[0]
			}
		}
		if fixed.ID == "" {
			t.Fatal("fixed direction missing")
		}
		wg.Add(1)
		go func(i int, name string, state anchorcycle.State, fixed anchorcycle.AngleHook) {
			defer wg.Done()
			server := &Server{store: store}
			state.PlanID = 0
			state.RunID = "offline-angle-expansion-wording-" + anchorcycle.ID()
			state.Accepted = nil
			state.Attempts = nil
			state.Used = map[string]anchorcycle.Use{}
			state.TextModel = speech
			state.Config.TextProfileID = speech.ID
			state.P0PolicyMD = ""
			if state.InstallP0(policy, source) != nil {
				t.Errorf("P0 unavailable")
				return
			}
			state.AnglePlanMotherSHA = derivedHash(mother + "\n" + planner.ID + ":" + planner.Model + ":" + planner.BaseURL + ":" + planner.SystemPrompt)
			plan := &anchorcycle.AnglePlan{Goal: state.Task().SellingPoint, Cards: []anchorcycle.AngleHook{fixed}, SourceSHA: state.AnglePlanKey(), PromptSHA: state.AnglePlanMotherSHA}
			state.AnglePlans = map[string]*anchorcycle.AnglePlan{plan.SourceSHA: plan}
			row := map[string]any{"name": name, "run_id": state.RunID, "fixed_direction": fixed}
			rows[i] = row
			returns := []map[string]any{}
			for visit := 0; visit < 3; visit++ {
				response, messages, e := server.completeCycleText(context.Background(), &state)
				r := map[string]any{"visit": visit + 1, "response": response, "messages": messages, "request_success": e == nil, "method": cycleExpressionMethod(&state)}
				returns = append(returns, r)
				row["texts"] = returns
				if e != nil {
					r["error"] = e.Error()
					t.Errorf("wording request failed")
					continue
				}
				if state.CurrentAngleHook().ID != fixed.ID {
					t.Errorf("fixed direction changed")
				}
				candidate := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: response.Text, OutputText: response.Text, AnglePlan: plan, AngleHook: &fixed}
				cleanCycleOpeningInstruction(&state, &candidate, "")
				if e = state.CheckP0Candidate(&candidate); e != nil {
					t.Errorf("P0 scan failed")
					continue
				}
				r["p0_report"], r["final_text"] = candidate.P0Report, candidate.OutputText
				r["exceeds_requested_max_chars"] = len([]rune(candidate.OutputText)) > state.Config.MaxChars
				if candidate.P0Report.Passed {
					state.Accepted = append(state.Accepted, anchorcycle.Segment{Task: state.Task(), Selected: candidate})
				} else {
					state.Attempts = append(state.Attempts, candidate)
				}
			}
		}(i, item.Name, item.State, fixed)
	}
	wg.Wait()
	raw, _ = json.MarshalIndent(map[string]any{"request_test_passed": !t.Failed(), "model": speech.Model, "text_only": true, "plan_assets_unchanged": true, "p0_policy_sha": derivedHash(policy), "rows": rows}, "", "  ")
	if err = os.WriteFile(os.Getenv("ANGLE_WORDING_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}
