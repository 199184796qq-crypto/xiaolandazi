package decisionexecutor

import (
	"context"
	"errors"
	"livecompanion/management/internal/contentpools"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

type expressionFixtureStore struct {
	*fakeStore
	source contentpools.Sources
	err    error
}

func (s *expressionFixtureStore) CurrentSpeechExpressionConstraints(context.Context, int64, int64) (contentpools.Sources, error) {
	return s.source, s.err
}
func TestInteractionExpressionConstraintGatesTextAndPreparedAudio(t *testing.T) {
	store := &expressionFixtureStore{fakeStore: readyVoiceStore(), source: contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1"}}, Corrections: []contentpools.Correction{{Kind: contentpools.ExpressionConstraintKind, ID: "private-id", Products: []int64{1}, Feedback: "不要早餐", Content: contentpools.CorrectionContent{Forbidden: "不作为早餐举例"}}}}}
	store.plan = model.LiveAgentPlan{ID: 4}
	agent := &fakeAgent{responses: []string{`{"passed":false,"quote":"早上赶时间","reason":"早餐暗示","suggestions":["删除早晨场景"]}`}}
	w := New(store, nil, agent, nil)
	session := model.LiveRuntimeSession{TenantID: 7, RoomID: 11}
	if _, e := w.verifySpeechExpressionConstraints(context.Background(), session, "早上赶时间，开袋就能吃。"); e == nil || !strings.Contains(e.Error(), "早餐") {
		t.Fatal("violating text reached audio", e)
	}
	if len(agent.requests) != 1 || agent.requests[0].Stage != "speech_constraint_text_review" || strings.Contains(agent.requests[0].Messages[1].Content, "private-id") {
		t.Fatal("wrong review or ID leaked", agent.requests)
	}
	agent.responses = append(agent.responses, `{"passed":true,"quote":"","reason":"","suggestions":[]}`)
	if text, e := w.verifySpeechExpressionConstraints(context.Background(), session, "一般第二天早上发货。"); e != nil || text == "" {
		t.Fatal("unrelated morning rejected", e)
	}
	store.err = errors.New("reader failed")
	if _, e := w.verifySpeechExpressionConstraints(context.Background(), session, "当前话术"); e == nil {
		t.Fatal("rule reader failure silently ignored")
	}
	store.err = nil
	first, e := w.interactionPreparationScope(context.Background(), session, decisionItem{})
	if e != nil {
		t.Fatal(e)
	}
	store.source.Corrections[0].Content.Forbidden = "不提午夜场景"
	next, e := w.interactionPreparationScope(context.Background(), session, decisionItem{})
	if e != nil || first == next {
		t.Fatal("old prepared audio survived constraint adoption", e)
	}
}
