package paidpipeline

import (
	"context"
	"encoding/json"
	"fmt"
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
	if !ok || item.PlaybackTaskID != "" {
		return fmt.Errorf("decision_changed")
	}
	s := p.v2Scope(req.TenantID, m.RoomID)
	if v := p.interactionV2.Load(); v != nil {
		reason, remaining, retry := v.engine.PreviewPreparationRetry(s, req.DecisionID, req.DurationMS, p.clock())
		if reason != "" {
			return &audioout.InteractionPreparationError{Reason: reason, RemainingMS: remaining, RetryAfterMS: retry}
		}
	}
	return nil
}
