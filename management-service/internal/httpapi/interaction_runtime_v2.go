package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionplan"
	"net/http"
	"strings"
	"time"
)

type interactionV2Input struct {
	PreviewInstruction string                           `json:"preview_instruction,omitempty"`
	Context            []interactionplan.ContextMessage `json:"context,omitempty"`
	Items              []interactionV2Input             `json:"items,omitempty"`
	BatchID            string                           `json:"batch_id,omitempty"`
	Tenant             int64                            `json:"tenant_id"`
	Room               int64                            `json:"room_id"`
	Plan               int64                            `json:"plan_id"`
	UserID             string                           `json:"user_id"`
	Nickname           string                           `json:"nickname"`
	Test               bool                             `json:"test_simulation"`
	Text               string                           `json:"text"`
	Protocol           string                           `json:"protocol"`
	Task               string                           `json:"task"`
	Epoch              string                           `json:"epoch"`
	Revision           uint64                           `json:"revision"`
	Kind               string                           `json:"kind"`
	Original           string                           `json:"original,omitempty"`
	Clarification      string                           `json:"clarification,omitempty"`
	Recent             []interactionplan.Turn           `json:"recent,omitempty"`
	CurrentMainline    string                           `json:"current_mainline,omitempty"`
	EngagementToken    string                           `json:"engagement_token,omitempty"`
	EventCount         int                              `json:"event_count,omitempty"`
	AllowClarify       bool                             `json:"allow_clarify"`
	ExpiresAt          time.Time                        `json:"expires_at"`
}

func (s *Server) testInteractionV2(w http.ResponseWriter, r *http.Request, in interactionV2Input, planName string) {
	e, err := s.prepareInteractionV2(r.Context(), in)
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	if !e.Intent.Relevant || e.Intent.Type == "主动互动" {
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
	results, _, err := s.prepareInteractionBatchV2(ctx, in.Task, []interactionV2Input{in})
	if err != nil {
		return interactionplan.Envelope{}, err
	}
	if results[0].Error != "" {
		return interactionplan.Envelope{}, fmt.Errorf("%s", results[0].Error)
	}
	return results[0].Envelope, nil
}
func (s *Server) resolveInteractionV2(w http.ResponseWriter, r *http.Request, in interactionV2Input) {
	ctx, cancel := context.WithTimeout(r.Context(), 28*time.Second)
	defer cancel()
	if in.Items != nil {
		if in.Test {
			writeError(w, 400, "内部批次不能使用测试身份")
			return
		}
		for _, item := range in.Items {
			if item.Tenant != in.Tenant || item.Room != in.Room || item.Plan != in.Plan || item.Epoch != in.Epoch || item.Test {
				writeError(w, 400, "互动批次身份不一致")
				return
			}
		}
		results, issues, err := s.prepareInteractionBatchV2(ctx, in.BatchID, in.Items)
		if err != nil {
			writeError(w, 422, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"protocol": interactionplan.Protocol, "batch_id": in.BatchID, "results": results, "issues": issues})
		return
	}
	e, err := s.prepareInteractionV2(ctx, in)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	raw, _ := json.Marshal(e)
	writeJSON(w, http.StatusOK, map[string]any{"question": e.Intent.Question, "relevant": e.Intent.Relevant, "topic": strings.TrimSpace(e.Intent.Meaning), "source": interactionplan.Protocol, "ambiguous": e.Intent.NeedClarify, "clarify": e.Intent.Clarify, "knowledge": json.RawMessage(raw)})
}
