package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/agentunderstanding"
	"livecompanion/management/internal/model"
)

func (s *Store) EnsureAgentUnderstandingDefault(ctx context.Context, actorUserID int64) error {
	defaults := agentunderstanding.DefaultPolicy()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_understanding_policies (
			scope_type, scope_id, mode, provider, model, max_context_messages,
			max_tokens, timeout_ms, monthly_budget_tokens, budget_fallback,
			min_confidence, enabled, updated_by_user_id
		) VALUES ('system', 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
		ON DUPLICATE KEY UPDATE id=id
	`,
		defaults.Mode,
		defaults.Provider,
		defaults.Model,
		defaults.MaxContextMessages,
		defaults.MaxTokens,
		defaults.TimeoutMS,
		defaults.MonthlyBudgetTokens,
		defaults.BudgetFallback,
		defaults.MinConfidence,
		actorUserID,
	)
	return err
}

func scanAgentUnderstandingPolicy(scanner interface{ Scan(...any) error }) (model.AgentUnderstandingPolicy, error) {
	var item model.AgentUnderstandingPolicy
	err := scanner.Scan(
		&item.ID,
		&item.ScopeType,
		&item.ScopeID,
		&item.Mode,
		&item.Provider,
		&item.Model,
		&item.MaxContextMessages,
		&item.MaxTokens,
		&item.TimeoutMS,
		&item.MonthlyBudgetTokens,
		&item.BudgetFallback,
		&item.MinConfidence,
		&item.Enabled,
		&item.UpdatedByUserID,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.AgentUnderstandingPolicy{}, err
	}
	return agentunderstanding.NormalizePolicy(item), nil
}

const agentUnderstandingSelect = `
	SELECT id, scope_type, scope_id, mode, provider, model,
	       max_context_messages, max_tokens, timeout_ms, monthly_budget_tokens,
	       budget_fallback, min_confidence, enabled, updated_by_user_id,
	       created_at, updated_at
	FROM agent_understanding_policies`

func (s *Store) ListAgentUnderstandingPolicies(ctx context.Context) ([]model.AgentUnderstandingPolicy, error) {
	rows, err := s.db.QueryContext(ctx, agentUnderstandingSelect+` ORDER BY FIELD(scope_type,'system','membership','tenant'), scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentUnderstandingPolicy, 0)
	for rows.Next() {
		item, err := scanAgentUnderstandingPolicy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAgentUnderstandingPolicy(ctx context.Context, scopeType string, scopeID int64) (model.AgentUnderstandingPolicy, error) {
	scopeType = strings.ToLower(strings.TrimSpace(scopeType))
	row := s.db.QueryRowContext(ctx, agentUnderstandingSelect+` WHERE scope_type=? AND scope_id=? LIMIT 1`, scopeType, scopeID)
	return scanAgentUnderstandingPolicy(row)
}

func validateUnderstandingPolicyInput(input model.AgentUnderstandingPolicyInput) (model.AgentUnderstandingPolicy, error) {
	policy := agentunderstanding.NormalizePolicy(model.AgentUnderstandingPolicy{
		ScopeType:           input.ScopeType,
		ScopeID:             input.ScopeID,
		Mode:                input.Mode,
		Provider:            input.Provider,
		Model:               input.Model,
		MaxContextMessages:  input.MaxContextMessages,
		MaxTokens:           input.MaxTokens,
		TimeoutMS:           input.TimeoutMS,
		MonthlyBudgetTokens: input.MonthlyBudgetTokens,
		BudgetFallback:      input.BudgetFallback,
		MinConfidence:       input.MinConfidence,
		Enabled:             input.Enabled,
	})
	switch policy.ScopeType {
	case model.AgentUnderstandingScopeSystem:
		policy.ScopeID = 0
		policy.Enabled = true
	case model.AgentUnderstandingScopeMembership, model.AgentUnderstandingScopeTenant:
		if policy.ScopeID <= 0 {
			return model.AgentUnderstandingPolicy{}, errors.New("理解策略作用范围 ID 无效")
		}
	default:
		return model.AgentUnderstandingPolicy{}, errors.New("理解策略作用范围无效")
	}
	if input.Mode != model.AgentUnderstandingModeProgram && input.Mode != model.AgentUnderstandingModeModel && input.Mode != model.AgentUnderstandingModeAuto {
		return model.AgentUnderstandingPolicy{}, errors.New("理解模式只能是 program、model 或 auto")
	}
	if policy.MonthlyBudgetTokens < 0 {
		return model.AgentUnderstandingPolicy{}, errors.New("月度理解预算不能小于 0")
	}
	return policy, nil
}

func (s *Store) UpsertAgentUnderstandingPolicy(
	ctx context.Context,
	input model.AgentUnderstandingPolicyInput,
	actorUserID int64,
) (model.AgentUnderstandingPolicy, error) {
	policy, err := validateUnderstandingPolicyInput(input)
	if err != nil {
		return model.AgentUnderstandingPolicy{}, err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO agent_understanding_policies (
			scope_type, scope_id, mode, provider, model, max_context_messages,
			max_tokens, timeout_ms, monthly_budget_tokens, budget_fallback,
			min_confidence, enabled, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			mode=VALUES(mode), provider=VALUES(provider), model=VALUES(model),
			max_context_messages=VALUES(max_context_messages), max_tokens=VALUES(max_tokens),
			timeout_ms=VALUES(timeout_ms), monthly_budget_tokens=VALUES(monthly_budget_tokens),
			budget_fallback=VALUES(budget_fallback), min_confidence=VALUES(min_confidence),
			enabled=VALUES(enabled), updated_by_user_id=VALUES(updated_by_user_id)
	`,
		policy.ScopeType, policy.ScopeID, policy.Mode, policy.Provider, policy.Model,
		policy.MaxContextMessages, policy.MaxTokens, policy.TimeoutMS,
		policy.MonthlyBudgetTokens, policy.BudgetFallback, policy.MinConfidence,
		policy.Enabled, actorUserID,
	)
	if err != nil {
		return model.AgentUnderstandingPolicy{}, err
	}
	return s.GetAgentUnderstandingPolicy(ctx, policy.ScopeType, policy.ScopeID)
}

func (s *Store) DeleteAgentUnderstandingPolicy(ctx context.Context, scopeType string, scopeID int64) error {
	scopeType = strings.ToLower(strings.TrimSpace(scopeType))
	if scopeType == model.AgentUnderstandingScopeSystem {
		return errors.New("系统默认理解策略不能删除")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_understanding_policies WHERE scope_type=? AND scope_id=?`, scopeType, scopeID)
	return err
}

func (s *Store) ResolveAgentUnderstandingPolicy(ctx context.Context, tenantID int64) (model.AgentUnderstandingEffectivePolicy, error) {
	base := model.AgentUnderstandingEffectivePolicy{
		AgentUnderstandingPolicy: agentunderstanding.DefaultPolicy(),
		ResolvedFrom:             "system_default",
		ResolvedScopeID:          0,
	}

	if systemPolicy, err := s.GetAgentUnderstandingPolicy(ctx, model.AgentUnderstandingScopeSystem, 0); err == nil && systemPolicy.Enabled {
		base.AgentUnderstandingPolicy = systemPolicy
		base.ResolvedFrom = model.AgentUnderstandingScopeSystem
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentUnderstandingEffectivePolicy{}, err
	}

	if tenantID <= 0 {
		return base, nil
	}

	cooperationStatus := model.CustomerCooperationStatusCooperating
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(cooperation_status, 'cooperating')
		FROM crm_customer_profiles WHERE tenant_id=? LIMIT 1
	`, tenantID).Scan(&cooperationStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentUnderstandingEffectivePolicy{}, err
	}
	base.CooperationStatus = cooperationStatus

	// Customer override has the highest business priority, including test and
	// special-customer cases explicitly configured by an authorized operator.
	if tenantPolicy, err := s.GetAgentUnderstandingPolicy(ctx, model.AgentUnderstandingScopeTenant, tenantID); err == nil && tenantPolicy.Enabled {
		base.AgentUnderstandingPolicy = tenantPolicy
		base.ResolvedFrom = model.AgentUnderstandingScopeTenant
		base.ResolvedScopeID = tenantID
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentUnderstandingEffectivePolicy{}, err
	} else {
		membership, membershipErr := s.getCurrentMembership(ctx, tenantID)
		if membershipErr == nil {
			base.MembershipPlanID = membership.PlanID
			if membershipPolicy, policyErr := s.GetAgentUnderstandingPolicy(ctx, model.AgentUnderstandingScopeMembership, membership.PlanID); policyErr == nil && membershipPolicy.Enabled {
				base.AgentUnderstandingPolicy = membershipPolicy
				base.ResolvedFrom = model.AgentUnderstandingScopeMembership
				base.ResolvedScopeID = membership.PlanID
			} else if policyErr != nil && !errors.Is(policyErr, sql.ErrNoRows) {
				return model.AgentUnderstandingEffectivePolicy{}, policyErr
			}
		} else if !errors.Is(membershipErr, sql.ErrNoRows) {
			return model.AgentUnderstandingEffectivePolicy{}, membershipErr
		}
	}

	if cooperationStatus == model.CustomerCooperationStatusNonCooperating {
		base.Mode = model.AgentUnderstandingModeProgram
		base.ResolvedFrom = "non_cooperating_fallback"
	}

	if base.MonthlyBudgetTokens > 0 && base.Mode != model.AgentUnderstandingModeProgram {
		used, err := s.SumUnderstandingTokensForTenantMonth(ctx, tenantID, time.Now().UTC())
		if err != nil {
			return model.AgentUnderstandingEffectivePolicy{}, fmt.Errorf("sum understanding budget: %w", err)
		}
		base.BudgetUsedTokens = used
		base.BudgetRemainingTokens = base.MonthlyBudgetTokens - used
		if base.BudgetRemainingTokens < 0 {
			base.BudgetRemainingTokens = 0
		}
		if used >= base.MonthlyBudgetTokens {
			base.BudgetExceeded = true
			base.Mode = model.AgentUnderstandingModeProgram
			base.ResolvedFrom = "budget_fallback"
		}
	}
	base.AgentUnderstandingPolicy = agentunderstanding.NormalizePolicy(base.AgentUnderstandingPolicy)
	return base, nil
}
