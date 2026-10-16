package decisionexecutor

import (
	"context"
	"errors"
	"testing"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type interactionExchangeTestStore struct {
	*fakeStore
	request   any
	stage     string
	completed bool
	cached    int64
	callErr   error
}

func (s *interactionExchangeTestStore) BeginModelExchange(_ context.Context, scope appdb.ModelExchangeScope, stage, _ string, request any) (int64, error) {
	if scope.TenantID != 7 || scope.RoomID != 11 || scope.PlanID != 3 {
		return 0, errors.New("wrong scope")
	}
	s.stage, s.request = stage, request
	return 42, nil
}

func (s *interactionExchangeTestStore) CompleteModelExchange(_ context.Context, id int64, _ any, _ string, _ int64, _, _, cached *int64, callErr error) error {
	if id != 42 {
		return errors.New("wrong exchange")
	}
	s.completed, s.callErr = true, callErr
	if cached != nil {
		s.cached = *cached
	}
	return nil
}

type interactionExchangeTestAgent struct{ fail error }

func (a interactionExchangeTestAgent) Complete(context.Context, agentgateway.Request) (agentgateway.Response, error) {
	return agentgateway.Response{Text: "回答", InputTokens: 100, OutputTokens: 10, CachedInputTokens: 30}, a.fail
}

func TestInteractionExchangePersistsRequestResponseAndProviderFailure(t *testing.T) {
	for _, fail := range []error{nil, errors.New("provider unavailable")} {
		s := &interactionExchangeTestStore{fakeStore: readyVoiceStore()}
		request := agentgateway.Request{Stage: "speech_generation", Messages: []agentgateway.Message{{Role: "user", Content: "完整商品和公共事实"}}}
		w := New(s, nil, interactionExchangeTestAgent{fail}, nil)
		_, err := w.completeInteractionAnswer(context.Background(), model.LiveRuntimeSession{TenantID: 7, RoomID: 11}, request)
		if !errors.Is(err, fail) || !s.completed || s.cached != 30 || !errors.Is(s.callErr, fail) || s.stage != "interaction_agent_answer" {
			t.Fatal("interaction exchange was not recorded", err, s)
		}
		if recorded, ok := s.request.(agentgateway.Request); !ok || recorded.Messages[0].Content != request.Messages[0].Content {
			t.Fatal("original complete request lost", s.request)
		}
	}
}
