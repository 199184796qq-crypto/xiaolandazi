package model

import "time"

type CommercialMembershipVersion struct {
	ID                         int64      `json:"id"`
	PlanID                     int64      `json:"plan_id"`
	VersionNo                  uint32     `json:"version_no"`
	LifecycleStatus            string     `json:"lifecycle_status"`
	Currency                   string     `json:"currency"`
	PriceCents                 uint64     `json:"price_cents"`
	BillingPeriodUnit          string     `json:"billing_period_unit"`
	BillingPeriodCount         uint32     `json:"billing_period_count"`
	IncludedSeconds            uint64     `json:"included_seconds"`
	DefaultTimeCardDiscountBPS uint32     `json:"default_time_card_discount_bps"`
	AllowAutoRenew             bool       `json:"allow_auto_renew"`
	EffectiveFrom              *time.Time `json:"effective_from,omitempty"`
	EffectiveTo                *time.Time `json:"effective_to,omitempty"`
	PublishedAt                *time.Time `json:"published_at,omitempty"`
	CreatedAt                  time.Time  `json:"created_at"`
}

type CommercialMembershipPlan struct {
	ID            int64                        `json:"id"`
	Code          string                       `json:"code"`
	Name          string                       `json:"name"`
	Description   string                       `json:"description"`
	Status        string                       `json:"status"`
	SortOrder     int                          `json:"sort_order"`
	CreatedAt     time.Time                    `json:"created_at"`
	UpdatedAt     time.Time                    `json:"updated_at"`
	LatestVersion *CommercialMembershipVersion `json:"latest_version,omitempty"`
	ActiveVersion *CommercialMembershipVersion `json:"active_version,omitempty"`
	DraftVersion  *CommercialMembershipVersion `json:"draft_version,omitempty"`
}

type CommercialMembershipInput struct {
	Code                       string `json:"code"`
	Name                       string `json:"name"`
	Description                string `json:"description"`
	SortOrder                  int    `json:"sort_order"`
	PriceCents                 uint64 `json:"price_cents"`
	IncludedSeconds            uint64 `json:"included_seconds"`
	DefaultTimeCardDiscountBPS uint32 `json:"default_time_card_discount_bps"`
	AllowAutoRenew             bool   `json:"allow_auto_renew"`
}
