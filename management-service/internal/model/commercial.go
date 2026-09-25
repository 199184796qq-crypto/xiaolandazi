package model

import "time"

type AccountProfile struct {
	UserID      int64     `json:"user_id"`
	TenantID    *int64    `json:"tenant_id,omitempty"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url"`
	Role        string    `json:"role"`
	Phone       string    `json:"phone"`
	Email       string    `json:"email"`
	QQ          string    `json:"qq"`
	Wechat      string    `json:"wechat"`
	Province    string    `json:"province"`
	City        string    `json:"city"`
	District    string    `json:"district"`
	Address     string    `json:"address"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}
type MembershipSummary struct {
	ID              int64     `json:"id"`
	PlanID          int64     `json:"plan_id"`
	PlanVersionID   int64     `json:"plan_version_id"`
	PlanName        string    `json:"plan_name"`
	Status          string    `json:"status"`
	CycleStartAt    time.Time `json:"cycle_start_at"`
	CycleEndAt      time.Time `json:"cycle_end_at"`
	IncludedSeconds uint64    `json:"included_seconds"`
	AutoRenew       bool      `json:"auto_renew"`
}

type QuotaSummary struct {
	MembershipSeconds uint64 `json:"membership_seconds"`
	PurchasedSeconds  uint64 `json:"purchased_seconds"`
	RewardSeconds     uint64 `json:"reward_seconds"`
	TotalSeconds      uint64 `json:"total_seconds"`
}

type AccountDashboard struct {
	Profile    AccountProfile     `json:"profile"`
	Membership *MembershipSummary `json:"membership,omitempty"`
	Quota      QuotaSummary       `json:"quota"`
}

type WalletLedgerRecord struct {
	ID                 int64     `json:"id"`
	Direction          string    `json:"direction"`
	AmountCents        uint64    `json:"amount_cents"`
	BalanceBeforeCents int64     `json:"balance_before_cents"`
	BalanceAfterCents  int64     `json:"balance_after_cents"`
	BusinessType       string    `json:"business_type"`
	OrderNo            string    `json:"order_no,omitempty"`
	Reason             string    `json:"reason,omitempty"`
	OccurredAt         time.Time `json:"occurred_at"`
}

type RechargeRecord struct {
	ID                   int64      `json:"id"`
	RechargeNo           string     `json:"recharge_no"`
	RequestedAmountCents uint64     `json:"requested_amount_cents"`
	CreditedAmountCents  uint64     `json:"credited_amount_cents"`
	PaymentMethod        string     `json:"payment_method"`
	Status               string     `json:"status"`
	PaidAt               *time.Time `json:"paid_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
}

type PurchaseRecord struct {
	ID                  int64      `json:"id"`
	OrderNo             string     `json:"order_no"`
	OrderType           string     `json:"order_type"`
	Status              string     `json:"status"`
	ListAmountCents     uint64     `json:"list_amount_cents"`
	DiscountAmountCents uint64     `json:"discount_amount_cents"`
	PaidAmountCents     uint64     `json:"paid_amount_cents"`
	RefundedAmountCents uint64     `json:"refunded_amount_cents"`
	PaidAt              *time.Time `json:"paid_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
}

type RefundRecord struct {
	ID                int64      `json:"id"`
	RefundNo          string     `json:"refund_no"`
	SourceType        string     `json:"source_type"`
	SourceID          int64      `json:"source_id"`
	RefundAmountCents uint64     `json:"refund_amount_cents"`
	RefundMethod      string     `json:"refund_method"`
	Status            string     `json:"status"`
	Reason            string     `json:"reason"`
	ProcessedAt       *time.Time `json:"processed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type FinanceDashboard struct {
	CashBalanceCents       int64                  `json:"cash_balance_cents"`
	RewardBalanceCents     int64                  `json:"reward_balance_cents"`
	CommissionBalanceCents int64                  `json:"commission_balance_cents"`
	CommissionFrozenCents  int64                  `json:"commission_frozen_cents"`
	TotalBalanceCents      int64                  `json:"total_balance_cents"`
	MonthSpentCents        uint64                 `json:"month_spent_cents"`
	AvailableSeconds       uint64                 `json:"available_seconds"`
	MembershipName         string                 `json:"membership_name,omitempty"`
	Ledger                 []WalletLedgerRecord   `json:"ledger"`
	Recharges              []RechargeRecord       `json:"recharges"`
	Purchases              []PurchaseRecord       `json:"purchases"`
	Refunds                []RefundRecord         `json:"refunds"`
	Payments               []SandboxPaymentRecord `json:"payments"`
}
