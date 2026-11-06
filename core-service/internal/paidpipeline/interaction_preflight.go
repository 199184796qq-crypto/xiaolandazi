package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/interactionruntime"
	"livecompanion/core/internal/messagecaptain"
	"time"
)

func (p *Processor) SetPreparationReader(fn func(context.Context, audioout.InteractionPreflightRequest) audioout.InteractionPreflightResult) {
	p.scheduleMu.Lock()
	p.preparationReader = fn
	p.scheduleMu.Unlock()
}
func (p *Processor) preparationCheck(ctx context.Context, s interactionruntime.Scope, decision string, duration int, at time.Time) audioout.InteractionPreflightResult {
	r := audioout.InteractionPreflightResult{Allowed: true, CheckedAt: at, RetryAfterMS: 5000}
	if v := p.interactionV2.Load(); v != nil {
		r.Reason, r.RemainingMS, r.RetryAfterMS = v.engine.PreviewPreparationRetry(s, decision, duration, at)
		r.Allowed = r.Reason == ""
	}
	p.scheduleMu.Lock()
	reader := p.preparationReader
	p.scheduleMu.Unlock()
	if reader != nil {
		window := reader(ctx, audioout.InteractionPreflightRequest{TenantID: s.Tenant, RoomID: s.Room, PlanID: s.Plan, Epoch: s.Epoch, DecisionID: decision, DurationMS: duration, Stage: "model"})
		if !window.Allowed {
			return window
		}
		if r.Allowed {
			window.RemainingMS = r.RemainingMS
			return window
		}
	}
	return r
}
func (p *Processor) preflightAudio(m messagecaptain.Message) error {
	var req audioout.InteractionPreflightRequest
	if json.Unmarshal(m.Payload, &req) != nil {
		return fmt.Errorf("invalid_preflight")
	}
	item, ok := p.decisions.Get(m.RoomID, req.DecisionID)
	// Claim admission runs before the atomic ClaimExpected transition. Paid
	// preparation runs afterwards and must continue requiring an owned claim.
	expected := agentdecision.StatusClaimed
	if req.Stage == "claim" {
		expected = agentdecision.StatusPending
	}
	if !ok || item.Status != expected || item.PlaybackTaskID != "" {
		return fmt.Errorf("decision_changed")
	}
	s := p.v2Scope(req.TenantID, m.RoomID)
	if req.Epoch != s.Epoch || !p.runtime.IsWorking(m.RoomID) {
		return fmt.Errorf("run_changed")
	}
	if v := p.interactionV2.Load(); v != nil {
		now := p.clock()
		v.engine.SetPolicy(s, p.v2Policy(s), now)
		reason, remaining, retry := v.engine.PreviewPreparationRetry(s, req.DecisionID, req.DurationMS, now)
		if reason != "" {
			return &audioout.InteractionPreparationError{Reason: reason, RemainingMS: remaining, RetryAfterMS: retry}
		}
	}
	return nil
}
