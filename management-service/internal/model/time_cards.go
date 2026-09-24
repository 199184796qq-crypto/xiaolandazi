package model

import "time"

type CommercialTimeCardVersion struct {
	ID                          int64      `json:"id"`
	ProductID                   int64      `json:"product_id"`
	VersionNo                   uint32     `json:"version_no"`
	LifecycleStatus             string     `json:"lifecycle_status"`
	Currency                    string     `json:"currency"`
	PriceCents                  uint64     `json:"price_cents"`
	DurationSeconds             uint64     `json:"duration_seconds"`
	ValidityDays                uint32     `json:"validity_days"`
	ActivationMode              string     `json:"activation_mode"`
	ActivationDeadlineDays      uint32     `json:"activation_deadline_days"`
	ParticipatesReferral        bool       `json:"participates_referral"`
	ParticipatesSalesCommission bool       `json:"participates_sales_commission"`
	ParticipatesAgentSettlement bool       `json:"participates_agent_settlement"`
	EffectiveFrom               *time.Time `json:"effective_from,omitempty"`
	EffectiveTo                 *time.Time `json:"effective_to,omitempty"`
	PublishedAt                 *time.Time `json:"published_at,omitempty"`
	CreatedAt                   time.Time  `json:"created_at"`
}

type CommercialTimeCardProduct struct {
	ID                 int64                      `json:"id"`
	Code               string                     `json:"code"`
	Name               string                     `json:"name"`
	Description        string                     `json:"description"`
	Status             string                     `json:"status"`
	SortOrder          int                        `json:"sort_order"`
	CreatedAt          time.Time                  `json:"created_at"`
	UpdatedAt          time.Time                  `json:"updated_at"`
	LatestVersion      *CommercialTimeCardVersion `json:"latest_version,omitempty"`
	ActiveVersion      *CommercialTimeCardVersion `json:"active_version,omitempty"`
	DraftVersion       *CommercialTimeCardVersion `json:"draft_version,omitempty"`
	MarketingCampaigns []MarketingCampaign        `json:"marketing_campaigns"`
}

type CommercialTimeCardInput struct {
	Code                        string `json:"code"`
	Name                        string `json:"name"`
	Description                 string `json:"description"`
	SortOrder                   int    `json:"sort_order"`
	PriceCents                  uint64 `json:"price_cents"`
	DurationSeconds             uint64 `json:"duration_seconds"`
	ValidityDays                uint32 `json:"validity_days"`
	ActivationMode              string `json:"activation_mode"`
	ActivationDeadlineDays      uint32 `json:"activation_deadline_days"`
	ParticipatesReferral        bool   `json:"participates_referral"`
	ParticipatesSalesCommission bool   `json:"participates_sales_commission"`
	ParticipatesAgentSettlement bool   `json:"participates_agent_settlement"`
}
