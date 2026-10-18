package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/anchorcycle"
	"time"
)

type pendingVoiceFailure struct{ Stage, Reason string }

// Only failures known to precede the Core call can safely return a quota.
// A timeout/invalid acknowledgement after submission needs reconciliation.
type mainlineNotSubmitted struct{ cause error }

func (e *mainlineNotSubmitted) Error() string { return e.cause.Error() }
func (e *mainlineNotSubmitted) Unwrap() error { return e.cause }
func beforeMainlineSubmit(err error) error    { return &mainlineNotSubmitted{cause: err} }

// Voice and text run concurrently. An in-flight text lease must not cause a
// lost refund, nor may a stale voice snapshot overwrite a newer adoption.
func (p *cycleSupply) flushVoiceFailures(ctx context.Context) error {
	p.mu.Lock()
	pending := make(map[string]pendingVoiceFailure, len(p.voiceFailures))
	for id, failure := range p.voiceFailures {
		pending[id] = failure
	}
	p.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	token := anchorcycle.ID()
	r := p.s.anchorCycles
	if err := r.Acquire(ctx, p.initial.TenantID, p.initial.PlanID, p.initial.RoomID, token); err != nil {
		if errors.Is(err, anchorcycle.ErrBusy) {
			return nil
		}
		return err
	}
	defer r.Release(context.WithoutCancel(ctx), p.initial.TenantID, p.initial.PlanID, p.initial.RoomID, token)
	latest, err := r.Get(ctx, p.initial.TenantID, p.initial.PlanID, p.initial.RoomID)
	if err != nil {
		return err
	}
	if latest.RunID != p.initial.RunID {
		return anchorcycle.ErrStale
	}
	changed := false
	for id, f := range pending {
		changed = latest.RecordSegmentDeliveryFailure(id, f.Stage, f.Reason, f.Stage != "delivery_unconfirmed", time.Now()) || changed
	}
	if changed {
		if err = r.Save(ctx, latest); err != nil {
			if errors.Is(err, anchorcycle.ErrStale) || errors.Is(err, anchorcycle.ErrBusy) {
				return nil
			}
			return err
		}
	}
	p.mu.Lock()
	for id, f := range pending {
		if p.voiceFailures[id] == f {
			delete(p.voiceFailures, id)
		}
	}
	p.mu.Unlock()
	p.receive(latest)
	return nil
}
