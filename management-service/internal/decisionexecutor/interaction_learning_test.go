package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"testing"
)

type learningStore struct {
	fakeStore
	observations []model.InteractionLearningObservation
}

func (s *learningStore) RecordInteractionLearningReview(_ context.Context, o model.InteractionLearningObservation) error {
	s.observations = append(s.observations, o)
	return nil
}

func TestOnlyRealQuestionGenerationCollectsReviews(t *testing.T) {
	s := &learningStore{}
	w := &Worker{store: s}
	runtime := model.LiveRuntimeSession{ID: 1, ExternalID: "s1", TenantID: 14, RoomID: 8}
	item := decisionItem{ID: "q1", MissionKind: "reply_question", SampleQuestions: []string{"不抽可以吗"}}
	if !w.shouldAssessInteraction(runtime, item) {
		t.Fatal("live question not assessed")
	}
	if w.shouldAssessInteraction(model.LiveRuntimeSession{}, item) {
		t.Fatal("test wrote production review")
	}
	manual := item
	manual.ManualOrigin = "agent_input"
	if w.shouldAssessInteraction(runtime, manual) {
		t.Fatal("operator text protocol changed")
	}
	welcome := item
	welcome.MissionKind = "welcome"
	if w.shouldAssessInteraction(runtime, welcome) {
		t.Fatal("welcome protocol changed")
	}
	w.recordInteractionReview(context.Background(), runtime, item, "可以。", nil)
	if len(s.observations) != 0 {
		t.Fatal("clear answer created review")
	}
}

func TestConfirmedSmallCorrectionTableSurvivesEmbeddingMiss(t *testing.T) {
	memories := []model.AgentMemoryItem{}
	ranks := []semanticMemoryCandidate{}
	for i := 0; i < 12; i++ {
		memories = append(memories, memoryItem(int64(i+1), model.AgentMemoryTypeSemantic, "语义"))
		ranks = append(ranks, semanticMemoryCandidate{index: i, score: 0.9 - float64(i)/20})
	}
	memories[11].CurrentVersion.Structured = map[string]any{"review_plan_id": float64(4)}
	got := selectRankedMemories(memories, ranks)
	for _, m := range got {
		if m.ID == 12 {
			return
		}
	}
	t.Fatal("user-confirmed correction dropped by embedding")
}
