package agentmemory

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestEvidenceValueFactUsesConfirmedValue(t *testing.T) {
	got := EvidenceValue(model.AgentMemoryTypeFact, "我们在绵阳山河。", map[string]any{
		"fact_key": "location",
		"value":    "绵阳山河",
	})
	if got != "绵阳山河" {
		t.Fatalf("EvidenceValue=%q, want 绵阳山河", got)
	}
}

func TestObservedEvidenceValuePrefersUserObservation(t *testing.T) {
	got := ObservedEvidenceValue(model.AgentMemoryTypeFact, "系统最终保护为绵阳山河", map[string]any{
		"value": "绵阳山河",
		"user_evidence_observation": map[string]any{
			"value": "绵阳三河",
		},
	})
	if got != "绵阳三河" {
		t.Fatalf("ObservedEvidenceValue=%q, want 绵阳三河", got)
	}
}

func TestLooksLikeExplicitCorrection(t *testing.T) {
	positive := []string{
		"我刚才打错了，应该是绵阳山河",
		"不是绵阳三河，是绵阳山河",
		"以后统一改成绵阳山河",
	}
	for _, value := range positive {
		if !LooksLikeExplicitCorrection(value) {
			t.Fatalf("%q should be explicit correction", value)
		}
	}
	if LooksLikeExplicitCorrection("我们是绵阳山河") {
		t.Fatal("plain fact statement should not be explicit correction")
	}
}

func TestStrongDominantEvidenceProtectsStableValueFromOneOff(t *testing.T) {
	stableValue := "绵阳山河"
	candidateValue := "绵阳三河"
	stats := []model.AgentMemoryEvidenceStat{
		{
			ValueText:               stableValue,
			ValueSignature:          EvidenceSignature(stableValue),
			OccurrenceCount:         20,
			ConsecutiveCount:        8,
			ExplicitCorrectionCount: 1,
			AdoptedCount:            2,
		},
		{
			ValueText:       candidateValue,
			ValueSignature:  EvidenceSignature(candidateValue),
			OccurrenceCount: 1,
		},
	}
	dominant, ok := StrongDominantEvidence(stats, EvidenceSignature(candidateValue), false)
	if !ok || dominant == nil || dominant.ValueText != stableValue {
		t.Fatalf("expected stable dominant value, got %#v ok=%v", dominant, ok)
	}
}

func TestStrongDominantEvidenceExplicitCorrectionOverridesHistory(t *testing.T) {
	stableValue := "绵阳山河"
	candidateValue := "绵阳三河"
	stats := []model.AgentMemoryEvidenceStat{
		{
			ValueText:        stableValue,
			ValueSignature:   EvidenceSignature(stableValue),
			OccurrenceCount:  50,
			ConsecutiveCount: 20,
			AdoptedCount:     5,
		},
	}
	if dominant, ok := StrongDominantEvidence(stats, EvidenceSignature(candidateValue), true); ok || dominant != nil {
		t.Fatalf("explicit correction must bypass frequency, got %#v ok=%v", dominant, ok)
	}
}
