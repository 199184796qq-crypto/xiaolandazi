package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
	"net/http"
	"strings"
	"time"
)

type interactionV2Input struct {
	PreviewInstruction string                 `json:"preview_instruction,omitempty"`
	Tenant             int64                  `json:"tenant_id"`
	Room               int64                  `json:"room_id"`
	Plan               int64                  `json:"plan_id"`
	UserID             string                 `json:"user_id"`
	Nickname           string                 `json:"nickname"`
	Test               bool                   `json:"test_simulation"`
	Text               string                 `json:"text"`
	Protocol           string                 `json:"protocol"`
	Task               string                 `json:"task"`
	Epoch              string                 `json:"epoch"`
	Revision           uint64                 `json:"revision"`
	Kind               string                 `json:"kind"`
	Original           string                 `json:"original,omitempty"`
	Clarification      string                 `json:"clarification,omitempty"`
	Recent             []interactionplan.Turn `json:"recent,omitempty"`
	CurrentMainline    string                 `json:"current_mainline,omitempty"`
	EventCount         int                    `json:"event_count,omitempty"`
	AllowClarify       bool                   `json:"allow_clarify"`
	ExpiresAt          time.Time              `json:"expires_at"`
}

func (s *Server) testInteractionV2(w http.ResponseWriter, r *http.Request, in interactionV2Input, planName string) {
	e, err := s.prepareInteractionV2(r.Context(), in)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if !e.Intent.Relevant {
		writeJSON(w, 200, map[string]any{"question": in.Text, "intent": e.Intent, "reply": "", "execution_mode": interactionplan.Protocol, "plan_name": planName, "plan_id": in.Plan})
		return
	}
	raw, _ := json.Marshal(e)
	plan, err := s.store.GetLiveAgentPlan(r.Context(), in.Tenant, in.Plan)
	if err != nil {
		writeError(w, 404, "测试方案不存在")
		return
	}
	worker := decisionexecutor.New(&planSpeechTestStore{Store: s.store, tenantID: in.Tenant, roomID: in.Room, plan: plan}, nil, s.speechGateway(), nil)
	result, err := worker.SimulatePreparedInteractionV2(r.Context(), in.Tenant, in.Room, raw, in.Nickname)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"question": in.Text, "intent": e.Intent, "body": e.Body, "reply": result.Reply, "execution_mode": interactionplan.Protocol, "plan_name": planName, "plan_id": in.Plan})
}

func (s *Server) prepareInteractionV2(ctx context.Context, in interactionV2Input) (interactionplan.Envelope, error) {
	var empty interactionplan.Envelope
	if in.Tenant <= 0 || in.Room <= 0 || in.Plan <= 0 || in.Task == "" || in.Epoch == "" {
		return empty, fmt.Errorf("互动任务身份不完整")
	}
	if !in.ExpiresAt.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, in.ExpiresAt)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	session := model.LiveRuntimeSession{TenantID: in.Tenant, RoomID: in.Room}
	if !in.Test {
		var err error
		session, err = s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || session.ID <= 0 || session.EndedAt != nil || session.Status != "running" {
			return empty, fmt.Errorf("互动场次当前不可用")
		}
	}
	source := interactionplan.Source(s.store)
	if in.Test {
		plan, err := s.store.GetLiveAgentPlan(ctx, in.Tenant, in.Plan)
		if err != nil {
			return empty, err
		}
		source = &planSpeechTestStore{Store: s.store, tenantID: in.Tenant, roomID: in.Room, plan: plan}
	}
	snapshot, err := interactionplan.Freeze(ctx, source, session)
	if err != nil {
		return empty, err
	}
	if snapshot.Scope.Plan != in.Plan {
		return empty, fmt.Errorf("互动方案已变化")
	}
	bind := interactionPurposeCostBinder(s.store.GetLiveRuntimeByRoom, in.Tenant, in.Room, in.Plan)
	complete := func(callCtx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		if !in.Test {
			var err error
			callCtx, err = bind(callCtx)
			if err != nil {
				return agentgateway.Response{}, err
			}
		}
		if !in.Test {
			scope := liveCostScope(callCtx)
			scope.Key = fmt.Sprintf("interaction-v2:%d:%s:%d:%s", session.ID, in.Task, in.Revision, req.Stage)
			callCtx = withLiveCostScope(callCtx, scope)
		}
		result, _, err := s.completeAnchorStyleWorkbench(callCtx, "", req.Stage, req.ResponseFormat, req.Messages, req.MaxTokens, false)
		return result, err
	}
	intent, body, err := interactionplan.Generate(ctx, snapshot, interactionplan.Input{Kind: in.Kind, Text: in.Text, Original: in.Original, Clarification: in.Clarification, Recent: in.Recent, CurrentMainline: in.CurrentMainline, Correction: in.PreviewInstruction, EventCount: in.EventCount, AllowClarify: in.AllowClarify}, complete)
	if err != nil {
		return empty, err
	}
	if !in.Test {
		current, err := s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || current.ID != session.ID || current.EndedAt != nil || current.Status != "running" {
			return empty, fmt.Errorf("互动场次在生成期间已变化")
		}
	}
	return interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: in.Task, Epoch: in.Epoch, Revision: in.Revision, Tenant: in.Tenant, Room: in.Room, Session: session.ID, Plan: in.Plan, Fingerprint: snapshot.Fingerprint, Body: body, Intent: intent, ExpiresAt: in.ExpiresAt}, nil
}
func (s *Server) resolveInteractionV2(w http.ResponseWriter, r *http.Request, in interactionV2Input) {
	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()
	e, err := s.prepareInteractionV2(ctx, in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	raw, _ := json.Marshal(e)
	writeJSON(w, http.StatusOK, map[string]any{"question": e.Intent.Question, "relevant": e.Intent.Relevant, "topic": strings.TrimSpace(e.Intent.Meaning), "source": interactionplan.Protocol, "ambiguous": e.Intent.NeedClarify, "clarify": e.Intent.Clarify, "knowledge": json.RawMessage(raw)})
}
