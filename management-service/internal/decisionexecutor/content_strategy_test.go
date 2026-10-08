package decisionexecutor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type strategyFactStore struct {
	*snapshotFactStore
	raw    string
	before time.Time
	failed bool
}

func (s *strategyFactStore) GetLatestPlanContentStrategy(_ context.Context, tenant, plan int64, hash string, before time.Time) (string, error) {
	s.before = before
	if tenant != 7 || plan != 2 || hash != contentstrategy.SHA(s.asset.SafeFacts) || s.failed {
		return "", appdb.ErrPlanAnalysisAssetNotFound
	}
	return s.raw, nil
}
func TestInteractionStrategyKeepsOfficialFactsAndExistingRules(t *testing.T) {
	facts := "- [商品卡] 1号绸都杨鸭子，现卤现炸，85.5元\n- [other/算账] 按只或斤；禁说=不要计算一克价格"
	snapshot := contentstrategy.Snapshot{Version: 1, Enabled: true, FactsSHA: contentstrategy.SHA(facts), Facts: contentstrategy.Facts(facts), Purposes: []contentstrategy.Purpose{{ID: "P01", Name: "理解价格", Concern: "贵不贵", FactIDs: []string{"F001"}}}, Rows: []contentstrategy.Row{{PurposeID: "P01", Angle: "单只预算", Task: "回答价格判断", FactIDs: []string{"F001", "F002"}, When: "价格问题"}}}
	raw, _ := json.Marshal(snapshot)
	store := &strategyFactStore{snapshotFactStore: &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: map[string]any{"derived_safety": map[string]any{"source_sha256": "approved"}}}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: facts}}, raw: string(raw)}
	w := New(store, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	started := time.Now().Add(-time.Minute)
	session := model.LiveRuntimeSession{ID: 100, TenantID: 7, RoomID: 15, StartedAt: started}
	prompt, err := w.answerPrompt(context.Background(), session, decisionItem{SampleQuestions: []string{"85.5贵不贵"}, ResumeMainline: "下面讲现卤现炸"})
	if err != nil || !strings.Contains(prompt, "单只预算") || !strings.Contains(prompt, "观众原话：85.5贵不贵") || !strings.Contains(prompt, "主线衔接硬要求") || !strings.Contains(prompt, "不要计算一克价格") || !store.before.Equal(started) {
		t.Fatal(prompt, err, store.before)
	}
	prompt, err = w.answerPrompt(context.Background(), session, decisionItem{ExecutionMode: "verbatim", FixedText: "原句"})
	if err != nil || strings.Contains(prompt, "目的角度表") {
		t.Fatal("verbatim mode used content rewriting strategy", prompt, err)
	}
	store.failed = true
	prompt, err = w.answerPrompt(context.Background(), session, decisionItem{})
	if err != nil || strings.Contains(prompt, "目的角度表") || !strings.Contains(prompt, "绸都杨鸭子") {
		t.Fatal("missing optional strategy broke old facts", prompt, err)
	}
	store.failed = false
	store.raw = `{"version":99}`
	if _, err = w.answerPrompt(context.Background(), session, decisionItem{}); err != nil {
		t.Fatal("invalid optional asset broke existing flow", err)
	}
}
