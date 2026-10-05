package model

import "time"

type CommerceRewardTier struct {
	MinimumPaidCents uint64 `json:"minimum_paid_cents"`
	RateBPS          uint32 `json:"rate_bps"`
}
type CommerceRewardRule struct {
	ID               int64                `json:"id"`
	HeadID           int64                `json:"head_id"`
	Channel          string               `json:"channel"`    // sales, referral
	ScopeType        string               `json:"scope_type"` // default, category, product, campaign
	ProductType      string               `json:"product_type"`
	TargetID         int64                `json:"target_id"`
	Name             string               `json:"name"`
	Mode             string               `json:"mode"` // off, fixed, percent, tier
	AmountCents      uint64               `json:"amount_cents"`
	RateBPS          uint32               `json:"rate_bps"`
	MinimumPaidCents uint64               `json:"minimum_paid_cents"`
	CapCents         uint64               `json:"cap_cents"`
	PendingDays      uint32               `json:"pending_days"`
	ScheduleMode     string               `json:"schedule_mode"` // immediate, monthly
	ReleaseDay       uint32               `json:"release_day"`
	MonthLag         uint32               `json:"month_lag"`
	WindowEndDay     uint32               `json:"window_end_day"` // 0: no window, release once and carry forward
	Tiers            []CommerceRewardTier `json:"tiers"`
	Status           string               `json:"status"`
	CreatedBy        int64                `json:"created_by"`
	PublishedBy      *int64               `json:"published_by,omitempty"`
	CreatedAt        time.Time            `json:"created_at"`
	PublishedAt      *time.Time           `json:"published_at,omitempty"`
}

type CommerceEarningDetail struct {
	ID            int64      `json:"id"`
	OrderNo       string     `json:"order_no"`
	ProductName   string     `json:"product_name"`
	RuleName      string     `json:"rule_name"`
	RuleVersionID int64      `json:"rule_version_id"`
	AmountCents   int64      `json:"amount_cents"`
	Status        string     `json:"status"`
	AvailableAt   *time.Time `json:"available_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}
