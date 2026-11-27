package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

type welcomeTaskKey struct{}

func preparedWelcomeResultsV2(ctx context.Context, snapshot interactionplan.Snapshot, session model.LiveRuntimeSession, inputs []interactionV2Input, items []interactionplan.BatchItem, write func(context.Context, model.LiveRuntimeSession, string, string, interactionplan.Snapshot) (string, error), prepareOther func([]interactionV2Input, []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error)) ([]interactionBatchResultV2, []string, error) {
	results := make([]interactionBatchResultV2, len(inputs))
	otherInputs := make([]interactionV2Input, 0, len(inputs))
	otherItems := make([]interactionplan.BatchItem, 0, len(inputs))
	otherPositions := make([]int, 0, len(inputs))
	for i, in := range inputs {
		if !decisionexecutor.IsWelcomeKind(in.Kind) {
			otherInputs = append(otherInputs, in)
			otherItems = append(otherItems, items[i])
			otherPositions = append(otherPositions, i)
			continue
		}
		results[i] = interactionBatchResultV2{Task: in.Task, Revision: in.Revision}
		if !in.ExpiresAt.IsZero() && !time.Now().Before(in.ExpiresAt) {
			results[i].Error = "该人候选已过期"
			results[i].Parse = interactionplan.ParseDiagnostics{Outcome: "rejected", Reason: "candidate_expired"}
			continue
		}
		body, err := write(context.WithValue(ctx, welcomeTaskKey{}, in.Task), session, in.Task, in.Kind, snapshot)
		if err != nil {
			results[i].Error = err.Error()
			results[i].Parse = interactionplan.ParseDiagnostics{Outcome: "rejected", Reason: "welcome_writer_failed"}
			continue
		}
		if !in.ExpiresAt.IsZero() && !time.Now().Before(in.ExpiresAt) {
			results[i].Error = "该人候选已过期"
			results[i].Parse = interactionplan.ParseDiagnostics{Outcome: "rejected", Reason: "candidate_expired"}
			continue
		}
		results[i].Envelope = interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: in.Task, Epoch: in.Epoch, Revision: in.Revision, Tenant: in.Tenant, Room: in.Room, Session: snapshot.Scope.Session, Plan: in.Plan, Fingerprint: snapshot.Fingerprint, Body: body, Intent: interactionplan.Intent{Relevant: true, Type: "欢迎", Priority: "普通", Meaning: "欢迎新进观众"}, ExpiresAt: in.ExpiresAt}
		results[i].Parse = interactionplan.ParseDiagnostics{Outcome: "accepted", Reason: "welcome_writer_body_ready"}
	}
	if len(otherInputs) == 0 {
		return results, nil, nil
	}
	other, issues, err := prepareOther(otherInputs, otherItems)
	if err != nil {
		return nil, nil, err
	}
	if len(other) != len(otherPositions) {
		return nil, nil, fmt.Errorf("互动批次结果数量不一致")
	}
	for i, position := range otherPositions {
		results[position] = other[i]
	}
	return results, issues, nil
}

type interactionBatchResultV2 struct {
	Task     string                           `json:"task"`
	Revision uint64                           `json:"revision"`
	Envelope interactionplan.Envelope         `json:"knowledge"`
	Error    string                           `json:"error,omitempty"`
	Parse    interactionplan.ParseDiagnostics `json:"parse"`
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
		items[i].Template = in.Template
	}
	return items, interactionplan.ValidateBatch(items)
}

func preparedBatchResultsV2(ctx context.Context, snapshot interactionplan.Snapshot, inputs []interactionV2Input, items []interactionplan.BatchItem, complete interactionplan.Complete) ([]interactionBatchResultV2, []string, error) {
	var stages interactionplan.ModelStages
	generated, err := interactionplan.GenerateBatch(ctx, snapshot, items, func(ctx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		started := time.Now()
		response, err := complete(ctx, req)
		if err == nil {
			stages.Record(req.Stage, response, time.Since(started))
		}
		return response, err
	})
	if err != nil {
		return nil, nil, err
	}
	results := make([]interactionBatchResultV2, len(inputs))
	for i, in := range inputs {
		item := generated.Items[i]
		results[i] = interactionBatchResultV2{Task: in.Task, Revision: in.Revision, Parse: item.Parse}
		if item.Err != nil {
			results[i].Error = item.Err.Error()
			continue
		}
		if !in.ExpiresAt.IsZero() && !time.Now().Before(in.ExpiresAt) {
			results[i].Error = "该人候选已过期"
			results[i].Parse.Outcome, results[i].Parse.Reason = "rejected", "candidate_expired"
			continue
		}
		results[i].Envelope = interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: in.Task, Epoch: in.Epoch, Revision: in.Revision, Tenant: in.Tenant, Room: in.Room, Session: snapshot.Scope.Session, Plan: in.Plan, Fingerprint: snapshot.Fingerprint, Body: item.Body, Intent: item.Intent, ExpiresAt: in.ExpiresAt}
		results[i].Envelope.ModelStages = stages.Recorded()
		results[i].Envelope.Template = in.Template
		if in.Template != "" {
			results[i].Envelope.EventCount = in.EventCount
		}
	}
	return results, generated.Issues, nil
}

// Route questions independently so a welcome or behavior in the same batch
// cannot send them to the generic interaction generator.
func prepareQuestionAndBehaviorResults(inputs []interactionV2Input, items []interactionplan.BatchItem, prepareQuestions, prepareBehavior func([]interactionV2Input, []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error)) ([]interactionBatchResultV2, []string, error) {
	results := make([]interactionBatchResultV2, len(inputs))
	var issues []string
	for _, kind := range []string{"question", "engagement", "template", "behavior"} {
		var groupInputs []interactionV2Input
		var groupItems []interactionplan.BatchItem
		var positions []int
		for i, in := range inputs {
			route := "behavior"
			switch {
			case interactionQuestionUsesAnswerFlow(in):
				route = "question"
			case in.Template != "":
				route = "template"
			case in.Kind == "engagement":
				route = "engagement"
			}
			if route == kind {
				groupInputs = append(groupInputs, in)
				item := items[i]
				item.ID = fmt.Sprintf("U%02d", len(groupInputs))
				groupItems = append(groupItems, item)
				positions = append(positions, i)
			}
		}
		if len(positions) == 0 {
			continue
		}
		prepare := prepareBehavior
		if kind == "question" {
			prepare = prepareQuestions
		}
		group, groupIssues, err := prepare(groupInputs, groupItems)
		issues = append(issues, groupIssues...)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || (err != nil && len(positions) == len(inputs)) {
			return nil, nil, err
		}
		if err == nil && len(group) != len(positions) {
			err = fmt.Errorf("互动批次结果数量不一致")
		}
		for i, pos := range positions {
			if err != nil {
				results[pos] = interactionBatchResultV2{Task: inputs[pos].Task, Revision: inputs[pos].Revision, Error: err.Error(), Parse: interactionplan.ParseDiagnostics{Outcome: "rejected", Reason: "interaction_generation_failed"}}
			} else {
				results[pos] = group[i]
			}
		}
	}
	return results, issues, nil
}

func (s *Server) prepareInteractionBatchV2(ctx context.Context, batchID string, inputs []interactionV2Input) ([]interactionBatchResultV2, []string, error) {
	// 每条问题都带上该观众最近 5 条弹幕（含时间顺序），意图与回答共用同一份上下文。
	for i := range inputs {
		if inputs[i].Test || strings.TrimSpace(inputs[i].UserID) == "" || !interactionQuestionUsesAnswerFlow(inputs[i]) {
			continue
		}
		turns, histErr := s.store.ListRecentInteractionTurns(ctx, inputs[i].Tenant, inputs[i].Room, inputs[i].UserID, 5)
		if histErr != nil || len(turns) == 0 {
			continue
		}
		history := make([]interactionplan.ContextMessage, 0, len(turns)+2)
		// 先给模型一句边界：历史只用于理解上下文，不能当商品事实引用。
		history = append(history, interactionplan.ContextMessage{
			Role: "system",
			Text: "以下为该观众此前的发言，只用于理解上下文，不作为商品事实。",
		})
		// 购买状态：已下单时“选好点的”是要发货质量，不是选购建议。
		if signal, sigErr := s.store.LatestOrderSignal(ctx, inputs[i].Tenant, inputs[i].Room, inputs[i].UserID); sigErr == nil && signal != "" {
			history = append(history, interactionplan.ContextMessage{
				Role: "system",
				Text: "该观众此前表示已下单（未经核实）：" + signal + "。若他要求挑好的、催发货，按已购买回应，不要再推荐规格或报价。",
			})
		}
		for _, turn := range turns {
			history = append(history, interactionplan.ContextMessage{
				Role: "user",
				Text: turn.At.Format("15:04:05") + " " + turn.Question,
			})
		}
		inputs[i].Context = history
	}
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
	if err := validateAnswerReasoningEffort(inputs); err != nil {
		return nil, nil, err
	}
	bind := interactionPurposeCostBinder(s.store.GetLiveRuntimeByRoom, in.Tenant, in.Room, in.Plan)
	complete := func(callCtx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
		if !in.Test {
			var err error
			callCtx, err = bind(callCtx)
			if err != nil {
				return agentgateway.Response{}, err
			}
			cost := liveCostScope(callCtx)
			if cost.SessionID != session.ID {
				return agentgateway.Response{}, fmt.Errorf("互动场次在生成前已变化")
			}
			cost.Key = fmt.Sprintf("interaction-v2-batch:%d:%s:%s", session.ID, batchID, req.StageCostKey())
			if req.Stage == "interaction_welcome_writer" {
				task, _ := callCtx.Value(welcomeTaskKey{}).(string)
				if task == "" {
					return agentgateway.Response{}, fmt.Errorf("欢迎任务身份缺失")
				}
				cost.Key = fmt.Sprintf("interaction-v2-welcome:%d:%x", session.ID, sha256.Sum256([]byte(task)))
			}
			callCtx = withLiveCostScope(callCtx, cost)
		} else if err := s.checkInteractionTestBatchBudgetV2(callCtx, len(inputs), req.MaxTokens); err != nil {
			return agentgateway.Response{}, err
		}
		// The workbench caps the live request at the leased model's output limit.
		// The batch parser verifies the result for each input within that limit.
		result, _, err := s.completeAnchorStyleRequest(callCtx, "", req, false)
		return result, err
	}
	ctx = interactionplan.WithContextWordSource(ctx, s.store)
	var results []interactionBatchResultV2
	var issues []string
	var writer *decisionexecutor.Worker
	for _, item := range inputs {
		if !decisionexecutor.IsWelcomeKind(item.Kind) {
			continue
		}
		if in.Test {
			plan, planErr := s.store.GetLiveAgentPlan(ctx, in.Tenant, in.Plan)
			if planErr != nil {
				return nil, nil, planErr
			}
			writer = decisionexecutor.New(&planSpeechTestStore{Store: s.store, tenantID: in.Tenant, roomID: in.Room, plan: plan}, nil, nil, nil)
		} else {
			writer = decisionexecutor.New(s.store, nil, nil, nil)
		}
		break
	}
	welcomeStages := make(map[string]*interactionplan.ModelStages)
	results, issues, err = preparedWelcomeResultsV2(ctx, snapshot, session, inputs, items,
		func(callCtx context.Context, current model.LiveRuntimeSession, task, kind string, frozen interactionplan.Snapshot) (string, error) {
			var stages interactionplan.ModelStages
			body, err := writer.PrepareWelcomeV2Body(callCtx, current, task, kind, frozen, func(ctx context.Context, req agentgateway.Request) (agentgateway.Response, error) {
				started := time.Now()
				response, err := complete(ctx, req)
				if err == nil {
					stages.Record(req.Stage, response, time.Since(started))
				}
				return response, err
			})
			welcomeStages[task] = stages.Recorded()
			return body, err
		},
		func(otherInputs []interactionV2Input, otherItems []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
			return prepareQuestionAndBehaviorResults(otherInputs, otherItems,
				func(questions []interactionV2Input, items []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
					return s.prepareAnswerFlowV2(ctx, snapshot, questions, items, complete)
				},
				func(behaviors []interactionV2Input, items []interactionplan.BatchItem) ([]interactionBatchResultV2, []string, error) {
					return preparedBatchResultsV2(interactionplan.WithParseRequest(ctx, batchID), snapshot, behaviors, items, complete)
				})
		})
	if err != nil {
		return nil, nil, err
	}
	if !in.Test {
		current, err := s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || current.ID != session.ID || current.EndedAt != nil || current.Status != "running" {
			return nil, nil, fmt.Errorf("互动场次在生成期间已变化")
		}
	}
	for i := range results {
		if stages := welcomeStages[results[i].Task]; stages != nil && results[i].Envelope.Body != "" {
			results[i].Envelope.ModelStages = stages
		}
	}
	return results, issues, nil
}

func validateAnswerReasoningEffort(inputs []interactionV2Input) error {
	for _, in := range inputs {
		if !interactionQuestionUsesAnswerFlow(in) {
			continue
		}
		switch in.AnswerReasoningEffort {
		case "":
		case "low":
			if in.Test {
				return fmt.Errorf("按次开启低思考仅支持正式互动")
			}
		default:
			return fmt.Errorf("answer_reasoning_effort 仅支持 low，省略时关闭思考")
		}
	}
	return nil
}

func interactionBatchBudgetV2(count, configured, requested int) error {
	if count > 1 && configured < requested {
		return fmt.Errorf("当前互动模型输出上限%d不足以处理本批%d人（请求预算%d）；请提高模型输出上限或减少批次人数", configured, count, requested)
	}
	return nil
}

// Offline tests retain the original review-profile budget check.
func (s *Server) checkInteractionTestBatchBudgetV2(ctx context.Context, count, requested int) error {
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
