package questioncluster

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

type paidClusterStore struct {
	sessions []model.LiveRuntimeSession
	plan     model.LiveAgentPlan
	prompt   string
}

func (s *paidClusterStore) ListRunningLiveRuntimeSessions(context.Context) ([]model.LiveRuntimeSession, error) {
	return s.sessions, nil
}

func (s *paidClusterStore) AgentPromptValue(_ context.Context, _, fallback string) string {
	if s.prompt != "" {
		return s.prompt
	}
	return fallback
}
func (s *paidClusterStore) GetLiveAgentPlanForRoom(_ context.Context, tenant, room int64) (model.LiveAgentPlan, error) {
	if tenant != 14 || room != 29 {
		return model.LiveAgentPlan{}, errors.New("wrong room")
	}
	return s.plan, nil
}

type paidClusterAgent struct{ calls int }

func (a *paidClusterAgent) Complete(context.Context, agentgateway.Request) (agentgateway.Response, error) {
	a.calls++
	return agentgateway.Response{Text: `{"groups":[]}`, InputTokens: 90, OutputTokens: 10}, nil
}
func clusterLiveFixture(t *testing.T) (*Worker, *paidClusterStore, *paidClusterAgent, *brainView, model.LiveRuntimeSession) {
	t.Helper()
	now := time.Date(2026, 10, 25, 10, 0, 0, 0, time.UTC)
	brain := &brainView{}
	brain.Intelligence.TopTopics = []topicBucket{{Topic: "Q:需要冷藏吗", Count: 1, Questions: []topicQuestion{{EventID: 1, Content: "需要冷藏吗", OccurredAt: now.Add(-time.Minute)}}}, {Topic: "Q:要放冰箱吗", Count: 1, Questions: []topicQuestion{{EventID: 2, Content: "要放冰箱吗", OccurredAt: now.Add(-time.Minute)}}}}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/internal/v1/rooms/29/brain" || request.URL.Query().Get("tenant_id") != "14" {
			t.Error("unexpected Core call", request.URL)
			http.Error(response, "bad", 400)
			return
		}
		json.NewEncoder(response).Encode(brain)
	}))
	t.Cleanup(server.Close)
	session := model.LiveRuntimeSession{ID: 99, TenantID: 14, RoomID: 29, ExternalID: "live-99", Status: "running", ExecutionRealm: "prod"}
	store := &paidClusterStore{sessions: []model.LiveRuntimeSession{session}, plan: model.LiveAgentPlan{ID: 5, TenantID: 14}}
	agent := &paidClusterAgent{}
	worker := New(store, coreclient.New(server.URL, ""), agent)
	worker.now = func() time.Time { return now }
	return worker, store, agent, brain, session
}
func TestPaidClusterAccountsOnlyTriggeredLiveCallAndLeavesProcessedSkip(t *testing.T) {
	w, _, agent, _, session := clusterLiveFixture(t)
	calls := 0
	w.SetPaidModelCompleter(func(ctx context.Context, actual model.LiveRuntimeSession, request agentgateway.Request, planID int64, key string, next func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
		calls++
		scope := agentgateway.ContextRoomModelScope(ctx)
		if actual != session || planID != 5 || request.Stage != "question_cluster" || !strings.Contains(key, "question-cluster:99:live-99:2:") || scope.RuntimeID != 99 || !scope.RequireLease {
			t.Fatal(actual, planID, key, scope, request.Stage)
		}
		return next(ctx, request)
	})
	w.RunOnce(context.Background())
	w.RunOnce(context.Background())
	if calls != 1 || agent.calls != 1 {
		t.Fatal("already processed input charged twice", calls, agent.calls)
	}
}
func TestPaidClusterStoppedWrongRealmOrNoQuestionsStillSkip(t *testing.T) {
	for _, scenario := range []string{"stopped", "other-realm", "empty", "only-one"} {
		t.Run(scenario, func(t *testing.T) {
			w, store, agent, brain, _ := clusterLiveFixture(t)
			calls := 0
			w.SetPaidModelCompleter(func(context.Context, model.LiveRuntimeSession, agentgateway.Request, int64, string, func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
				calls++
				return agentgateway.Response{}, nil
			})
			switch scenario {
			case "stopped":
				store.sessions[0].Status = "stopped"
			case "other-realm":
				store.sessions[0].ExecutionRealm = "sandbox"
			case "empty":
				brain.Intelligence.TopTopics = nil
			case "only-one":
				brain.Intelligence.TopTopics = brain.Intelligence.TopTopics[:1]
			}
			w.RunOnce(context.Background())
			if calls != 0 || agent.calls != 0 {
				t.Fatal("skip guards changed", calls, agent.calls)
			}
		})
	}
}
func TestPaidClusterUnknownRetryKeepsStableKeyAndCannotReachSupplier(t *testing.T) {
	w, store, agent, _, session := clusterLiveFixture(t)
	var keys []string
	w.SetPaidModelCompleter(func(_ context.Context, _ model.LiveRuntimeSession, _ agentgateway.Request, _ int64, key string, _ func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
		keys = append(keys, key)
		return agentgateway.Response{}, errors.New("original receipt unknown")
	})
	for range 2 {
		if err := w.refineRoom(context.Background(), session, false); err == nil {
			t.Fatal("expected pending receipt")
		}
		store.prompt = "同一批问题换了提示词，不应当成新的付费操作"
	}
	if len(keys) != 2 || keys[0] != keys[1] || agent.calls != 0 {
		t.Fatal("ambiguous retry changed paid request identity", keys, agent.calls)
	}
}
func TestPaidClusterManualInspectionWithoutLiveSessionBypassesLiveBudget(t *testing.T) {
	w, _, agent, _, _ := clusterLiveFixture(t)
	calls := 0
	w.SetPaidModelCompleter(func(context.Context, model.LiveRuntimeSession, agentgateway.Request, int64, string, func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error) {
		calls++
		return agentgateway.Response{}, nil
	})
	if err := w.RunRoomOnce(context.Background(), 14, 29, false); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || agent.calls != 1 {
		t.Fatal("manual nonlive inspection entered live budget", calls, agent.calls)
	}
}
