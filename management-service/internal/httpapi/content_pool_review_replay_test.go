package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
)

// Opt-in replay of the actual failed candidate. Records provider traces only;
// never creates/claims a job, saves a pool set, publishes a version or starts audio.
func TestContentPoolReviewReplay(t *testing.T) {
	if os.Getenv("CONTENT_POOL_REVIEW_REPLAY") != "1" {
		t.Skip("explicit failed-candidate replay required")
	}
	raw, err := os.ReadFile(os.Getenv("CONTENT_POOL_REPLAY_FIXTURE"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Pool     contentpools.Pool      `json:"pool"`
		Previous []contentpools.Task    `json:"previous"`
		Products []contentpools.Product `json:"products"`
		Response json.RawMessage        `json:"response"`
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("DB unavailable")
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 4, RunID: "content-pool-structure-only-replay"})
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mother, _, err := loadReplaceablePromptAsset("CONTENT_POOL_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/content-pool-task-review.md", "内容池复核母版")
	if err != nil {
		t.Fatal(err)
	}
	outputs := []map[string]any{}
	defer func() {
		if path := os.Getenv("CONTENT_POOL_REPLAY_RESULT"); path != "" {
			out, _ := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "outputs": outputs}, "", "  ")
			if err := os.WriteFile(path, out, 0600); err != nil {
				t.Error(err)
			}
		}
	}()
	var decoded map[string]any
	if err = json.Unmarshal(input.Response, &decoded); err != nil {
		t.Fatal(err)
	}
	brokenRaw, _ := json.Marshal(decoded)
	var broken map[string]any
	_ = json.Unmarshal(brokenRaw, &broken)
	broken["tasks"].([]any)[0].(map[string]any)["source_ids"] = []string{"unknown-source"}
	brokenRaw, _ = json.Marshal(broken)
	for _, trial := range []struct{ Name, Draft string }{{"actual_below_50", string(input.Response)}, {"missing_reference", string(brokenRaw)}, {"wrong_amount_model_correction", strings.Replace(string(input.Response), "48.8元", "28.8元", 1)}} {
		pool := input.Pool
		pool.Tasks = nil
		calls := 0
		started := time.Now()
		err = s.reviewContentPoolTaskDraft(ctx, cfg.SellingPointProfileID, mother, &pool, input.Previous, input.Products, trial.Draft, func() error { calls++; return nil })
		outputs = append(outputs, map[string]any{"trial": trial.Name, "tasks": pool.Tasks, "calls": calls, "elapsed_ms": time.Since(started).Milliseconds()})
		if err != nil {
			t.Fatal(trial.Name, err)
		}
		if len(pool.Tasks) == 0 || calls < 1 || calls > 2 {
			t.Fatal("bounded model review did not run")
		}
		if trial.Name == "wrong_amount_model_correction" {
			result, _ := json.Marshal(pool.Tasks)
			if strings.Contains(string(result), "28.8") {
				t.Fatal("model did not correct fabricated amount")
			}
		}
		t.Logf("trial=%s tasks=%d review_calls=%d elapsed_ms=%d structure_only=true thinking=false no_adoption=true", trial.Name, len(pool.Tasks), calls, time.Since(started).Milliseconds())
	}
}
