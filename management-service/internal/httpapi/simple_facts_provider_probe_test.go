package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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

// Isolated, explicitly paid comparison. These requests intentionally omit
// the production role/style/history/working-card stack. No live or plan writes.
func TestSimpleFactsProviderProbe(t *testing.T) {
	if os.Getenv("SIMPLE_FACTS_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid comparison required")
	}
	var cases []struct {
		Name  string `json:"name"`
		Task  string `json:"task"`
		Facts string `json:"facts"`
	}
	raw, err := os.ReadFile(os.Getenv("SIMPLE_FACTS_PROBE_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	mother := os.Getenv("SIMPLE_FACTS_PROBE_PROMPT")
	if mother == "" {
		mother = "text-simple-facts-probe.md"
	}
	base, err := interactionpolicy.Read(mother)
	if err != nil {
		t.Fatal(err)
	}
	role, err := interactionpolicy.Read("text-simple-facts-probe-role.md")
	if err != nil {
		t.Fatal(err)
	}
	// P0 is mandatory in EVERY probe, including short prompts and comparisons.
	// Earlier experiments without it remain historical raw data only.
	policyState := anchorcycle.State{Config: anchorcycle.Config{ComplianceScope: "ordinary_food"}}
	if err := installP0Mother(&policyState); err != nil {
		t.Fatal("P0 policy unavailable")
	}
	p0, err := json.Marshal(generationP0View(policyState.P0PolicyMD, "ordinary_food"))
	if err != nil {
		t.Fatal(err)
	}
	p0Constraints := "\n\n本次有效P0：\n" + string(p0)
	p0SHA := derivedHash(policyState.P0PolicyMD)
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(ctx, settings.Cycle.TextProfileID)
	if err != nil || profile == nil {
		t.Fatal("speech profile unavailable")
	}
	server := &Server{store: store}
	temperature := 0.25
	variants := 2
	if os.Getenv("SIMPLE_FACTS_PROBE_NO_ROLE_ONLY") == "1" {
		variants = 1
	}
	rows := make([]map[string]any, len(cases)*variants*3)
	limit := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, c := range cases {
		for variant := 0; variant < variants; variant++ {
			for attempt := 0; attempt < 3; attempt++ {
				index, name, job, facts := (i*variants+variant)*3+attempt, c.Name, c.Task, c.Facts
				variant, attempt := variant, attempt
				wg.Add(1)
				go func() {
					defer wg.Done()
					limit <- struct{}{}
					defer func() { <-limit }()
					prompt, variantName := string(base)+p0Constraints, "仅短提示"
					if variant == 1 {
						prompt, variantName = string(role)+"\n"+prompt, "短提示加原角色"
					}
					request := agentgateway.Request{Stage: "isolated_simple_fact_text", Model: profile.Model,
						Messages:       []agentgateway.Message{{Role: "system", Content: prompt}, {Role: "user", Content: "本次任务：" + job + "\n本次事实：\n" + facts}},
						ResponseFormat: agentgateway.ResponseText, EnableThinking: false, Temperature: &temperature, MaxTokens: 450, Timeout: 25 * time.Second}
					prepared, e := agentgateway.PrepareTextRequest(*profile, request)
					if e != nil || len(prepared.Messages) != 2 {
						t.Errorf("probe acquired production prompt stack")
						return
					}
					runID := "offline-simple-facts-" + anchorcycle.ID()
					callCtx := withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 0, RunID: runID})
					exchangeID, e := server.beginModelExchange(callCtx, request.Stage, profile.ID, request)
					if e != nil {
						t.Errorf("archive unavailable")
						return
					}
					started := time.Now()
					response, e := agentgateway.CompleteSpeechEndpoint(callCtx, *profile, request)
					response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
					e = server.finishModelExchange(callCtx, exchangeID, response, response.Text, started, response.InputTokens, response.OutputTokens, e)
					row := map[string]any{"name": name, "variant": variantName, "attempt": attempt + 1, "exchange_id": exchangeID, "run_id": runID, "request": request, "response": response, "request_success": e == nil}
					if e != nil {
						row["error"] = e.Error()
						t.Errorf("%s/%s/%d request failed", name, variantName, attempt+1)
					}
					rows[index] = row
				}()
			}
		}
	}
	wg.Wait()
	result := map[string]any{"request_test_passed": !t.Failed(), "model": profile.Model, "text_only": true, "plan_assets_unchanged": true,
		"sampling_temperature": temperature, "system_prompt": string(base), "role_comparison": string(role), "variant_count": variants,
		"p0_constraints_present": p0Constraints != "", "p0_policy_sha": p0SHA, "p0_prompt": p0Constraints, "rows": rows}
	raw, _ = json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(os.Getenv("SIMPLE_FACTS_PROBE_OUTPUT"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("Saved %d isolated requests; content is retained for review, not graded by this protocol test.\n", len(rows))
}
