package livecost

import (
	"context"
	"errors"
	"time"
)

type policyContextKey struct{}

// AccountingPolicy is the application policy: hourly windows are retained for
// reporting only. Saved legacy targets cannot reactivate monetary admission.
func AccountingPolicy() Policy {
	return Policy{Configured: true, AccountingOnly: true, Window: time.Hour}
}

// WithPolicy fixes the budget used by one scheduling read or request admission.
// It never mutates a shared Controller. Invalid explicit settings fail closed
// for optional work; only legacy necessary requests may degrade. Strict room
// budgets always fail closed, including necessary requests.
// SpoolDir is always owned by the Controller, never by a context override.
func WithPolicy(ctx context.Context, policy Policy) context.Context {
	return context.WithValue(ctx, policyContextKey{}, policy)
}

// NormalizePolicy keeps zero-value legacy constructors compatible. Explicit
// configured zeroes disable optional work instead of restoring default limits.
func NormalizePolicy(policy Policy) (Policy, error) {
	if policy.AccountingOnly {
		if policy.Window <= 0 {
			policy.Window = time.Hour
		}
		if policy.RoomBudget != nil {
			if err := policy.RoomBudget.Validate(); err != nil {
				return Policy{}, err
			}
			copy := *policy.RoomBudget
			policy.RoomBudget = &copy
		}
		return policy, nil
	}
	if policy.RoomBudget != nil {
		if err := policy.RoomBudget.Validate(); err != nil {
			return Policy{}, err
		}
		copy := *policy.RoomBudget
		policy.RoomBudget = &copy
		if policy.Window != time.Hour || policy.TotalTargetMicros != copy.Total || policy.MainlineTargetMicros != copy.Mainline || policy.OptionalLimitMicros != copy.Total-copy.Safety {
			return Policy{}, errors.New("房间费用闸门与分类限额不一致")
		}
	}
	if !policy.Configured {
		defaults := DefaultPolicy()
		if policy.Window <= 0 {
			policy.Window = defaults.Window
		}
		if policy.OptionalLimitMicros <= 0 {
			policy.OptionalLimitMicros = defaults.OptionalLimitMicros
		}
		if policy.TotalTargetMicros <= 0 {
			policy.TotalTargetMicros = defaults.TotalTargetMicros
		}
		if policy.MainlineTargetMicros <= 0 {
			policy.MainlineTargetMicros = defaults.MainlineTargetMicros
		}
		policy.MainlineTargetMicros = min(policy.MainlineTargetMicros, policy.OptionalLimitMicros, policy.TotalTargetMicros)
	}
	if policy.Window <= 0 || policy.TotalTargetMicros <= 0 || policy.MainlineTargetMicros < 0 || policy.OptionalLimitMicros < 0 ||
		(policy.Configured && (policy.MainlineTargetMicros > policy.OptionalLimitMicros || policy.OptionalLimitMicros > policy.TotalTargetMicros)) {
		return Policy{}, errors.New("费用预算设置无效")
	}
	return policy, nil
}

func (controller *Controller) policyForContext(ctx context.Context) (Policy, error) {
	policy := DefaultPolicy()
	if controller != nil {
		policy = controller.policy
	}
	if override, ok := ctx.Value(policyContextKey{}).(Policy); ok {
		override.SpoolDir = policy.SpoolDir
		policy = override
	}
	return NormalizePolicy(policy)
}
