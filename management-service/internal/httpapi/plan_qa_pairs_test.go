package httpapi

import (
	"context"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestQAOrganizerUsesReviewRole(t *testing.T) {
	if !agentgateway.UsesOperatorPinnedModel("qa_pairs_organize") {
		t.Fatal("QA organizer must use the operator review profile")
	}
	models := agentgateway.SpeechModels{ReviewDefaultID: "review-profile", SystemDefaultID: "analysis-profile"}
	if got := workbenchProfileForStage(context.Background(), models, "other-profile", "qa_pairs_organize"); got != "review-profile" {
		t.Fatal("QA organizer escaped configured review role", got)
	}
}

func TestQARoutesRequireAuthenticatedActor(t *testing.T) {
	for _, handler := range []func(*httptest.ResponseRecorder){
		func(w *httptest.ResponseRecorder) {
			(&Server{}).planQAPairs(w, httptest.NewRequest("GET", "/api/v1/live-agent-plans/4/qa-pairs", nil))
		},
		func(w *httptest.ResponseRecorder) {
			(&Server{}).recallPlanQAPairs(w, httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/qa-pairs/recall", nil))
		},
		func(w *httptest.ResponseRecorder) {
			(&Server{}).commitQAPairs(w, httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/qa-pairs/commit", nil))
		},
		func(w *httptest.ResponseRecorder) {
			(&Server{}).organizeQAPairs(w, httptest.NewRequest("POST", "/api/v1/live-agent-plans/4/qa-pairs/organize", nil))
		},
	} {
		w := httptest.NewRecorder()
		handler(w)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
}

func TestQAEmbeddingDeduplicatesQuestionsAndPreservesAnswerAndResidualVectors(t *testing.T) {
	calls := 0
	old := qapair.Mutation{Entry: qapair.Entry{Question: "原问题", Answer: "原答案", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}, Model: "old-model", Space: qapair.SpaceVersion, Vectors: [][]float32{{0, 1}}}
	items := []qapair.Mutation{old, {Entry: qapair.Entry{Question: "问题", Answer: "答案不送向量", Aliases: []string{"相似问法"}, Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}}, {Entry: qapair.Entry{Question: "问题", Answer: "另一对象答案", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 11}}}}, {Entry: qapair.Entry{Question: "待归属", Answer: "不付费", Enabled: false}}}
	if err := fillQAVectors(context.Background(), items, danmakuFakeEmbed{calls: &calls}); err != nil || calls != 1 || !reflect.DeepEqual(items[0], old) || len(items[1].Vectors) != 2 || len(items[2].Vectors) != 1 || len(items[3].Vectors) != 0 {
		t.Fatal(items, err, calls)
	}
}

func TestQAVectorFailureAndDisabledNoCharge(t *testing.T) {
	if err := fillQAVectors(context.Background(), []qapair.Mutation{{Entry: qapair.Entry{Question: "问", Answer: "答", Enabled: false}}}, nil); err != nil {
		t.Fatal(err)
	}
	calls := 0
	writes := []qapair.Mutation{{Entry: qapair.Entry{Question: "问", Answer: "答", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}}}
	if err := fillQAVectors(context.Background(), writes, danmakuFakeEmbed{calls: &calls, fail: true}); err == nil || calls != 1 || writes[0].Model != "" {
		t.Fatal(err, calls, writes)
	}
}
