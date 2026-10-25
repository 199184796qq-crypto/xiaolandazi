package decisionexecutor

import (
	"context"
	"fmt"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

// Adapters are configured once before Run. They share the same room ledger as
// mainline supply without making this worker depend on HTTP handlers.
type PaidRequestContextFactory func(context.Context, model.LiveRuntimeSession, string, int64, string) context.Context
type PaidModelCompleter func(context.Context, model.LiveRuntimeSession, agentgateway.Request, int64, string, func(context.Context, agentgateway.Request) (agentgateway.Response, error)) (agentgateway.Response, error)

func (w *Worker) SetPaidRequestAdapters(contextFactory PaidRequestContextFactory, modelCompleter PaidModelCompleter) {
	if w == nil {
		return
	}
	w.paidContextFactory = contextFactory
	w.paidModelCompleter = modelCompleter
}

type workerPaidScopeKey struct{}
type workerPaidOptOutKey struct{}
type workerPaidScope struct {
	session model.LiveRuntimeSession
	planID  int64
	key     string
}

func withWorkerPaidScope(ctx context.Context, session model.LiveRuntimeSession, planID int64, key string) context.Context {
	return context.WithValue(ctx, workerPaidScopeKey{}, workerPaidScope{session: session, planID: planID, key: key})
}
func paidWorkerOptOut(ctx context.Context) bool {
	optOut, _ := ctx.Value(workerPaidOptOutKey{}).(bool)
	return optOut
}
func (w *Worker) workerPaidScope(ctx context.Context, session model.LiveRuntimeSession, key string) (workerPaidScope, error) {
	scope, _ := ctx.Value(workerPaidScopeKey{}).(workerPaidScope)
	if scope.session.ID == session.ID && scope.session.TenantID == session.TenantID && scope.session.RoomID == session.RoomID && scope.planID > 0 {
		return scope, nil
	}
	plan, err := w.store.GetLiveAgentPlanForRoom(ctx, session.TenantID, session.RoomID)
	if err != nil {
		return workerPaidScope{}, err
	}
	if plan.ID <= 0 {
		return workerPaidScope{}, fmt.Errorf("互动费用缺少本房间真实方案身份")
	}
	return workerPaidScope{session: session, planID: plan.ID, key: key}, nil
}
func (w *Worker) completePaidModel(ctx context.Context, session model.LiveRuntimeSession, request agentgateway.Request, key string) (agentgateway.Response, error) {
	if w.paidModelCompleter == nil || session.ID <= 0 || paidWorkerOptOut(ctx) {
		return w.agent.Complete(ctx, request)
	}
	scope, err := w.workerPaidScope(ctx, session, key)
	if err != nil {
		return agentgateway.Response{}, err
	}
	// Payloads may change due to randomized addressing or rewriting. They are
	// contract fingerprints, never a new operation after an unknown receipt.
	logical := fmt.Sprintf("interaction:%d:%s:%s:%s:%s", session.ID, session.ExternalID, scope.key, key, request.Stage)
	return w.paidModelCompleter(ctx, session, request, scope.planID, logical, w.agent.Complete)
}
func (w *Worker) synthesizePaidAudio(ctx context.Context, session model.LiveRuntimeSession, stage, key string, request ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	if w.paidContextFactory == nil || session.ID <= 0 || paidWorkerOptOut(ctx) {
		return w.tts.SynthesizeURL(ctx, request)
	}
	scope, err := w.workerPaidScope(ctx, session, key)
	if err != nil {
		return ttsgateway.SynthesizeResponse{}, err
	}
	logical := fmt.Sprintf("interaction:%d:%s:%s:%s:%s", session.ID, session.ExternalID, scope.key, stage, key)
	ctx = w.paidContextFactory(ctx, session, stage, scope.planID, logical)
	return w.tts.SynthesizeURL(ctx, request)
}
