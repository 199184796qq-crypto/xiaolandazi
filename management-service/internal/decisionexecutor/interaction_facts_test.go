package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

type snapshotFactStore struct {
	*fakeStore
	asset  model.LiveDerivedSafetyAsset
	failed bool
}

func TestInteractionBusinessPlanRetainsCompleteFactsForLiveAndSimulation(t *testing.T) {
	for i, facts := range []string{
		"1号套餐337元；2号套餐187元。公共：仅堂食，每桌限用一张券；停车2小时免费，退款按未核销条件办理。未确认真空包装和寄送。",
		"1号成品酱料29元；2号原料辣椒19元。公共：常温配送；酱料开袋即食，原料必须做熟。未确认开封后保质期。",
		"1号电饭煲299元；2号替换内胆79元。公共：七日退货须包装完好；内胆不能放明火上。未确认蒸汽消毒功能。",
		"1号线下课199元；2号线上课99元。公共：提前一天预约；仅线下课在门店使用，未确认线上课可改期。",
	} {
		for _, simulation := range []bool{false, true} {
			t.Run(fmt.Sprintf("industry=%d/simulation=%t", i, simulation), func(t *testing.T) {
				s := &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: map[string]any{"derived_safety": map[string]any{"source_sha256": "approved"}}}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: facts}}
				w := New(s, &fakeCore{}, &fakeAgent{}, &fakeTTS{})
				item := decisionItem{SampleQuestions: []string{"怎么使用？"}, BusinessCoreOnly: simulation, BusinessPlan: &interactionBusinessPlan{Basis: []string{"规划只摘录部分资料"}, Requirements: []string{}}}
				prompt, err := w.answerPrompt(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 15}, item)
				if err != nil || !strings.Contains(prompt, facts) || strings.Count(prompt, facts) != 1 {
					t.Fatalf("complete package lost or duplicated: %v\n%s", err, prompt)
				}
				if simulation {
					var data map[string]json.RawMessage
					if err := json.Unmarshal([]byte(prompt), &data); err != nil || !strings.Contains(string(data["current_facts"]), facts) {
						t.Fatal("simulation lacks authoritative facts", err, prompt)
					}
				}
				factContext, err := w.interactionProductContext(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 15}, 2, simulation)
				if err != nil || factContext != facts {
					t.Fatal("fact check did not receive full approved package", factContext, err)
				}
			})
		}
	}
}

func TestInteractionApprovedFactsRejectsInvalidSnapshotAndCrossPlan(t *testing.T) {
	for _, generation := range []map[string]any{
		{"derived_safety": nil},
		{"derived_safety": "invalid"},
		{"derived_safety": map[string]any{}},
		{"derived_safety": map[string]any{"source_sha256": "missing"}},
	} {
		s := &snapshotFactStore{fakeStore: &fakeStore{publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: generation}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: "不能回退旧事实"}}
		w := New(s, nil, nil, nil)
		if _, err := w.interactionProductContext(context.Background(), model.LiveRuntimeSession{TenantID: 7}, 2, false); err == nil {
			t.Fatal("invalid reference silently fell back", generation)
		}
	}
	s := &snapshotFactStore{fakeStore: &fakeStore{publishedVersion: &model.LiveAgentPlanVersion{PlanID: 3}}}
	if _, err := New(s, nil, nil, nil).interactionProductContext(context.Background(), model.LiveRuntimeSession{TenantID: 7}, 2, false); err == nil {
		t.Fatal("another scheme was accepted")
	}
}

func TestInteractionFactCheckReceivesProductAndCommonFacts(t *testing.T) {
	facts := "1号套餐337元；2号套餐187元。\n公共：仅堂食，每桌限用1张券。"
	s := &snapshotFactStore{fakeStore: &fakeStore{plan: model.LiveAgentPlan{ID: 2}, publishedVersion: &model.LiveAgentPlanVersion{PlanID: 2, GenerationContext: map[string]any{"derived_safety": map[string]any{"source_sha256": "approved"}}}}, asset: model.LiveDerivedSafetyAsset{SafeFacts: facts}}
	a := &fakeAgent{responses: []string{`{"verdict":"supported","fabricated_facts":[],"missing_facts":[]}`}}
	w := New(s, nil, a, nil)
	w.checkFactGap(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 15}, decisionItem{SampleQuestions: []string{"一桌几张券"}}, "每桌限用1张券。")
	if len(a.requests) != 1 || !strings.Contains(a.requests[0].Messages[1].Content, facts) {
		t.Fatal("review model did not receive both product cards and public conditions", a.requests)
	}
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
