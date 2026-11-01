package ttsgateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"livecompanion/management/internal/ttsscheduler"
)

type synthesisScheduler interface {
	Acquire(context.Context, ttsscheduler.Work) (ttsscheduler.Permit, error)
}

var defaultSynthesisScheduler struct {
	sync.RWMutex
	value synthesisScheduler
}

// ConfigureScheduler runs once at application startup, before any workers.
// NewFromEnv callers share it; independent test gateways created by New do not.
func ConfigureScheduler(s synthesisScheduler) {
	defaultSynthesisScheduler.Lock()
	defer defaultSynthesisScheduler.Unlock()
	defaultSynthesisScheduler.value = s
}
func currentScheduler() synthesisScheduler {
	defaultSynthesisScheduler.RLock()
	defer defaultSynthesisScheduler.RUnlock()
	return defaultSynthesisScheduler.value
}

// With a pool, credentials are selected after quota admission. A missing
// legacy environment key must not prevent an enabled pool account from serving.
func (g *Gateway) UsesAccountPool() bool { return g.scheduler != nil }

type queueScopeKey struct{}
type synthesisTimeoutKey struct{}

func WithQueueScope(ctx context.Context, scope ttsscheduler.Scope) context.Context {
	return context.WithValue(ctx, queueScopeKey{}, scope)
}

// WithSynthesisTimeout starts the provider deadline after queue admission.
// The parent cancellation/deadline still governs the entire operation.
func WithSynthesisTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, synthesisTimeoutKey{}, timeout)
}
func (g *Gateway) scheduledSynthesis(ctx context.Context, p Provider, request SynthesizeRequest) (result SynthesizeResponse, err error) {
	if g.scheduler != nil && (request.Provider == ProviderQwen || request.Provider == ProviderCosyVoice || request.Provider == ProviderVolcengine) {
		scope, _ := ctx.Value(queueScopeKey{}).(ttsscheduler.Scope)
		required := ""
		if request.LongText != nil {
			if state, e := readLongTextState(request.LongText.StatePath); e == nil {
				required = state.AccountID
			}
		}
		permit, claimErr := g.scheduler.Acquire(ctx, ttsscheduler.Work{Model: request.Model, Provider: request.Provider, VoiceID: request.VoiceID, Scope: scope, RequiredAccountID: required})
		if claimErr != nil {
			return result, notSent(claimErr)
		}
		defer func() {
			status := "completed"
			if err != nil {
				status = "failed"
			}
			permit.Finish(status)
		}()
		if permit.AccountID != ttsscheduler.CurrentAccountID {
			adapter, ok := p.(accountProvider)
			if !ok {
				return result, notSent(errors.New("该供应商不支持账户池凭据注入"))
			}
			p = adapter.withAccountKey(permit.APIKey)
		}
		if permit.VoiceID != "" {
			request.VoiceID = permit.VoiceID
		}
		if request.LongText != nil {
			job := *request.LongText
			job.AccountID = permit.AccountID
			request.LongText = &job
		}
	}
	if err = ctx.Err(); err != nil {
		return result, notSent(err)
	}
	if timeout, _ := ctx.Value(synthesisTimeoutKey{}).(time.Duration); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	if request.LongText != nil {
		if _, e := readLongTextState(request.LongText.StatePath); e == nil {
			// Querying an accepted task is not another paid synthesis. Retain
			// the original conservative/unknown ledger entry for reconciliation.
			result, err = p.SynthesizeURL(ctx, request)
			result.UsageSource = synthesisUsageSource(result.Usage)
			result.AccountingPending = true
			return result, err
		}
	}
	return observeSynthesis(ctx, p, request)
}
