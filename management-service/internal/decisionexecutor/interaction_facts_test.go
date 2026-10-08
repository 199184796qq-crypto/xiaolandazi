package decisionexecutor

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

type snapshotFactStore struct {
	*fakeStore
	asset  model.LiveDerivedSafetyAsset
	failed bool
}

func (s *snapshotFactStore) GetLiveDerivedSafety(_ context.Context, tenant, plan int64, hash string) (model.LiveDerivedSafetyAsset, error) {
	if tenant != 7 || plan != 2 || hash != "approved" || s.failed {
		return model.LiveDerivedSafetyAsset{}, errors.New("missing")
	}
	return s.asset, nil
}
func TestInteractionUsesFullApprovedCardsInsteadOfHistoricSampleFacts(t *testing.T) {
	published := &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: map[string]any{"derived_safety": map[string]any{"source_sha256": "approved"}}}
	store := &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: published, facts: []model.LiveAgentPlanFact{{Key: "旧记录", Value: "1号50斤", Status: "active"}}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: "商品卡：1号新黄菜籽油5L/9.2斤129.9元；2号黑菜籽油5L/9.2斤119.9元。福利：新人试吃5斤。"}}
	w := New(store, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	text, err := w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 15}, decisionItem{SampleQuestions: []string{"1号和2号区别"}})
	if err != nil || !strings.Contains(text, "1号新黄菜籽油") || !strings.Contains(text, "2号黑菜籽油") || !strings.Contains(text, "新人试吃5斤") || strings.Contains(text, "1号50斤") {
		t.Fatal(text, err)
	}
	store.failed = true
	if _, err = w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 15}, decisionItem{}); err == nil {
		t.Fatal("missing approved facts silently fell back to old records")
	}
}
