package questioncluster

import (
	"context"
	"fmt"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
)

type PaidModelCompleter func(context.Context, model.LiveRuntimeSession, agentgateway.Request, int64, string, func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error)

type liveCostPlanSource interface {
	GetLiveAgentPlanForRoom(context.Context, int64, int64) (model.LiveAgentPlan, error)
}

func (w *Worker) completePaidModel(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request, maxEventID int64) (agentgateway.Response, error) {
	if w.paidModelCompleter == nil || session.ID <= 0 {
		return w.agent.Complete(ctx, request)
	}
	source, ok := w.store.(liveCostPlanSource)
	if !ok {
		return agentgateway.Response{}, fmt.Errorf("问题归类费用缺少本房间方案来源")
	}
	plan, err := source.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return agentgateway.Response{}, err
	}
	if plan.ID <= 0 {
		return agentgateway.Response{}, fmt.Errorf("问题归类费用缺少有效方案身份")
	}
	key := fmt.Sprintf("question-cluster:%d:%s:%d:question_cluster", session.ID, session.ExternalID, maxEventID)
	return w.paidModelCompleter(ctx, session, request, plan.ID, key, w.agent.Complete)
}
