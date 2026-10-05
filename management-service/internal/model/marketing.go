package model

import "time"

type MarketingCampaignItem struct {
	FixedPriceCents *uint64 `json:"fixed_price_cents,omitempty"`
	ID              int64   `json:"id"`
	CampaignID      int64   `json:"campaign_id"`
	TargetType      string  `json:"target_type"`
	TargetID        int64   `json:"target_id"`
	PricingMode     string  `json:"pricing_mode"`
	PackageMonths   uint32  `json:"package_months"`
	DiscountBPS     uint32  `json:"discount_bps"`
	Quantity        uint32  `json:"quantity"`
	SortOrder       int     `json:"sort_order"`
}

// MarketingCampaign is a sell-side marketing plan. One plan can contain many
// product targets, and each target can have its own package period, quantity
// and discount. Product base definitions remain independent from marketing.
type MarketingCampaign struct {
	Controls         MarketingCampaignControls `json:"controls"`
	Eligible         *bool                     `json:"eligible,omitempty"`
	IneligibleReason string                    `json:"ineligible_reason,omitempty"`
	ID               int64                     `json:"id"`
	Code             string                    `json:"code"`
	Name             string                    `json:"name"`
	Description      string                    `json:"description"`
	Status           string                    `json:"status"`
	SortOrder        int                       `json:"sort_order"`
	PricingRule      string                    `json:"pricing_rule"`
	Items            []MarketingCampaignItem   `json:"items"`
	DisplayLocations []string                  `json:"display_locations"`

	// Legacy first-item fields are retained for older clients and historical
	// orders. New code should use Items.
	TargetType    string `json:"target_type,omitempty"`
	TargetID      int64  `json:"target_id,omitempty"`
	PricingMode   string `json:"pricing_mode,omitempty"`
	PackageMonths uint32 `json:"package_months,omitempty"`
	DiscountBPS   uint32 `json:"discount_bps,omitempty"`

	CreatedByUserID *int64    `json:"created_by_user_id,omitempty"`
	UpdatedByUserID *int64    `json:"updated_by_user_id,omitempty"`
	StartsAt        string    `json:"starts_at,omitempty"`
	EndsAt          string    `json:"ends_at,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type MarketingCampaignInput struct {
	Controls         MarketingCampaignControls `json:"controls"`
	Code             string                    `json:"code"`
	Name             string                    `json:"name"`
	Description      string                    `json:"description"`
	Status           string                    `json:"status"`
	SortOrder        int                       `json:"sort_order"`
	PricingRule      string                    `json:"pricing_rule"`
	StartsAt         string                    `json:"starts_at"`
	EndsAt           string                    `json:"ends_at"`
	Items            []MarketingCampaignItem   `json:"items"`
	DisplayLocations []string                  `json:"display_locations"`
}

// Limits count successful and reserved claims; cancellation releases only
// reservations, never an already consumed new-account benefit.
type MarketingCampaignControls struct {
	Audience       string `json:"audience"` // all, new_since_start, new_within_days
	NewAccountDays uint32 `json:"new_account_days"`
	MaxClaims      uint32 `json:"max_claims"`
	MaxUnits       uint32 `json:"max_units"`
	BenefitKey     string `json:"benefit_key"`
	RequirePhone   bool   `json:"require_phone"`
}
