package model

import "time"

type IncentiveRule struct {
	ID               int64     `json:"id"`
	ProgramVersionID int64     `json:"program_version_id"`
	Priority         int       `json:"priority"`
	EventType        string    `json:"event_type"`
	ConditionsJSON   string    `json:"conditions_json"`
	ActionType       string    `json:"action_type"`
	ActionConfigJSON string    `json:"action_config_json"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type IncentiveProgramVersion struct {
	ID              int64           `json:"id"`
	ProgramID       int64           `json:"program_id"`
	VersionNo       uint32          `json:"version_no"`
	LifecycleStatus string          `json:"lifecycle_status"`
	PendingDays     uint32          `json:"pending_days"`
	EffectiveFrom   *time.Time      `json:"effective_from,omitempty"`
	EffectiveTo     *time.Time      `json:"effective_to,omitempty"`
	PublishedAt     *time.Time      `json:"published_at,omitempty"`
	CreatedAt       time.Time       `json:"created_at"`
	Rules           []IncentiveRule `json:"rules"`
}

type IncentiveProgram struct {
	ID            int64                    `json:"id"`
	Code          string                   `json:"code"`
	Name          string                   `json:"name"`
	ProgramType   string                   `json:"program_type"`
	Status        string                   `json:"status"`
	Description   string                   `json:"description"`
	CreatedAt     time.Time                `json:"created_at"`
	UpdatedAt     time.Time                `json:"updated_at"`
	ActiveVersion *IncentiveProgramVersion `json:"active_version,omitempty"`
	DraftVersion  *IncentiveProgramVersion `json:"draft_version,omitempty"`
	LatestVersion *IncentiveProgramVersion `json:"latest_version,omitempty"`
}

type IncentiveRuleInput struct {
	Priority         int    `json:"priority"`
	EventType        string `json:"event_type"`
	ConditionsJSON   string `json:"conditions_json"`
	ActionType       string `json:"action_type"`
	ActionConfigJSON string `json:"action_config_json"`
	Enabled          bool   `json:"enabled"`
}

type IncentiveProgramInput struct {
	Code        string               `json:"code"`
	Name        string               `json:"name"`
	ProgramType string               `json:"program_type"`
	Description string               `json:"description"`
	PendingDays uint32               `json:"pending_days"`
	Rules       []IncentiveRuleInput `json:"rules"`
}

type IncentiveEarning struct {
	ID               int64      `json:"id"`
	ExternalID       string     `json:"external_id"`
	BeneficiaryType  string     `json:"beneficiary_type"`
	BeneficiaryID    int64      `json:"beneficiary_id"`
	EarningType      string     `json:"earning_type"`
	SourceOrderID    *int64     `json:"source_order_id,omitempty"`
	SourceRefundID   *int64     `json:"source_refund_id,omitempty"`
	ProgramVersionID *int64     `json:"program_version_id,omitempty"`
	RuleID           *int64     `json:"rule_id,omitempty"`
	Currency         string     `json:"currency"`
	AmountCents      int64      `json:"amount_cents"`
	QuotaSeconds     int64      `json:"quota_seconds"`
	Status           string     `json:"status"`
	AvailableAt      *time.Time `json:"available_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type SettlementBatch struct {
	ID                    int64      `json:"id"`
	BatchNo               string     `json:"batch_no"`
	BeneficiaryType       string     `json:"beneficiary_type"`
	BeneficiaryID         int64      `json:"beneficiary_id"`
	Currency              string     `json:"currency"`
	PeriodStartAt         time.Time  `json:"period_start_at"`
	PeriodEndAt           time.Time  `json:"period_end_at"`
	GrossAmountCents      int64      `json:"gross_amount_cents"`
	AdjustmentAmountCents int64      `json:"adjustment_amount_cents"`
	SettlementAmountCents int64      `json:"settlement_amount_cents"`
	Status                string     `json:"status"`
	CreatedByUserID       *int64     `json:"created_by_user_id,omitempty"`
	ApprovedByUserID      *int64     `json:"approved_by_user_id,omitempty"`
	ApprovedAt            *time.Time `json:"approved_at,omitempty"`
	PaidAt                *time.Time `json:"paid_at,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
	ItemCount             int        `json:"item_count"`
}

type CreateSettlementBatchInput struct {
	BeneficiaryType string    `json:"beneficiary_type"`
	BeneficiaryID   int64     `json:"beneficiary_id"`
	PeriodStartAt   time.Time `json:"period_start_at"`
	PeriodEndAt     time.Time `json:"period_end_at"`
}
