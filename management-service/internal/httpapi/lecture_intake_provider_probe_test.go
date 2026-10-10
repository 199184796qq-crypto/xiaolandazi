package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

// Explicit provider replay. No library/fact adoption, plan save or live start.
func TestLectureProviderIntakePreview(t *testing.T) {
	if os.Getenv("LECTURE_FLOW_DEMO") != "1" {
		t.Skip("explicit paid provider replay required")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	s := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: 14, PlanID: 4, RoomID: 26})
	p0, p1, p2, err := loadRiskAnnotationMothers()
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.loadDerivedInput(ctx, 14, 4, p0, p1, p2)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _, err := s.tenantFactCategoryCatalog(ctx, 14)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	profile := settings.SafetyProfileID
	if profile == "" {
		profile = settings.SellingPointProfileID
	}
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_LECTURE_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/lecture-source-review.md", "故事审核母版")
	if err != nil {
		t.Fatal(err)
	}
	clean := "杨鸭子用的土麻鸭产自四川南充。嘉陵江穿城而过，丘陵水田、气候温润。南充本来就是吃鸭的老地方，本地水土、本地鸭子、本地老卤，做出地道的杨鸭子。"
	for _, input := range []struct {
		source string
		passed bool
	}{{"这鸭子包治百病，绝对治糖尿病。", false}, {clean, true}} {
		payload, _ := json.Marshal(map[string]any{"business_scope": "当前方案的商品素材", "current_rules": []any{generationP0View(p0, "*"), p1, p2}, "current_facts": current.Facts})
		userSource, _ := json.Marshal(map[string]any{"source_text": input.source})
		result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "lecture_source_review", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}, {Role: "user", Content: string(userSource)}}, 2600, false)
		if err != nil {
			t.Fatal(err)
		}
		review, err := parseLectureReview(result.Text, input.source)
		if err != nil {
			t.Fatal(err, result.Text)
		}
		if *review.Passed != input.passed {
			t.Fatal("unexpected review", result.Text)
		}
		t.Logf("source review passed=%v issues=%d", *review.Passed, len(review.Issues))
	}
	mother, _, err = loadReplaceablePromptAsset("ANCHOR_LECTURE_ORGANIZE_PROMPT_PATH", "configs/prompts/anchor-style/lecture-source-organize.md", "故事整理母版")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"source_text": clean, "available_fact_categories": catalog.Categories, "effective_p0": generationP0View(p0, "*")})
	result, _, err := s.completeAnchorStyleWorkbench(ctx, profile, "lecture_source_organize", agentgateway.ResponseJSON, []agentgateway.Message{{Role: "system", Content: mother}, {Role: "user", Content: string(payload)}}, 4000, false)
	if err != nil {
		t.Fatal(err)
	}
	task, facts, err := parseLectureOrganized(result.Text, clean, catalog)
	if err != nil {
		t.Fatal(err, result.Text)
	}
	t.Logf("organized title=%s facts=%d angles=%d related_question_terms=%d", task.Title, len(facts), len(task.Angles), len(task.QuestionTerms))
}
