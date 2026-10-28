package decisionexecutor

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionlearning"
	"livecompanion/management/internal/interactionpolicy"
	"livecompanion/management/internal/model"
	"os"
	"strings"
	"testing"
	"time"
)

type nicknameProbeGateway struct {
	store   *db.Store
	gateway *agentgateway.Gateway
	calls   []map[string]any
}

func (g *nicknameProbeGateway) ModelRoleIdentity(ctx context.Context, stage string) (string, error) {
	return g.gateway.ModelRoleIdentity(ctx, stage)
}
func (g *nicknameProbeGateway) Complete(ctx context.Context, r agentgateway.Request) (agentgateway.Response, error) {
	id, e := g.store.ResolveTextRole(ctx, r.Stage)
	if e != nil {
		return agentgateway.Response{}, e
	}
	p, e := g.store.ResolveSpeechModel(ctx, id)
	if e != nil || p == nil {
		return agentgateway.Response{}, e
	}
	r.Model = p.Model
	if p.SystemPrompt != "" {
		r.Messages = append([]agentgateway.Message{{Role: "system", Content: p.SystemPrompt}}, r.Messages...)
	}
	r, e = agentgateway.PrepareTextRequest(*p, r)
	if e != nil {
		return agentgateway.Response{}, e
	}
	response, e := agentgateway.CompleteSpeechEndpoint(ctx, *p, r)
	g.calls = append(g.calls, map[string]any{"stage": r.Stage, "request": r, "response": response, "success": e == nil})
	return response, e
}

// Explicit paid offline coupling of the production nickname planner, cache
// and Flash request transport. No TTS, session, queue or business-data writes.
func TestNicknameProductionProviderSmoke(t *testing.T) {
	if os.Getenv("NICKNAME_PROVIDER_SMOKE") != "1" {
		t.Skip("explicit paid probe required")
	}
	raw, e := os.ReadFile(os.Getenv("NICKNAME_SMOKE_INPUT"))
	if e != nil {
		t.Fatal(e)
	}
	var cases []struct {
		Name  string   `json:"name"`
		Names []string `json:"names"`
		Kind  string   `json:"kind"`
	}
	if e = json.Unmarshal(raw, &cases); e != nil {
		t.Fatal(e)
	}
	store, e := db.Open(config.Load())
	if e != nil {
		t.Fatal("configuration database unavailable")
	}
	defer store.Close()
	g := &nicknameProbeGateway{store: store, gateway: agentgateway.NewFromEnv().WithSpeechModels(store)}
	w := &Worker{store: store, agent: g, recentNamed: map[int64]map[string]time.Time{}}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	session := model.LiveRuntimeSession{TenantID: 14, RoomID: 26}
	all := []string{}
	for _, c := range cases {
		for _, n := range c.Names {
			if !containsNickname(all, n) {
				all = append(all, n)
			}
		}
	}
	for start := 0; start < len(all); start += 16 {
		item := decisionItem{Nicknames: all[start:min(start+16, len(all))]}
		w.planNicknameAddresses(ctx, session, &item)
	}
	planCalls := len(g.calls)
	rows := []map[string]any{}
	core, _ := interactionpolicy.Read("interaction-core-content.md")
	for _, c := range cases {
		item := decisionItem{Nicknames: c.Names, MissionKind: "welcome_named", MissionEventCount: len(c.Names)}
		w.planNicknameAddresses(ctx, session, &item)
		plan := frozenAddressingPlan(item)
		data, _ := json.Marshal(map[string]any{"场景": c.Kind, "本次目的": "回应来访观众，帮助进入当前话题", "当前正在讲": "商品规格和包装", "本批观众昵称": item.promptNicknames(), "昵称已按称呼规划处理": true, "涉及多人": len(c.Names) > 1, "聚合事件数": len(c.Names)})
		body := strings.ReplaceAll(string(core), "{{SCENE_DATA}}", string(data)) + "\n" + addressingPlanGuidance(plan) + "\n本场喜欢老乡、朋友，不喜欢宝子。已确认事实：本店使用京东物流，具体到货时间未确认。正文20到50字。"
		if c.Kind == "商品问答" {
			body += "\n观众问题：什么物流？"
		}
		body += "\n本次最终只返回JSON对象：{\"reply\":\"口播正文\",\"needs_review\":false,\"review\":null}。"
		r := agentgateway.Request{Stage: "speech_generation", Messages: []agentgateway.Message{{Role: "system", Content: string(core)}, {Role: "user", Content: body}}, ResponseFormat: agentgateway.ResponseJSON, EnableThinking: false, MaxTokens: 650, Timeout: 30 * time.Second}
		response, e := g.Complete(ctx, r)
		answer, decodeErr := interactionlearning.DecodeAnswer(response.Text)
		rows = append(rows, map[string]any{"name": c.Name, "original_names": item.Nicknames, "prompt_names": item.PromptNicknames, "addressing": plan, "reply": answer.Reply, "success": e == nil && decodeErr == nil, "response": response})
		if e != nil || decodeErr != nil {
			t.Errorf("%s generation/protocol failed", c.Name)
		}
	}
	raw, _ = json.MarshalIndent(map[string]any{"passed": !t.Failed(), "text_only": true, "planner_calls": planCalls, "rows": rows, "calls": g.calls}, "", "  ")
	if e = os.WriteFile(os.Getenv("NICKNAME_SMOKE_OUTPUT"), raw, 0600); e != nil {
		t.Fatal(e)
	}
}
func containsNickname(values []string, n string) bool {
	for _, v := range values {
		if v == n {
			return true
		}
	}
	return false
}
