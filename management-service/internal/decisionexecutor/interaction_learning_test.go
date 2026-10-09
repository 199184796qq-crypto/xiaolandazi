package decisionexecutor

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

type learningStore struct {
	fakeStore
	observations []model.InteractionLearningObservation
}

func TestLiveGenerationRecordsReviewButReturnsOnlySpeech(t *testing.T) {
	s := &learningStore{fakeStore: *readyVoiceStore()}
	a := &fakeAgent{responses: []string{`{"reply":"您是问能不能选不抽真空的包装吧？这项选择还需要确认。","needs_review":true,"review":{"kind":"missing_fact","meaning":"能否选非真空","context":"食品包装","reason":"未提供可选规则","suggested_rule":"","needed_facts":["是否支持不抽真空"]}}`, "您是问能不能选不抽真空的包装吧？这项选择还需要确认。"}}
	w := New(s, &fakeCore{}, a, &fakeTTS{})
	text, err := w.generateDecisionText(context.Background(), model.LiveRuntimeSession{ID: 1, ExternalID: "s1", TenantID: 7, RoomID: 11}, decisionItem{ID: "q1", MissionKind: "reply_question", Scene: "product_question", SampleQuestions: []string{"不抽可以吗"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "needed_facts") || strings.Contains(text, "missing_fact") || strings.HasPrefix(text, "{") || !strings.Contains(text, "不抽真空") {
		t.Fatal("metadata in speech", text)
	}
	if len(s.observations) != 1 || s.observations[0].Review.PlanID != 3 || s.observations[0].Question != "不抽可以吗" || s.observations[0].Reply != text {
		t.Fatal(s.observations)
	}
	if a.calls > 2 {
		t.Fatal("extra analysis request", a.calls)
	}
}

func (s *learningStore) RecordInteractionLearningReview(_ context.Context, o model.InteractionLearningObservation) error {
	s.observations = append(s.observations, o)
	return nil
}

func TestAdvisoryReviewDoesNotRewriteOrDelayConfirmedFact(t *testing.T) {
	const speech = "咱们默认抽真空，开袋可以直接吃。"
	baselineCalls := 0
	for i, raw := range []string{
		`{"reply":"咱们默认抽真空，开袋可以直接吃。","needs_review":false,"review":null}`,
		`{"reply":"咱们默认抽真空，开袋可以直接吃。","needs_review":true,"review":{"kind":"missing_fact","meaning":"默认包装","context":"食品包装","reason":"模型怀疑默认方式","needed_facts":["默认包装方式"]}}`,
		`{"reply":"咱们默认抽真空，开袋可以直接吃。","needs_review":true,"review":{"kind":"unsupported"}}`,
	} {
		s := &learningStore{fakeStore: *readyVoiceStore()}
		a := &fakeAgent{responses: []string{raw, speech}}
		w := New(s, &fakeCore{}, a, &fakeTTS{})
		got, err := w.generateDecisionText(context.Background(), model.LiveRuntimeSession{ID: 1, ExternalID: "s1", TenantID: 7, RoomID: 11}, decisionItem{ID: "q1", MissionKind: "reply_question", SampleQuestions: []string{"抽不抽真空"}})
		if err != nil || strings.TrimRight(got, "。") != strings.TrimRight(speech, "。") {
			t.Fatalf("advisory changed or vetoed fact: %q %v", got, err)
		}
		if i == 0 {
			baselineCalls = a.calls
		} else if a.calls != baselineCalls || len(s.observations) != 1 {
			t.Fatalf("advisory added a review call or lost its record: calls=%d baseline=%d records=%d", a.calls, baselineCalls, len(s.observations))
		}
	}
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
