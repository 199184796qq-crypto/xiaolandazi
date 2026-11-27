package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/answerflow"
	"livecompanion/management/internal/decisionexecutor"
	"livecompanion/management/internal/interactionplan"
	"livecompanion/management/internal/model"
)

func manualAnswerFlowContext(question string, turns []model.InteractionRecentTurn) []interactionplan.ContextMessage {
	var history []interactionplan.ContextMessage
	for _, turn := range turns {
		text := strings.TrimSpace(turn.Question)
		if text != "" && text != question {
			history = append(history, interactionplan.ContextMessage{Role: "user", Text: truncateRunes(text, 120), At: turn.At})
		}
	}
	return history
}

// PrepareManualAnswerFlow is the claimed, human-selected question adapter.
// It uses the same library, intent, retrieval and answer service as a normal
// live question, then hands the frozen body back to the existing playback path.
func (s *Server) PrepareManualAnswerFlow(ctx context.Context, manual decisionexecutor.ManualAnswerFlowInput, complete interactionplan.Complete) (interactionplan.Envelope, error) {
	var empty interactionplan.Envelope
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if manual.Session.ID <= 0 || manual.Task == "" || strings.TrimSpace(manual.Question) == "" ||
		manual.Snapshot.Scope.Tenant != manual.Session.TenantID || manual.Snapshot.Scope.Room != manual.Session.RoomID ||
		manual.Snapshot.Scope.Session != manual.Session.ID {
		return empty, fmt.Errorf("人工问题与冻结场次不匹配")
	}
	in := interactionV2Input{
		Tenant: manual.Session.TenantID, Room: manual.Session.RoomID, Plan: manual.Snapshot.Scope.Plan,
		Task: manual.Task, Epoch: "manual", Revision: 1, Kind: "semantic",
		Text: strings.TrimSpace(manual.Question), Original: strings.TrimSpace(manual.Question),
		CurrentMainline: manual.CurrentMainline, AllowClarify: true, ExpiresAt: manual.ExpiresAt,
	}
	// A merged cluster has no single owner. Never feed one viewer's chat history
	// to another. A single selected viewer gets the same bounded history as the
	// existing live interaction path; the selected question itself is omitted.
	if len(manual.UserIDs) == 1 && strings.TrimSpace(manual.UserIDs[0]) != "" {
		if turns, historyErr := s.store.ListRecentInteractionTurns(ctx, in.Tenant, in.Room, manual.UserIDs[0], 5); historyErr == nil {
			in.Context = manualAnswerFlowContext(in.Text, turns)
		}
	}
	items, err := batchInputsV2(manual.Task, []interactionV2Input{in}, time.Now())
	if err != nil {
		return empty, err
	}
	results, _, err := s.prepareAnswerFlowV2(ctx, manual.Snapshot, []interactionV2Input{in}, items, complete)
	if err != nil {
		return empty, err
	}
	if len(results) != 1 {
		return empty, fmt.Errorf("人工问题回答结果数量无效")
	}
	if results[0].Error != "" {
		return empty, fmt.Errorf("人工问题回答未完成：%s", results[0].Error)
	}
	return results[0].Envelope, nil
}

func interactionQuestionUsesAnswerFlow(in interactionV2Input) bool {
	// Instruction previews exercise their draft behavior prompt. A live room's
	// engagement token alone does not change a viewer question into that activity.
	if in.Template != "" || (in.Test && in.PreviewInstruction != "") {
		return false
	}
	switch in.Kind {
	case "semantic", "dialogue", "urgent", "reply_chat", "chat":
		return true
	default:
		return false
	}
}

func liveAnswerInput(in interactionV2Input) answerflow.Input {
	effort := in.AnswerReasoningEffort
	if effort == "" {
		effort = "low"
	}
	out := answerflow.Input{Question: in.Text, Original: in.Original, Clarification: in.Clarification, CurrentMainline: in.CurrentMainline, AnswerReasoningEffort: effort}
	for _, turn := range in.Recent {
		out.Recent = append(out.Recent, answerflow.Turn{Question: turn.Question, Answer: turn.Answer})
	}
	for _, m := range in.Context {
		out.Context = append(out.Context, answerflow.Message{Role: m.Role, Text: m.Text, At: m.At})
	}
	return out
}

// interactionAnswerer returns one result per candidate in the request order,
// preserving each candidate's ID.
type interactionAnswerer interface {
	AnswerBatch(context.Context, answerflow.Scope, []answerflow.Candidate) (answerflow.BatchResult, error)
}

type liveRetrievedAnswerer struct {
	answerflow.Service
	retrieval answerflow.RetrievalConfig
}

func (a liveRetrievedAnswerer) AnswerBatch(ctx context.Context, scope answerflow.Scope, candidates []answerflow.Candidate) (answerflow.BatchResult, error) {
	return a.AnswerRetrievedBatch(ctx, scope, candidates, a.retrieval)
}

func (s *Server) prepareAnswerFlowV2(ctx context.Context, snapshot interactionplan.Snapshot, inputs []interactionV2Input, items []interactionplan.BatchItem, complete interactionplan.Complete) ([]interactionBatchResultV2, []string, error) {
	var stages interactionplan.ModelStages
	attempts := map[string]int{}
	recordComplete := func(ctx context.Context, request agentgateway.Request) (agentgateway.Response, error) {
		attempts[request.Stage]++
		request.StageAttempt = attempts[request.Stage]
		started := time.Now()
		response, err := complete(ctx, request)
		if err == nil {
			stages.Record(request.Stage, response, time.Since(started))
		}
		return response, err
	}
	// Both automatic and manual questions use this answerer.
	answerer, roomIntro, err := s.newLiveAnswerer(ctx, snapshot, inputs[0], len(inputs), recordComplete)
	if err != nil {
		return nil, nil, err
	}
	results, issues, err := preparedAnswerFlowResultsV2(ctx, snapshot, inputs, items, answerer, roomIntro)
	for i := range results {
		result := &results[i]
		if result.Envelope.Body == "" {
			continue
		}
		resultStages := stages
		if result.Parse.Reason == "qa_pair_match" {
			resultStages.Answer = &interactionplan.ModelStage{Thinking: "no_model"}
		}
		result.Envelope.ModelStages = resultStages.Recorded()
	}
	return results, issues, err
}

func (s *Server) newLiveAnswerer(ctx context.Context, snapshot interactionplan.Snapshot, in interactionV2Input, count int, complete interactionplan.Complete) (interactionAnswerer, string, error) {
	var lib danmakuLibrary
	var retrieval answerflow.RetrievalConfig
	var err error
	if in.Test {
		lib, err = s.loadDanmakuLibrary(ctx, in.Tenant, in.Plan)
	} else {
		lib, retrieval, err = s.loadDanmakuRetrieval(ctx, in.Tenant, in.Plan)
	}
	if err != nil {
		return nil, "", err
	}
	embedder, _ := s.semanticMetrics.(danmakuEmbedder)
	service := newDanmakuAnswerService(lib, embedder, nil)
	service.QuestionLimit = 2400
	roomIntro, systemPrefix, err := s.loadAnswerPlanContext(ctx, in.Tenant, in.Plan)
	if err != nil {
		return nil, "", err
	}
	service.AnswerSystemPrefix = systemPrefix
	service.ValidateQA = func(answer string) error { return interactionplan.Validate(snapshot, answer) }
	completeStage := func(ctx context.Context, stage answerflow.Stage, messages []agentgateway.Message, effort string) (agentgateway.Response, error) {
		return complete(ctx, liveAnswerFlowRequest(stage, messages, count, effort))
	}
	service.Complete = func(ctx context.Context, stage answerflow.Stage, messages []agentgateway.Message) (agentgateway.Response, error) {
		return completeStage(ctx, stage, messages, "")
	}
	if !in.Test {
		service.CompleteAnswer = func(ctx context.Context, effort string, messages []agentgateway.Message) (agentgateway.Response, error) {
			return completeStage(ctx, answerflow.AnswerStage, messages, effort)
		}
	}
	service.Guard = func(ctx context.Context, scope answerflow.Scope, _ answerflow.Point) error {
		if scope.Tenant != snapshot.Scope.Tenant || scope.Plan != snapshot.Scope.Plan {
			return fmt.Errorf("回答流程身份已变化")
		}
		if in.Test {
			return nil
		}
		current, err := s.store.GetLiveRuntimeByRoom(ctx, in.Tenant, in.Room)
		if err != nil || current.ID != snapshot.Scope.Session || current.Status != "running" || current.EndedAt != nil {
			return fmt.Errorf("回答流程场次已变化")
		}
		now, err := interactionplan.Freeze(ctx, s.store, current)
		if err != nil || now.Fingerprint != snapshot.Fingerprint {
			return fmt.Errorf("回答流程方案版本已变化")
		}
		return ctx.Err()
	}
	// The existing vector adapter batches at most eight queries. Recheck at the
	// actual call boundary as well as the service's orchestration boundary.
	if embedder != nil {
		checked := answerFlowEmbedder{danmakuEmbedder: embedder, check: func(ctx context.Context) error {
			return service.Guard(ctx, answerflow.Scope{Tenant: in.Tenant, Plan: in.Plan}, answerflow.BeforeEmbedding)
		}}
		service.Embed = newDanmakuAnswerService(lib, checked, nil).Embed
	}
	if in.Test {
		return service, roomIntro, nil
	}
	configureRetrievedDanmakuPrompts(&service)
	if rank := retrieval.Rerank; rank != nil {
		retrieval.Rerank = func(ctx context.Context, query string, texts []string) ([]float64, error) {
			if err := service.Guard(ctx, answerflow.Scope{Tenant: in.Tenant, Plan: in.Plan}, answerflow.BeforeRerank); err != nil {
				return nil, err
			}
			return rank(ctx, query, texts)
		}
	}
	return liveRetrievedAnswerer{Service: service, retrieval: retrieval}, roomIntro, nil
}

func liveAnswerFlowRequest(stage answerflow.Stage, messages []agentgateway.Message, count int, effort string) agentgateway.Request {
	request := agentgateway.Request{Stage: "interaction_answerflow_" + string(stage), Messages: messages, MaxTokens: 4000,
		ResponseFormat: agentgateway.ResponseJSON, Timeout: 27 * time.Second, CacheSystemPrompt: true}
	if stage == answerflow.AnswerStage {
		request.ResponseFormat = agentgateway.ResponseText
		request.ReasoningEffort = effort
	}
	if stage == answerflow.BatchIntentStage {
		request.MaxTokens = 900 * count
	}
	return request
}

type answerFlowEmbedder struct {
	danmakuEmbedder
	check func(context.Context) error
}

func (e answerFlowEmbedder) EmbedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := e.check(ctx); err != nil {
		return nil, err
	}
	return e.danmakuEmbedder.EmbedTexts(ctx, texts)
}

func preparedAnswerFlowResultsV2(ctx context.Context, snapshot interactionplan.Snapshot, inputs []interactionV2Input, items []interactionplan.BatchItem, answerer interactionAnswerer, roomIntro string) ([]interactionBatchResultV2, []string, error) {
	if len(inputs) != len(items) || len(inputs) == 0 {
		return nil, nil, fmt.Errorf("回答批次映射无效")
	}
	candidates := make([]answerflow.Candidate, len(inputs))
	for i, in := range inputs {
		if in.Tenant != snapshot.Scope.Tenant || in.Plan != snapshot.Scope.Plan || in.Room != snapshot.Scope.Room {
			return nil, nil, fmt.Errorf("回答批次与冻结方案不匹配")
		}
		input := liveAnswerInput(in)
		input.RoomIntro = roomIntro
		candidates[i] = answerflow.Candidate{ID: items[i].ID, Input: input, ExpiresAt: in.ExpiresAt}
	}
	result, err := answerer.AnswerBatch(ctx, answerflow.Scope{Tenant: snapshot.Scope.Tenant, Plan: snapshot.Scope.Plan}, candidates)
	if err != nil {
		return nil, nil, err
	}
	if len(result.Items) != len(candidates) {
		return nil, nil, fmt.Errorf("回答结果数量与请求不匹配")
	}
	out := make([]interactionBatchResultV2, len(inputs))
	for i, in := range inputs {
		c := result.Items[i]
		if c.ID != candidates[i].ID {
			return nil, nil, fmt.Errorf("回答结果编号与请求不匹配")
		}
		intent := interactionplan.Intent{Meaning: c.Intent.Meaning, Type: "闲聊", Priority: "普通", Relevant: false}
		if c.Intent.Category == "问题" || c.Intent.SpeechAct == "提问" {
			intent.Type = "提问"
			intent.Question = true
		}
		if c.Intent.Category == "请求" || c.Intent.SpeechAct == "请求" {
			intent.Type = "请求"
			intent.Question = true
		}
		if c.Intent.Category == "投诉" || c.Intent.SpeechAct == "投诉" {
			intent.Type = "投诉"
		}
		body := ""
		reason := "batch_not_selected"
		if c.Reply != nil {
			if !in.ExpiresAt.IsZero() && !time.Now().Before(in.ExpiresAt) {
				return nil, nil, context.DeadlineExceeded
			}
			body = c.Reply.Answer
			intent.Relevant = true
			if c.Reply.InteractionIntent != nil {
				intent = *c.Reply.InteractionIntent
			}
			if c.Reply.Action == "clarify" {
				if !in.AllowClarify {
					return nil, nil, fmt.Errorf("当前任务不允许发起澄清")
				}
				intent.NeedClarify = true
				intent.Clarify = body
			}
			if body != "" {
				if err := interactionplan.Validate(snapshot, body); err != nil {
					return nil, nil, err
				}
			}
			reason = "answerflow_body_ready"
		}
		envelope := interactionplan.Envelope{Protocol: interactionplan.Protocol, Task: in.Task, Epoch: in.Epoch, Revision: in.Revision, Tenant: in.Tenant, Room: in.Room, Session: snapshot.Scope.Session, Plan: in.Plan, Fingerprint: snapshot.Fingerprint, Body: body, Intent: intent, ExpiresAt: in.ExpiresAt}
		outcome := "accepted"
		if c.Reply == nil {
			outcome = "skipped"
		}
		parse := interactionplan.ParseDiagnostics{Outcome: outcome, Reason: reason}
		if c.Reply != nil && c.Reply.AnswerParse != nil {
			parse = *c.Reply.AnswerParse
		}
		out[i] = interactionBatchResultV2{Task: in.Task, Revision: in.Revision, Envelope: envelope, Parse: parse}
	}
	return out, nil, nil
}

func (s *Server) loadAnswerPlanContext(ctx context.Context, tenant, plan int64) (roomIntro, systemPrefix string, err error) {
	intro, err := s.store.LiveAgentPlanRoomIntro(ctx, tenant, plan)
	if err != nil {
		return "", "", fmt.Errorf("读取方案直播简介失败: %w", err)
	}
	doc, err := s.store.GetLiveAgentPlanPromptDocument(ctx, tenant, plan)
	if err != nil {
		return "", "", fmt.Errorf("读取方案互动提示词失败: %w", err)
	}
	return truncateRunes(strings.TrimSpace(intro), answerflow.RoomIntroLimit), answerPlanBlock(doc.Sections.PersonaMD, doc.Sections.RedLinesMD, doc.InteractionGuidanceMD), nil
}

func answerPlanBlock(persona, redLines, interaction string) string {
	sections := make([]string, 0, 3)
	for _, section := range []struct{ title, text string }{
		{"主播人设", persona}, {"红线禁止", redLines}, {"互动要求", interaction},
	} {
		if body := strings.TrimSpace(section.text); body != "" {
			sections = append(sections, "【"+section.title+"】\n"+body)
		}
	}
	return strings.Join(sections, "\n\n")
}
