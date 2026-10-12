package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/contentpools"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/model"
)

type savedPlanIsolationProbeStore struct{ *crossCategoryProbeStore }

func (s *savedPlanIsolationProbeStore) LoadLivePolicyLayers(context.Context, int64, int64) (string, *model.LivePolicyVersion, *model.LivePolicyVersion, *model.LivePolicyVersion, error) {
	panic("legacy policy fetched")
}
func (s *savedPlanIsolationProbeStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return []model.AgentMemoryItem{{TenantID: 14, RoomID: 15, Status: "active", MemoryType: "fact", CurrentVersion: &model.AgentMemoryVersion{ContentText: "旧房间哨兵：绵阳菜籽油，中通，2斤试吃装"}},
		{TenantID: 14, RoomID: 15, Status: "active", MemoryType: "style", CurrentVersion: &model.AgentMemoryVersion{ContentText: "旧风格哨兵：买两桶144元"}}}, nil
}
func (s *savedPlanIsolationProbeStore) CurrentSpeechExpressionConstraints(context.Context, int64, int64) (contentpools.Sources, error) {
	return contentpools.Sources{}, nil
}

// Offline exchanges are archived below; no business asset or production log is written.
func (s *savedPlanIsolationProbeStore) BeginModelExchange(context.Context, appdb.ModelExchangeScope, string, string, any) (int64, error) {
	return 0, nil
}
func (s *savedPlanIsolationProbeStore) CompleteModelExchange(context.Context, int64, any, string, int64, *int64, *int64, error) error {
	return nil
}

type isolationProbeGateway struct {
	gateway *agentgateway.Gateway
	calls   []map[string]any
}

func (g *isolationProbeGateway) Complete(ctx context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	raw, _ := json.Marshal(r.Messages)
	for _, bad := range []string{"旧房间哨兵", "旧风格哨兵", "\"source_layer\":\"L3\""} {
		if strings.Contains(string(raw), bad) {
			return agentgateway.Response{}, fmt.Errorf("request leaked %s", bad)
		}
	}
	if !strings.Contains(string(raw), "完整有效P0") {
		return agentgateway.Response{}, fmt.Errorf("P0 absent in %s", r.Stage)
	}
	response, err := g.gateway.Complete(ctx, r)
	g.calls = append(g.calls, map[string]any{"request": r, "response": response, "completed": err == nil})
	return response, err
}

func TestSavedPlanIsolationProviderProbe(t *testing.T) {
	if os.Getenv("SAVED_PLAN_ISOLATION_PROBE") != "1" {
		t.Skip("explicit paid isolated provider replay")
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("test connection unavailable")
	}
	defer store.Close()
	server := &Server{store: store}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fixtures := crossCategoryFixtures()
	fixtures[3].Questions[0] = "这款是多少克多少钱？"
	fixtures = append(fixtures, crossCategoryFixture{"同一保湿霜改事实", "使用方式", "核对新规格", []string{"1号链接保湿霜80g，售价128元，仅用于完整皮肤外用"}, []string{"这是外用护肤品，不是食品或药品；没有确认治疗效果"}, []string{"这款是多少克多少钱？"}})
	rows := []map[string]any{}
	defer func() {
		raw, _ := json.MarshalIndent(map[string]any{"rows": rows, "synthetic": true, "tts": false, "played": false, "adopted": false, "passed": !t.Failed()}, "", "  ")
		if err := os.WriteFile(os.Getenv("SAVED_PLAN_ISOLATION_OUTPUT"), raw, 0600); err != nil {
			t.Error(err)
		}
	}()
	for _, f := range fixtures {
		facts := strings.Join(append(append([]string{}, f.Topic...), f.Boundary...), "\n")
		s := &savedPlanIsolationProbeStore{&crossCategoryProbeStore{Store: store, name: f.Name, facts: facts, productContext: facts}}
		g := &isolationProbeGateway{gateway: server.speechGateway()}
		worker := decisionexecutor.New(s, nil, g, nil)
		result, err := worker.SimulateInteraction(ctx, 14, 15, f.Questions[0], "", nil)
		row := map[string]any{"category": f.Name, "question": f.Questions[0], "facts": facts, "result": result, "calls": g.calls, "completed": err == nil}
		if err != nil {
			row["error"] = err.Error()
			t.Errorf("%s failed: %v", f.Name, err)
		}
		rows = append(rows, row)
		t.Logf("%s complete=%t calls=%d", f.Name, err == nil, len(g.calls))
	}
}
