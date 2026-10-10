package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
)

// Explicit offline replay: records model exchanges, never saves a content set,
// changes source records, publishes a plan, queues speech or calls TTS.
func TestContentPoolWireProviderReplay(t *testing.T) {
	if os.Getenv("CONTENT_POOL_WIRE_REPLAY") != "1" {
		t.Skip("explicit reference protocol replay required")
	}
	var fixture struct {
		Pool        contentpools.Pool      `json:"pool"`
		Previous    []contentpools.Task    `json:"previous"`
		Products    []contentpools.Product `json:"products"`
		FailedDraft string                 `json:"failed_draft"`
	}
	raw, err := os.ReadFile(os.Getenv("CONTENT_POOL_WIRE_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 4, RunID: "offline-content-summary-wire-replay"})
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	gen, _, err := loadReplaceablePromptAsset("CONTENT_POOL_TASK_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-generation.md", "内容池生成")
	if err != nil {
		t.Fatal(err)
	}
	review, _, err := loadReplaceablePromptAsset("CONTENT_POOL_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-review.md", "内容池复核")
	if err != nil {
		t.Fatal(err)
	}
	outputs := []map[string]any{}
	defer func() {
		if path := os.Getenv("CONTENT_POOL_WIRE_RESULT"); path != "" {
			data, _ := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "outputs": outputs, "adopted": false}, "", "  ")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	// The actual empty-reference failure is repaired by the model, not by
	// guessing a source for an unsupported task in application code.
	p := fixture.Pool
	p.Tasks = nil
	calls := 0
	err = s.reviewContentPoolTaskDraft(ctx, cfg.SellingPointProfileID, review, &p, fixture.Previous, fixture.Products, fixture.FailedDraft, func() error { calls++; return nil })
	outputs = append(outputs, map[string]any{"trial": "actual_failed_draft", "tasks": p.Tasks, "calls": calls, "success": err == nil})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		p = fixture.Pool
		p.Tasks = nil
		wire := newContentPoolWire(p)
		input, _ := json.Marshal(map[string]any{"pool": wire.pool, "products": fixture.Products, "existing_tasks": wire.tasks(fixture.Previous)})
		response, _, e := s.completeAnchorStyleWorkbench(ctx, cfg.SellingPointProfileID, "content_pool_task_generation", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: gen}, {Role: "user", Content: string(input)}}, 12000, false)
		if e != nil {
			t.Fatal(e)
		}
		outputs = append(outputs, map[string]any{"trial": "new_generation", "iteration": i + 1, "raw": response.Text})
		calls = 0
		e = s.reviewContentPoolTaskDraft(ctx, cfg.SellingPointProfileID, review, &p, fixture.Previous, fixture.Products, wire.response(response.Text, true), func() error { calls++; return nil })
		outputs = append(outputs, map[string]any{"trial": "generation_and_review", "iteration": i + 1, "tasks": p.Tasks, "calls": calls, "success": e == nil})
		if e != nil {
			t.Fatal(e)
		}
	}
}
