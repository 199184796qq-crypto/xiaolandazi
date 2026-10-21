package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/contentstrategy"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

// intentTaskAngles extracts the task_angles the purpose resolver already mined
// for this question. These are the purpose-scoped angle candidates the plan
// step should choose from, not re-derive.
func intentTaskAngles(knowledge json.RawMessage) []contentstrategy.Row {
	if len(knowledge) == 0 {
		return nil
	}
	var purpose struct {
		TaskAngles []contentstrategy.Row `json:"task_angles"`
	}
	if err := json.Unmarshal(knowledge, &purpose); err != nil {
		return nil
	}
	return purpose.TaskAngles
}

type interactionExchangeStore interface {
	BeginModelExchange(context.Context, appdb.ModelExchangeScope, string, string, any) (int64, error)
	CompleteModelExchange(context.Context, int64, any, string, int64, *int64, *int64, *int64, error) error
}

var _ interactionExchangeStore = (*appdb.Store)(nil)

func (w *Worker) completeInteractionAnswer(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request) (agentgateway.Response, error) {
	if session.ID > 0 {
		ctx = agentgateway.WithRoomModelScope(ctx, agentgateway.RoomModelScope{TenantID: session.TenantID, RoomID: session.RoomID, RuntimeID: session.ID, RequireLease: true})
	}
	source, ok := w.store.(interactionExchangeStore)
	if !ok {
		return w.agent.Complete(ctx, request)
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return agentgateway.Response{}, err
	}
	identity := ""
	if gateway, ok := w.agent.(interface {
		ModelRoleIdentity(context.Context, string) (string, error)
	}); ok {
		identity, err = gateway.ModelRoleIdentity(ctx, request.Stage)
		if err != nil {
			return agentgateway.Response{}, err
		}
	} else if gateway, ok := w.agent.(interface {
		TextGenerationIdentity(context.Context) (string, error)
	}); ok {
		identity, err = gateway.TextGenerationIdentity(ctx)
		if err != nil {
			return agentgateway.Response{}, err
		}
	}
	stage := request.Stage
	id, err := source.BeginModelExchange(ctx, appdb.ModelExchangeScope{TenantID: session.TenantID, RoomID: session.RoomID, PlanID: plan.ID, RunID: session.ExternalID}, stage, identity, request)
	if err != nil {
		return agentgateway.Response{}, fmt.Errorf("互动请求记录失败: %w", err)
	}
	started := time.Now()
	response, callErr := w.agent.Complete(ctx, request)
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), 8*time.Second)
	defer cancel()
	var input, output, cached *int64
	if response.InputTokens > 0 {
		input = &response.InputTokens
	}
	if response.OutputTokens > 0 {
		output = &response.OutputTokens
	}
	if response.CachedInputTokens > 0 {
		cached = &response.CachedInputTokens
	}
	if err := source.CompleteModelExchange(persist, id, response, response.Text, time.Since(started).Milliseconds(), input, output, cached, callErr); err != nil {
		return agentgateway.Response{}, fmt.Errorf("互动返回记录失败: %w", err)
	}
	return response, callErr
}

func isViewerQuestionAnswer(item decisionItem) bool {
	if len(item.SampleQuestions) == 0 || strings.EqualFold(item.ExecutionMode, "verbatim") {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.ManualOrigin)) {
	case "agent_input", "agent_input_preview":
		return false
	}
	switch strings.ToLower(strings.TrimSpace(item.MissionKind)) {
	case "", "reply_question", "reply_chat":
		return true
	}
	return item.Scene == "product_question" || item.Scene == "complaint"
}

type InteractionRecentAnswer struct {
	Question string `json:"question"`
	Purpose  string `json:"purpose"`
	Angle    string `json:"angle"`
	Reply    string `json:"reply"`
}

func ValidateInteractionRecentAnswers(items []InteractionRecentAnswer) error {
	if len(items) > 8 {
		return fmt.Errorf("近期测试记录最多8条")
	}
	for _, item := range items {
		if strings.TrimSpace(item.Question) == "" || utf8.RuneCountInString(item.Question) > 1000 || utf8.RuneCountInString(item.Reply) > 1000 || utf8.RuneCountInString(item.Purpose) > 180 || utf8.RuneCountInString(item.Angle) > 180 {
			return fmt.Errorf("近期测试记录无效或过长")
		}
	}
	return nil
}
