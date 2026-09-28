package model

import "time"

const (
	AgentUnderstandingScopeSystem     = "system"
	AgentUnderstandingScopeMembership = "membership"
	AgentUnderstandingScopeTenant     = "tenant"

	AgentUnderstandingModeProgram = "program"
	AgentUnderstandingModeModel   = "model"
	AgentUnderstandingModeAuto    = "auto"

	AgentUnderstandingBudgetFallbackProgram = "program"
)

// AgentUnderstandingPolicy controls only natural-language interpretation.
// It never grants execution permission and never bypasses confirmation or
// command-executor authorization.
type AgentUnderstandingPolicy struct {
	ID                  int64     `json:"id"`
	ScopeType           string    `json:"scope_type"`
	ScopeID             int64     `json:"scope_id"`
	Mode                string    `json:"mode"`
	Provider            string    `json:"provider"`
	Model               string    `json:"model"`
	MaxContextMessages  int       `json:"max_context_messages"`
	MaxTokens           int       `json:"max_tokens"`
	TimeoutMS           int       `json:"timeout_ms"`
	MonthlyBudgetTokens int64     `json:"monthly_budget_tokens"`
	BudgetFallback      string    `json:"budget_fallback"`
	MinConfidence       float64   `json:"min_confidence"`
	Enabled             bool      `json:"enabled"`
	UpdatedByUserID     int64     `json:"updated_by_user_id"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

type AgentUnderstandingPolicyInput struct {
	ScopeType           string  `json:"scope_type"`
	ScopeID             int64   `json:"scope_id"`
	Mode                string  `json:"mode"`
	Provider            string  `json:"provider"`
	Model               string  `json:"model"`
	MaxContextMessages  int     `json:"max_context_messages"`
	MaxTokens           int     `json:"max_tokens"`
	TimeoutMS           int     `json:"timeout_ms"`
	MonthlyBudgetTokens int64   `json:"monthly_budget_tokens"`
	BudgetFallback      string  `json:"budget_fallback"`
	MinConfidence       float64 `json:"min_confidence"`
	Enabled             bool    `json:"enabled"`
}

type AgentUnderstandingEffectivePolicy struct {
	AgentUnderstandingPolicy
	ResolvedFrom          string `json:"resolved_from"`
	ResolvedScopeID       int64  `json:"resolved_scope_id"`
	BudgetUsedTokens      int64  `json:"budget_used_tokens"`
	BudgetRemainingTokens int64  `json:"budget_remaining_tokens"`
	BudgetExceeded        bool   `json:"budget_exceeded"`
	MembershipPlanID      int64  `json:"membership_plan_id,omitempty"`
	CooperationStatus     string `json:"cooperation_status,omitempty"`
}
