package httpapi

import (
	"testing"

	"livecompanion/management/internal/agentmemory"
	"livecompanion/management/internal/model"
)

func stableLocationMemory(id int64, value string) model.AgentMemoryItem {
	return model.AgentMemoryItem{
		ID:         id,
		MemoryType: model.AgentMemoryTypeFact,
		MemoryKey:  "location",
		Target:     "所在地",
		Status:     model.AgentMemoryStatusActive,
		CurrentVersion: &model.AgentMemoryVersion{
			ContentText: "我们这边是" + value + "的。",
			Structured: map[string]any{
				"fact_key": "location",
				"value":    value,
				"source":   "human_confirmed",
			},
		},
	}
}

func locationCandidate(value string) agentLearningModelOutput {
	return agentLearningModelOutput{
		MemoryType: model.AgentMemoryTypeFact,
		Target:     "所在地",
		MemoryKey:  "location",
		ResultText: "我们这边是" + value + "的。",
		Structured: map[string]any{
			"fact_key": "location",
			"value":    value,
			"source":   "human_confirmed",
		},
	}
}

func strongLocationStats(value string) []model.AgentMemoryEvidenceStat {
	return []model.AgentMemoryEvidenceStat{
		{
			ValueText:               value,
			ValueSignature:          agentmemory.EvidenceSignature(value),
			OccurrenceCount:         20,
			ConsecutiveCount:        8,
			ExplicitCorrectionCount: 1,
			AdoptedCount:            2,
		},
	}
}

func TestHistoricalEvidenceGuardKeepsStableUserValueOnOneOffMismatch(t *testing.T) {
	memories := []model.AgentMemoryItem{stableLocationMemory(7, "绵阳山河")}
	got := applyHistoricalEvidenceGuard(
		"我们是绵阳三河",
		locationCandidate("绵阳三河"),
		memories,
		strongLocationStats("绵阳山河"),
	)
	if got.MatchedMemoryItemID != 7 {
		t.Fatalf("matched memory=%d, want stable memory 7", got.MatchedMemoryItemID)
	}
	if got.Structured["value"] != "绵阳山河" {
		t.Fatalf("stable fact not preserved: %#v", got.Structured)
	}
	meta, _ := got.Structured["historical_evidence"].(map[string]any)
	if meta["decision"] != "kept_stable_user_value" {
		t.Fatalf("decision=%v", meta["decision"])
	}
}

func TestHistoricalEvidenceGuardExplicitCorrectionCanReplaceFrequentValue(t *testing.T) {
	candidate := locationCandidate("绵阳三河")
	got := applyHistoricalEvidenceGuard(
		"我刚才写错了，现在改成绵阳三河",
		candidate,
		[]model.AgentMemoryItem{stableLocationMemory(7, "绵阳山河")},
		strongLocationStats("绵阳山河"),
	)
	if got.Structured["value"] != "绵阳三河" {
		t.Fatalf("explicit correction was overwritten: %#v", got.Structured)
	}
	meta, _ := got.Structured["historical_evidence"].(map[string]any)
	if meta["decision"] != "explicit_correction_overrides_frequency" {
		t.Fatalf("decision=%v", meta["decision"])
	}
}

func TestHistoricalEvidenceGuardWeakHistoryDoesNotOverride(t *testing.T) {
	weak := []model.AgentMemoryEvidenceStat{
		{
			ValueText:        "绵阳山河",
			ValueSignature:   agentmemory.EvidenceSignature("绵阳山河"),
			OccurrenceCount:  2,
			ConsecutiveCount: 2,
		},
	}
	got := applyHistoricalEvidenceGuard(
		"我们是绵阳三河",
		locationCandidate("绵阳三河"),
		[]model.AgentMemoryItem{stableLocationMemory(7, "绵阳山河")},
		weak,
	)
	if got.Structured["value"] != "绵阳三河" {
		t.Fatalf("weak history should not override candidate: %#v", got.Structured)
	}
}
