// Explicit paid text-only replay of the plan editor's production simulation.
// The caller owns case identity; no correction is adopted, no TTS or live work starts.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	"livecompanion/management/internal/db"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/model"
)

type planStore struct {
	*db.Store
	tenant, room int64
	plan         model.LiveAgentPlan
}

func (s *planStore) GetLiveAgentPlanForRoom(_ context.Context, tenant, room int64) (model.LiveAgentPlan, error) {
	if tenant != s.tenant || room != s.room || s.plan.TenantID != tenant {
		return model.LiveAgentPlan{}, fmt.Errorf("scope mismatch")
	}
	return s.plan, nil
}
func (s *planStore) GetPublishedLiveAgentPlanVersionForRoom(ctx context.Context, tenant, room int64) (model.LiveAgentPlanVersion, error) {
	p, err := s.GetLiveAgentPlanForRoom(ctx, tenant, room)
	return model.LiveAgentPlanVersion{PlanID: p.ID}, err
}

type completer interface {
	Complete(context.Context, agentgateway.Request) (agentgateway.Response, error)
}
type recordingAgent struct {
	inner completer
	calls []map[string]any
}

func (a *recordingAgent) Complete(ctx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
	resp, err := a.inner.Complete(ctx, req)
	row := map[string]any{"request": req, "response": resp, "success": err == nil}
	if err != nil {
		row["error"] = "model request failed"
	}
	a.calls = append(a.calls, row)
	return resp, err
}

func main() {
	live := flag.Bool("live", false, "authorize paid text-only simulation")
	in := flag.String("input", "", "case input")
	out := flag.String("output", "", "raw request and response archive")
	flag.Parse()
	if !*live || *in == "" || *out == "" {
		panic("explicit probe arguments required")
	}
	var packet struct {
		Tenant int64 `json:"tenant_id"`
		Plan   int64 `json:"plan_id"`
		Room   int64 `json:"room_id"`
		Cases  []struct {
			Label      string `json:"label"`
			Question   string `json:"question"`
			Correction string `json:"correction"`
		} `json:"cases"`
	}
	raw, err := os.ReadFile(*in)
	if err != nil || json.Unmarshal(raw, &packet) != nil || len(packet.Cases) == 0 || len(packet.Cases) > 12 {
		panic("invalid probe input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	s, err := db.Open(config.Load())
	if err != nil {
		panic("configuration unavailable")
	}
	defer s.Close()
	p, err := s.GetLiveAgentPlan(ctx, packet.Tenant, packet.Plan)
	if err != nil || p.Status != "active" {
		panic("plan unavailable")
	}
	store := &planStore{Store: s, tenant: packet.Tenant, room: packet.Room, plan: p}
	rows := []map[string]any{}
	save := func() {
		b, _ := json.MarshalIndent(map[string]any{"rows": rows, "text_only": true, "production_adopted": false, "tts_generated": false, "live_sessions_started": false}, "", "  ")
		if os.WriteFile(*out+".tmp", b, 0600) != nil || os.Rename(*out+".tmp", *out) != nil {
			panic("archive failed")
		}
	}
	for _, c := range packet.Cases {
		agent := &recordingAgent{inner: agentgateway.NewFromEnv().WithSpeechModels(s)}
		worker := decisionexecutor.New(store, nil, agent, nil)
		started := time.Now().UTC()
		result, err := worker.SimulateAnswerWithPreview(ctx, packet.Tenant, packet.Room, c.Question, c.Correction, "semantic", "", 0)
		row := map[string]any{"label": c.Label, "question": c.Question, "correction": c.Correction, "started_at": started, "result": result, "calls": agent.calls, "success": err == nil}
		if err != nil {
			row["error"] = "simulation failed"
		}
		rows = append(rows, row)
		save()
		fmt.Printf("case=%s success=%v calls=%d\n", c.Label, err == nil, len(agent.calls))
	}
}
