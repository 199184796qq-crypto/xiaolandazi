package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"os"
	"sync"
	"testing"
	"time"
)

// Explicit paid, offline cross-category replay. No business adoption, speech,
// playback, live queue, customer source data or plan version is changed.
func TestNaturalCorrectionCrossCategoryProvider(t *testing.T) {
	if os.Getenv("CONTENT_CORRECTION_PROVIDER_PROBE") != "1" {
		t.Skip("explicit paid offline probe required")
	}
	var cases []struct{ Name, Product, Fact, Angle, Feedback string }
	raw, e := os.ReadFile(os.Getenv("CONTENT_CORRECTION_PROBE_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	store, e := appdb.Open(config.Load())
	if e != nil {
		t.Fatal("model configuration unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14})
	rows := make([]map[string]any, len(cases))
	limit := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for i, c := range cases {
		wg.Add(1)
		go func(i int, c struct{ Name, Product, Fact, Angle, Feedback string }) {
			defer wg.Done()
			limit <- struct{}{}
			defer func() { <-limit }()
			row := map[string]any{"name": c.Name, "product": c.Product, "original_fact": c.Fact, "original_angle": c.Angle, "user_feedback": c.Feedback}
			rows[i] = row
			source := contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1", Name: c.Product}}, Rows: []contentpools.SourceRow{{ID: "program-source", Fields: map[string]string{"事实": c.Fact}}}}
			set := contentpools.Set{ID: 1, Sources: source, SourceSHA: source.SHA(), SafeFacts: c.Fact, Pools: []contentpools.Pool{{Key: "program-pool", Owner: 1, Products: []int64{1}, Facts: []contentpools.Fact{{ID: "program-fact", Text: c.Fact, Products: []int64{1}, Sources: []string{"program-source"}}}, Tasks: []contentpools.Task{{ID: "program-task", Point: "本次沟通任务", Angle: c.Angle, Sources: []string{"program-fact"}}}}}}
			correction, facts, e := bindContentCorrection(set, contentCorrectionInput{ExpectedSetID: 1, PoolKey: "program-pool", TaskID: "program-task", Feedback: c.Feedback})
			if e != nil {
				row["error"] = e.Error()
				return
			}
			correction.Content, e = s.normalizeContentCorrection(ctx, correction, facts, source)
			row["normalized"] = correction.Content
			if e != nil {
				row["error"] = e.Error()
				return
			}
			if correction.Content.Question != "" {
				row["clarification_only"] = true
				return
			}
			next, e := s.repairCorrectionSet(ctx, set, correction, nil)
			if e != nil {
				row["error"] = e.Error()
				return
			}
			row["repaired_set"] = next
			if !correction.Content.Persistent() {
				row["cancellation_only"] = true
				return
			}
			tasks, _, e := contentPoolRuntime(next, anchorcycle.ProductPolicy{})
			if e != nil {
				row["error"] = e.Error()
				return
			}
			if len(tasks) == 0 {
				row["no_selectable_task"] = true
				return
			}
			state := cycleTestState(t)
			state.Queue = tasks
			state.Cursor = 0
			state.TaskIndex = 0
			state.BaselineJSON = textTestBaseline()
			state.Config.ComplianceScope = ""
			state.Config.MaxChars = 220
			state.Facts = next.SafeFacts
			if e = installP0Mother(&state); e != nil {
				row["error"] = "P0 unavailable"
				return
			}
			row["p0_sha256"] = derivedHash(state.P0PolicyMD)
			s.ensureCycleAnglePlan(ctx, &state, agentgateway.SpeechModel{})
			row["direction_plan"] = state.CurrentAnglePlan()
			outputs := []string{}
			for n := 0; n < 2; n++ {
				messages, e := renderCycleTextMessages(&state)
				if e != nil {
					row["error"] = e.Error()
					return
				}
				res, _, e := s.completeAnchorStyleWorkbench(ctx, "", "mainline_text", agentgateway.ResponseText, messages, 1200, false)
				if e != nil {
					row["error"] = e.Error()
					return
				}
				outputs = append(outputs, res.Text)
				state.CycleNumber++
			}
			row["writer_outputs"] = outputs
		}(i, c)
	}
	wg.Wait()
	raw, e = json.MarshalIndent(map[string]any{"rows": rows, "offline": true, "business_assets_adopted": false, "p0_always_included": true}, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("CONTENT_CORRECTION_PROBE_OUTPUT"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	for _, r := range rows {
		if r["error"] != nil {
			t.Errorf("case %s failed: %v", r["name"], r["error"])
		}
	}
}
