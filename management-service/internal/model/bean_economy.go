package model

import (
	"encoding/json"
	"time"
)

type BeanWallet struct {
	ID             int64     `json:"id"`
	OwnerType      string    `json:"owner_type"`
	OwnerID        int64     `json:"owner_id"`
	AvailableBeans uint64    `json:"available_beans"`
	FrozenBeans    uint64    `json:"frozen_beans"`
	Status         string    `json:"status"`
	Version        uint64    `json:"version"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type BeanLedgerEntry struct {
	ID              int64     `json:"id"`
	ExternalID      string    `json:"external_id"`
	WalletID        int64     `json:"wallet_id"`
	AvailableDelta  int64     `json:"available_delta"`
	FrozenDelta     int64     `json:"frozen_delta"`
	AvailableBefore uint64    `json:"available_before"`
	AvailableAfter  uint64    `json:"available_after"`
	FrozenBefore    uint64    `json:"frozen_before"`
	FrozenAfter     uint64    `json:"frozen_after"`
	BusinessType    string    `json:"business_type"`
	ReferenceType   string    `json:"reference_type"`
	ReferenceID     *int64    `json:"reference_id,omitempty"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
}

type BeanCommerceSettings struct {
	PurchaseBeansPerYuan        uint64    `json:"purchase_beans_per_yuan"`
	MinimumPurchaseCents        uint64    `json:"minimum_purchase_cents"`
	StaffCashFenPer100Beans     uint64    `json:"staff_cash_fen_per_100_beans"`
	MinimumStaffConversionBeans uint64    `json:"minimum_staff_conversion_beans"`
	Enabled                     bool      `json:"enabled"`
	Version                     uint64    `json:"version"`
	UpdatedByUserID             int64     `json:"updated_by_user_id"`
	UpdatedAt                   time.Time `json:"updated_at"`
}

type BeanCommerceSettingsInput struct {
	PurchaseBeansPerYuan        uint64 `json:"purchase_beans_per_yuan"`
	MinimumPurchaseCents        uint64 `json:"minimum_purchase_cents"`
	StaffCashFenPer100Beans     uint64 `json:"staff_cash_fen_per_100_beans"`
	MinimumStaffConversionBeans uint64 `json:"minimum_staff_conversion_beans"`
	Enabled                     bool   `json:"enabled"`
	Version                     uint64 `json:"version"`
}

type BeanPricingRule struct {
	ID                 int64     `json:"id"`
	ActionCode         string    `json:"action_code"`
	ActionName         string    `json:"action_name"`
	Category           string    `json:"category"`
	Description        string    `json:"description"`
	ChargeMode         string    `json:"charge_mode"`
	BeansPerUnit       uint64    `json:"beans_per_unit"`
	UnitSize           uint64    `json:"unit_size"`
	MinimumChargeBeans uint64    `json:"minimum_charge_beans"`
	MaximumChargeBeans uint64    `json:"maximum_charge_beans"`
	StaffRewardBPS     uint32    `json:"staff_reward_bps"`
	Enabled            bool      `json:"enabled"`
	Version            uint64    `json:"version"`
	UpdatedByUserID    int64     `json:"updated_by_user_id"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type BeanPricingRuleInput struct {
	ActionCode         string `json:"action_code"`
	ActionName         string `json:"action_name"`
	Category           string `json:"category"`
	Description        string `json:"description"`
	ChargeMode         string `json:"charge_mode"`
	BeansPerUnit       uint64 `json:"beans_per_unit"`
	UnitSize           uint64 `json:"unit_size"`
	MinimumChargeBeans uint64 `json:"minimum_charge_beans"`
	MaximumChargeBeans uint64 `json:"maximum_charge_beans"`
	StaffRewardBPS     uint32 `json:"staff_reward_bps"`
	Enabled            bool   `json:"enabled"`
	Version            uint64 `json:"version"`
}

type BeanPurchaseOrder struct {
	ID                   int64     `json:"id"`
	PurchaseNo           string    `json:"purchase_no"`
	TenantID             int64     `json:"tenant_id"`
	CashAmountCents      uint64    `json:"cash_amount_cents"`
	BeansPerYuanSnapshot uint64    `json:"beans_per_yuan_snapshot"`
	CreditedBeans        uint64    `json:"credited_beans"`
	Status               string    `json:"status"`
	OperatorUserID       int64     `json:"operator_user_id"`
	CreatedAt            time.Time `json:"created_at"`
}

type BeanChargeQuote struct {
	ActionCode     string `json:"action_code"`
	ActionName     string `json:"action_name"`
	Units          uint64 `json:"units"`
	QuotedBeans    uint64 `json:"quoted_beans"`
	RuleID         int64  `json:"rule_id"`
	RuleVersion    uint64 `json:"rule_version"`
	PricingEnabled bool   `json:"pricing_enabled"`
}

type BeanCharge struct {
	ID                int64           `json:"id"`
	ExternalID        string          `json:"external_id"`
	ActionCode        string          `json:"action_code"`
	PayerOwnerType    string          `json:"payer_owner_type"`
	PayerOwnerID      int64           `json:"payer_owner_id"`
	BeneficiaryUserID *int64          `json:"beneficiary_user_id,omitempty"`
	RequestedUnits    uint64          `json:"requested_units"`
	ActualUnits       uint64          `json:"actual_units"`
	QuotedBeans       uint64          `json:"quoted_beans"`
	ReservedBeans     uint64          `json:"reserved_beans"`
	ChargedBeans      uint64          `json:"charged_beans"`
	StaffRewardBeans  uint64          `json:"staff_reward_beans"`
	PlatformBeans     uint64          `json:"platform_beans"`
	Status            string          `json:"status"`
	ReferenceType     string          `json:"reference_type"`
	ReferenceID       *int64          `json:"reference_id,omitempty"`
	Metadata          json.RawMessage `json:"metadata,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	SettledAt         *time.Time      `json:"settled_at,omitempty"`
}

type BeanConversionRequest struct {
	ID                         int64      `json:"id"`
	ConversionNo               string     `json:"conversion_no"`
	UserID                     int64      `json:"user_id"`
	UserName                   string     `json:"user_name"`
	BeanAmount                 uint64     `json:"bean_amount"`
	CashAmountCents            uint64     `json:"cash_amount_cents"`
	CashFenPer100BeansSnapshot uint64     `json:"cash_fen_per_100_beans_snapshot"`
	Status                     string     `json:"status"`
	RejectReason               string     `json:"reject_reason"`
	RequestedAt                time.Time  `json:"requested_at"`
	ApprovedAt                 *time.Time `json:"approved_at,omitempty"`
	PaidAt                     *time.Time `json:"paid_at,omitempty"`
}

type BeanWalletDashboard struct {
	Wallet      BeanWallet              `json:"wallet"`
	Ledger      []BeanLedgerEntry       `json:"ledger"`
	Purchases   []BeanPurchaseOrder     `json:"purchases,omitempty"`
	Conversions []BeanConversionRequest `json:"conversions,omitempty"`
	Settings    BeanCommerceSettings    `json:"settings"`
}

type BeanCommercialSummary struct {
	CustomerAvailableBeans uint64 `json:"customer_available_beans"`
	CustomerFrozenBeans    uint64 `json:"customer_frozen_beans"`
	StaffAvailableBeans    uint64 `json:"staff_available_beans"`
	StaffFrozenBeans       uint64 `json:"staff_frozen_beans"`
	PurchasedBeans         uint64 `json:"purchased_beans"`
	PurchaseCashCents      uint64 `json:"purchase_cash_cents"`
	ChargedBeans           uint64 `json:"charged_beans"`
	PendingConversionCents uint64 `json:"pending_conversion_cents"`
}

type BeanCommercialDashboard struct {
	Settings BeanCommerceSettings  `json:"settings"`
	Rules    []BeanPricingRule     `json:"rules"`
	Summary  BeanCommercialSummary `json:"summary"`
}

type BeanFinanceDashboard struct {
	Summary      BeanCommercialSummary   `json:"summary"`
	Conversions  []BeanConversionRequest `json:"conversions"`
	RecentLedger []BeanLedgerEntry       `json:"recent_ledger"`
}
