package httpapi

import (
	"context"
	"database/sql"
	"net/http/httptest"
	"strconv"
	"testing"

	"livecompanion/management/internal/model"
)

type memoryScopeStore struct {
	plans    map[int64]model.LiveAgentPlan
	bound    int64
	memories map[int64]model.AgentMemoryItem
	reads    int
}

func (s *memoryScopeStore) GetLiveAgentPlan(_ context.Context, tenant, id int64) (model.LiveAgentPlan, error) {
	s.reads++
	p, ok := s.plans[id]
	if !ok || p.TenantID != tenant {
		return model.LiveAgentPlan{}, sql.ErrNoRows
	}
	return p, nil
}
func (s *memoryScopeStore) GetLiveAgentPlanForRoom(ctx context.Context, tenant, room int64) (model.LiveAgentPlan, error) {
	return s.GetLiveAgentPlan(ctx, tenant, s.bound)
}
func (s *memoryScopeStore) GetAgentMemoryItem(_ context.Context, tenant, room, id int64) (model.AgentMemoryItem, error) {
	m, ok := s.memories[id]
	if !ok || m.TenantID != tenant || m.RoomID != room {
		return model.AgentMemoryItem{}, sql.ErrNoRows
	}
	return m, nil
}

func TestAgentMemorySelectedPlanDoesNotFallBackToBoundPlan(t *testing.T) {
	s := &memoryScopeStore{bound: 4, plans: map[int64]model.LiveAgentPlan{4: {ID: 4, TenantID: 14, Status: "active"}, 5: {ID: 5, TenantID: 14, Status: "active"}, 6: {ID: 6, TenantID: 15, Status: "active"}, 7: {ID: 7, TenantID: 14, Status: "archived"}}}
	for _, tc := range []struct {
		query  string
		want   int64
		status int
	}{{"?plan_id=5", 5, 200}, {"", 4, 200}, {"?plan_id=6", 0, 404}, {"?plan_id=7", 0, 404}, {"?plan_id=999", 0, 404}, {"?plan_id=bad", 0, 400}, {"?plan_id=0", 0, 400}, {"?plan_id=", 0, 400}, {"?plan_id=4&plan_id=5", 0, 400}} {
		w := httptest.NewRecorder()
		p, ok := agentMemoryPlan(w, httptest.NewRequest("GET", "/"+tc.query, nil), s, 14, 26)
		if p != tc.want || w.Code != tc.status || ok != (tc.status == 200) {
			t.Fatalf("%s: plan=%d ok=%v status=%d", tc.query, p, ok, w.Code)
		}
	}
	s.bound = 0
	w := httptest.NewRecorder()
	p, ok := agentMemoryPlan(w, httptest.NewRequest("GET", "/", nil), s, 14, 26)
	if !ok || p != 0 {
		t.Fatal("missing binding must be empty, never list every plan")
	}
}

func TestAgentMemoryEditsRejectForeignAndUnboundSources(t *testing.T) {
	s := &memoryScopeStore{memories: map[int64]model.AgentMemoryItem{}}
	for _, tc := range []struct {
		id, tenant, room, plan int64
		key                    string
		allowed                bool
	}{
		{1, 14, 26, 4, "plan:4:children", true}, {2, 14, 26, 5, "plan:5:children", false}, {3, 14, 26, 0, "response_strategy:cutting", false}, {4, 15, 26, 4, "plan:4:children", false}, {5, 14, 27, 4, "plan:4:children", false}, {6, 14, 26, 4, "plan:5:children", false},
	} {
		s.memories[tc.id] = model.AgentMemoryItem{ID: tc.id, TenantID: tc.tenant, RoomID: tc.room, MemoryKey: tc.key, CurrentVersion: &model.AgentMemoryVersion{Structured: map[string]any{"review_plan_id": tc.plan}}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/", nil)
		_, ok := agentMemoryInPlan(w, r, s, 14, 26, 4, tc.id)
		if ok != tc.allowed {
			t.Fatal(tc, w.Body.String())
		}
		w = httptest.NewRecorder()
		if agentLearningSourceInPlan(w, r, s, 14, 26, 4, "agent_memory:"+strconv.FormatInt(tc.id, 10)) != tc.allowed {
			t.Fatal("correction source escaped scope", tc)
		}
	}
}
