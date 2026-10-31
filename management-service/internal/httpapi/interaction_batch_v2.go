package httpapi

import (
	"context"
	"fmt"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

type interactionBatchResultV2 struct {
	Task     string                   `json:"task"`
	Revision uint64                   `json:"revision"`
	Envelope interactionplan.Envelope `json:"knowledge"`
	Error    string                   `json:"error,omitempty"`
}

func batchInputsV2(batchID string, inputs []interactionV2Input, now time.Time) ([]interactionplan.BatchItem, error) {
	if batchID == "" || len(batchID) > 160 || len(inputs) < 1 || len(inputs) > interactionplan.MaxBatchItems {
		return nil, fmt.Errorf("互动批次须包含1至10人及有效批次身份")
	}
	first := inputs[0]
	seen := map[string]bool{}
	items := make([]interactionplan.BatchItem, len(inputs))
	for i, in := range inputs {
		if in.Tenant <= 0 || in.Room <= 0 || in.Plan <= 0 || in.Task == "" || len(in.Task) > 160 || in.Epoch == "" || len(in.Epoch) > 160 || in.Tenant != first.Tenant || in.Room != first.Room || in.Plan != first.Plan || in.Epoch != first.Epoch || in.Test != first.Test || seen[in.Task] || len(in.Items) > 0 {
			return nil, fmt.Errorf("互动任务身份不完整或批次身份不一致")
		}
		seen[in.Task] = true
		if !in.ExpiresAt.IsZero() && !now.Before(in.ExpiresAt) {
			return nil, context.DeadlineExceeded
		}
		if len(inputs) > 1 && (in.ExpiresAt.IsZero() || in.Revision == 0) {
			return nil, fmt.Errorf("互动批次期限或修订号缺失")
		}
		items[i] = interactionplan.BatchItem{ID: fmt.Sprintf("U%02d", i+1), Input: interactionplan.Input{Kind: in.Kind, Text: in.Text, Original: in.Original, Clarification: in.Clarification, Recent: in.Recent, Context: in.Context, CurrentMainline: in.CurrentMainline, EngagementToken: in.EngagementToken, Correction: in.PreviewInstruction, EventCount: in.EventCount, AllowClarify: in.AllowClarify}}
	}
	return items, interactionplan.ValidateBatch(items)
}

func preparedBatchResultsV2(ctx context.Context, snapshot interactionplan.Snapshot, inputs []interactionV2Input, items []interactionplan.BatchItem, complete interactionplan.Complete) ([]interactionBatchResultV2, []string, error) {
	generated, err := interactionplan.GenerateBatch(ctx, snapshot, items, complete)
	if err != nil {
		return nil, nil, err
	}
	results := make([]interactionBatchResultV2, len(inputs))
	for i, in := range inputs {
		item := generated.Items[i]
		results[i] = interactionBatchResultV2{Task: in.Task, Revision: in.Revision}
		if item.Err != nil {
			results[i].Error = item.Err.Error()
			continue
		}
		if !in.ExpiresAt.IsZero() && !time.Now().Before(in.ExpiresAt) {
			results[i].Error = "该人候选已过期"
			continue
		}
		results[i].Envelope = interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: in.Task, Epoch: in.Epoch, Revision: in.Revision, Tenant: in.Tenant, Room: in.Room, Session: snapshot.Scope.Session, Plan: in.Plan, Fingerprint: snapshot.Fingerprint, Body: item.Body, Intent: item.Intent, ExpiresAt: in.ExpiresAt}
	}
	return results, generated.Issues, nil
}

func (s *Server) prepareInteractionBatchV2(ctx context.Context, batchID string, inputs []interactionV2Input) ([]interactionBatchResultV2, []string, error) {
	items, err := batchInputsV2(batchID, inputs, time.Now())
	if err != nil {
		return nil, nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, nil, err
	}
	in := inputs[0]
	// One nearly-expired candidate must not cancel all other people's analysis.
	var deadline time.Time
	for _, item := range inputs {
		if item.ExpiresAt.After(deadline) {
			deadline = item.ExpiresAt
		}
	}
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	session := model.LiveRuntimeSession{TenantID: in.Tenant, RoomID: in.Room}
	if !in.Test {
		session, err = s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || session.ID <= 0 || session.EndedAt != nil || session.Status != "running" {
			return nil, nil, fmt.Errorf("互动场次当前不可用")
		}
	}
	source := interactionplan.Source(s.store)
	if in.Test {
		plan, err := s.store.GetLiveAgentPlan(ctx, in.Tenant, in.Plan)
		if err != nil {
			return nil, nil, err
		}
		source = &planSpeechTestStore{Store: s.store, tenantID: in.Tenant, roomID: in.Room, plan: plan}
	}
	snapshot, err := interactionplan.Freeze(ctx, source, session)
	if err != nil {
		return nil, nil, err
	}
	if snapshot.Scope.Plan != in.Plan {
		return nil, nil, fmt.Errorf("互动方案已变化")
	}
	bind := interactionPurposeCostBinder(s.store.GetLiveRuntimeByRoom, in.Tenant, in.Room, in.Plan)
	complete := func(callCtx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		if err := s.checkInteractionBatchBudgetV2(callCtx, len(inputs), req.MaxTokens); err != nil {
			return agentgateway.Response{}, err
		}
		if !in.Test {
			var err error
			callCtx, err = bind(callCtx)
			if err != nil {
				return agentgateway.Response{}, err
			}
			cost := liveCostScope(callCtx)
			cost.Key = fmt.Sprintf("interaction-v2-batch:%d:%s:%s", session.ID, batchID, req.Stage)
			callCtx = withLiveCostScope(callCtx, cost)
		}
		result, _, err := s.completeAnchorStyleWorkbench(callCtx, "", req.Stage, req.ResponseFormat, req.Messages, req.MaxTokens, false)
		return result, err
	}
	results, issues, err := preparedBatchResultsV2(ctx, snapshot, inputs, items, complete)
	if err != nil {
		return nil, nil, err
	}
	if !in.Test {
		current, err := s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || current.ID != session.ID || current.EndedAt != nil || current.Status != "running" {
			return nil, nil, fmt.Errorf("互动场次在生成期间已变化")
		}
	}
	return results, issues, nil
}

func interactionBatchBudgetV2(count, configured, requested int) error {
	if count > 1 && configured < requested {
		return fmt.Errorf("当前互动模型输出上限%d不足以处理本批%d人（请求预算%d）；请提高模型输出上限或减少批次人数", configured, count, requested)
	}
	return nil
}

// Match the existing operator-pinned resolution route. A small profile must not
// silently clip a ten-person request into a one-answer response.
func (s *Server) checkInteractionBatchBudgetV2(ctx context.Context, count, requested int) error {
	if count <= 1 {
		return nil
	}
	configured, err := s.store.SpeechModels(ctx)
	if err != nil {
		return err
	}
	id := workbenchProfileForStage(ctx, configured, "", "interaction_purpose_resolution")
	if operator, err := s.store.OperatorSpeechModels(ctx); err == nil && operator.ReviewDefaultID != "" {
		id = operator.ReviewDefaultID
	}
	if id == "" {
		return nil
	}
	profile, err := s.store.ResolveOperatorSpeechModel(ctx, id)
	if err != nil {
		return err
	}
	if profile == nil {
		return fmt.Errorf("未配置可用互动模型")
	}
	return interactionBatchBudgetV2(count, profile.MaxTokens, requested)
}
