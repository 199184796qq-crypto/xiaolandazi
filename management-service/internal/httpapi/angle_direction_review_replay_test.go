package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"os"
	"testing"
)

// Replays untouched provider responses through the current parser and selector.
// This is deterministic program verification, not another paid model test.
func TestDirectionReviewArchivedProviderReplay(t *testing.T) {
	path := os.Getenv("ANGLE_DIRECTION_REVIEW_REPLAY_INPUT")
	if path == "" {
		t.Skip("explicit archived provider result required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var archive struct {
		Model string `json:"model"`
		Rows  []struct {
			Case directionReviewProbeCase `json:"case"`
			Plan *anchorcycle.AnglePlan   `json:"plan"`
		} `json:"rows"`
	}
	if err = json.Unmarshal(raw, &archive); err != nil || len(archive.Rows) == 0 {
		t.Fatal("invalid archive", err)
	}
	results := []map[string]any{}
	for _, row := range archive.Rows {
		if row.Plan == nil || len(row.Plan.DirectionReviews) != 1 || len(row.Plan.ReviewCandidates) != 1 {
			t.Fatal("incomplete request archive")
		}
		original := row.Plan.DirectionReviews[0]
		r := original
		r.Decision, err = anchorcycle.ParseDirectionReview(r.Raw)
		if err != nil {
			t.Fatal(row.Case.Name, err)
		}
		r.Error = ""
		plan := &anchorcycle.AnglePlan{Cards: row.Plan.ReviewCandidates}
		filterAngleDirectionsWithReview(context.Background(), plan, row.Plan.ReviewContextSHA, nil, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt { return r })
		if (len(plan.Cards) == 1) != row.Case.WantPass || !plan.DirectionReviews[0].Completed() {
			t.Fatal("final selection mismatch", row.Case.Name)
		}
		cacheCalls := 0
		copyCard := row.Plan.ReviewCandidates[0]
		copyCard.ID += "-new-generation"
		fresh := &anchorcycle.AnglePlan{Cards: []anchorcycle.AngleHook{copyCard}}
		filterAngleDirectionsWithReview(context.Background(), fresh, row.Plan.ReviewContextSHA, plan.DirectionReviews, func(context.Context, anchorcycle.AngleHook) anchorcycle.DirectionReviewReceipt {
			cacheCalls++
			return anchorcycle.DirectionReviewReceipt{}
		})
		if cacheCalls != 0 || (len(fresh.Cards) == 1) != row.Case.WantPass {
			t.Fatal("cached result mismatch", row.Case.Name)
		}
		results = append(results, map[string]any{"case": row.Case, "original_receipt": original, "receipt": plan.DirectionReviews[0], "final_selected": len(plan.Cards), "cache_calls": cacheCalls})
	}
	b, err := json.MarshalIndent(map[string]any{"source_sha256": anchorcycle.ProductDigest(string(raw)), "model": archive.Model, "new_model_calls": 0, "mode": "unchanged raw provider response replay", "rows": results}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(os.Getenv("ANGLE_DIRECTION_REVIEW_REPLAY_OUTPUT"), b, 0600); err != nil {
		t.Fatal(err)
	}
}
