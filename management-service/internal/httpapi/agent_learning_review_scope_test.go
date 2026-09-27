package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestApplyAgentLearningReviewCorrectionStructureCannotRewriteBusinessText(t *testing.T) {
	candidate := agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeFact,
		Target:     "价格状态",
		MemoryKey:  "price_status",
		ResultText: "价格等下就开。",
		Structured: map[string]any{"fact_key": "price_status", "value": "价格等下就开"},
	}
	review := agentLearningReviewOutput{
		Action:          "auto_correct",
		Confidence:      "high",
		CorrectionScope: "structure",
		Corrected: agentLearningModelOutput{
			MemoryType: model.AgentMemoryTypeSemantic,
			Target:     "被审校员错误改写的主题",
			MemoryKey:  "wrong_key",
			ResultText: "价格是39.9。",
			Structured: map[string]any{"fact_key": "price_status", "value": "价格等下就开", "source": "human_confirmed"},
		},
	}
	got, applied := applyAgentLearningReviewCorrection(model.AgentLearningSession{}, "价格等下就开", candidate, review)
	if !applied {
		t.Fatal("safe structure correction should apply")
	}
	if got.ResultText != candidate.ResultText || got.MemoryType != candidate.MemoryType || got.Target != candidate.Target || got.MemoryKey != candidate.MemoryKey {
		t.Fatalf("structure review rewrote business meaning: %#v", got)
	}
	if got.Structured["source"] != "human_confirmed" {
		t.Fatalf("structure update not applied: %#v", got.Structured)
	}
}

func TestApplyAgentLearningReviewCorrectionDuplicateMatchOnlyChangesLinkage(t *testing.T) {
	candidate := agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeSemantic,
		Target:     "价格回答方式",
		MemoryKey:  "semantic:price_answer",
		ResultText: "有人问价格时，说价格等下公布。",
		Structured: map[string]any{"semantic_mode": "response_strategy"},
	}
	review := agentLearningReviewOutput{
		Action:          "auto_correct",
		Confidence:      "high",
		CorrectionScope: "duplicate_match",
		Corrected: agentLearningModelOutput{
			MemoryType:          model.AgentMemoryTypeFact,
			Target:              "错误改写",
			MemoryKey:           "semantic:price",
			MatchedMemoryItemID: 88,
			ResultText:          "价格39.9。",
		},
	}
	got, applied := applyAgentLearningReviewCorrection(model.AgentLearningSession{}, "以后有人问价格，先说价格等下公布", candidate, review)
	if !applied {
		t.Fatal("safe duplicate-match correction should apply")
	}
	if got.MatchedMemoryItemID != 88 || got.MemoryKey != "semantic:price" {
		t.Fatalf("duplicate linkage not applied: %#v", got)
	}
	if got.ResultText != candidate.ResultText || got.MemoryType != candidate.MemoryType || got.Target != candidate.Target {
		t.Fatalf("duplicate review rewrote business content: %#v", got)
	}
}
