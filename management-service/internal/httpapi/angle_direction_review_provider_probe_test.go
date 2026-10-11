package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

type directionReviewProbeCase struct {
	Name, Category, Identity, Facts, Direction string
	WantPass                                   bool
}

// Uses the production review entrypoint and configured audit role. Synthetic
// products never enter business pools, speech queues, TTS or playback.
func TestAngleDirectionReviewCrossCategoryProvider(t *testing.T) {
	if os.Getenv("ANGLE_DIRECTION_REVIEW_PROBE") != "1" {
		t.Skip("explicit paid offline direction audit experiment")
	}
	raw, err := os.ReadFile(os.Getenv("ANGLE_DIRECTION_REVIEW_INPUT"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []directionReviewProbeCase
	if err = json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 || len(cases) > 40 {
		t.Fatal("invalid explicit fixtures", err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	modelID, err := store.ResolveTextRole(ctx, "angle_direction_review")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := store.ResolveSpeechModel(ctx, modelID)
	if err != nil || profile == nil || !profile.Enabled {
		t.Fatal("current audit role unavailable")
	}
	p0, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	mother, _, err := loadReplaceablePromptAsset("ANGLE_DIRECTION_REVIEW_PROMPT_PATH", "configs/prompts/anchor-style/angle-direction-review.md", "方向审核母版")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{store: store}
	rows := []map[string]any{}
	archive := func() {
		b, e := json.MarshalIndent(map[string]any{"model": profile.Model, "role": "angle_direction_review", "p0_source": p0, "mother": mother, "rows": rows, "offline": true, "business_pool_changed": false, "tts": false, "played": false}, "", "  ")
		if e == nil {
			e = os.WriteFile(os.Getenv("ANGLE_DIRECTION_REVIEW_OUTPUT"), b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}
	defer archive()
	for i, fixture := range cases {
		task := anchorcycle.Task{Key: fmt.Sprint("synthetic-", i), SellingPoint: fixture.Category, Angle: "按本次资料展开讲解", AllowedLinksJSON: `["1"]`, FactScope: &anchorcycle.TaskFactScope{Facts: []anchorcycle.HookFact{{ID: "program-fact", Products: []string{"1"}, Data: fixture.Facts}}, ProductBoundary: []anchorcycle.HookFact{{ID: "program-boundary", Products: []string{"1"}, Data: fixture.Identity}}}}
		state := &anchorcycle.State{TenantID: 14, Surface: factPreviewSurface, Queue: []anchorcycle.Task{task}, ReviewModel: profile, P0PolicyMD: p0}
		if fixture.Category == "熟食" || fixture.Category == "咖啡豆" {
			state.Config.ComplianceScope = "ordinary_food"
		}
		card := anchorcycle.AngleHook{ID: fmt.Sprint("program-card-", i), Action: fixture.Direction}
		plan := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{card}, GeneratedCards: []anchorcycle.AngleHook{card}, Raw: fixture.Direction}
		messages, e := angleDirectionReviewMessages(state, card, mother)
		if e != nil {
			t.Fatal(e)
		}
		started := time.Now()
		server.reviewCycleAngleDirections(ctx, state, plan)
		ms := time.Since(started).Milliseconds()
		passed := len(plan.Cards) == 1
		row := map[string]any{"case": fixture, "messages": messages, "plan": plan, "passed": passed, "expected_match": passed == fixture.WantPass, "full_ms": ms}
		if len(plan.DirectionReviews) == 1 {
			receipt := plan.DirectionReviews[0]
			server.reviewCycleAngleDirections(ctx, state, plan)
			row["repeat_reused"] = len(plan.DirectionReviews) == 1 && plan.DirectionReviews[0].ExchangeID == receipt.ExchangeID
			if receipt.Completed() {
				cloned := card
				cloned.ID += "-new-generation"
				fresh := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{cloned}}
				calls := 0
				filterAngleDirectionsWithReview(ctx, fresh, plan.ReviewContextSHA, []anchorcycle.DirectionReviewReceipt{receipt}, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
					calls++
					return anchorcycle.DirectionReviewReceipt{}
				})
				row["new_generation_cache_calls"] = calls
				row["new_generation_kept"] = len(fresh.Cards)
			}
		}
		rows = append(rows, row)
		archive()
		t.Logf("%s expected=%v selected=%v model=%s duration_ms=%d", fixture.Name, fixture.WantPass, passed, profile.Model, ms)
	}
}
