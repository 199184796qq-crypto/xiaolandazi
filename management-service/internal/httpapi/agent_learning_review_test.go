package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestCanAutoApplyAgentLearningReviewOnlyAllowsSafeHighConfidenceScopes(t *testing.T) {
	allowed := []string{"classification", "structure", "duplicate_match"}
	for _, scope := range allowed {
		if !canAutoApplyAgentLearningReview(agentLearningReviewOutput{
			Action: "auto_correct", Confidence: "high", CorrectionScope: scope,
		}) {
			t.Fatalf("safe high-confidence scope %q should auto apply", scope)
		}
	}

	blocked := []agentLearningReviewOutput{
		{Action: "auto_correct", Confidence: "medium", CorrectionScope: "classification"},
		{Action: "auto_correct", Confidence: "high", CorrectionScope: "business_fact"},
		{Action: "needs_confirmation", Confidence: "high", CorrectionScope: "business_fact"},
		{Action: "accept", Confidence: "high", CorrectionScope: "structure"},
	}
	for _, review := range blocked {
		if canAutoApplyAgentLearningReview(review) {
			t.Fatalf("unsafe review should not auto apply: %+v", review)
		}
	}
}

func TestNormalizeAgentLearningReviewFailsSafe(t *testing.T) {
	got := normalizeAgentLearningReview(agentLearningReviewOutput{
		Action: "invent", Confidence: "certain", CorrectionScope: " BUSINESS_FACT ", Reason: "  conflicting fact  ",
	})
	if got.Action != "accept" {
		t.Fatalf("invalid action normalized to %q, want accept", got.Action)
	}
	if got.Confidence != "low" {
		t.Fatalf("invalid confidence normalized to %q, want low", got.Confidence)
	}
	if got.CorrectionScope != "business_fact" {
		t.Fatalf("scope normalized to %q, want business_fact", got.CorrectionScope)
	}
	if got.Reason != "conflicting fact" {
		t.Fatalf("reason=%q", got.Reason)
	}
}

func TestAttachAgentLearningReviewKeepsMachineAuditTrail(t *testing.T) {
	candidate := agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeSemantic,
		ResultText: "回答这类问题时多举几个例子。",
		Structured: map[string]any{"semantic_mode": "response_strategy"},
	}
	review := agentLearningReviewOutput{
		Action: "auto_correct", Reason: "事实误归类为回答策略", Confidence: "high", CorrectionScope: "classification",
	}
	got := attachAgentLearningReview(candidate, review, true)
	raw, ok := got.Structured["auto_review"].(map[string]any)
	if !ok {
		t.Fatal("auto_review audit metadata missing")
	}
	if raw["applied"] != true || raw["action"] != "auto_correct" || raw["correction_scope"] != "classification" {
		t.Fatalf("unexpected auto review metadata: %#v", raw)
	}
}
