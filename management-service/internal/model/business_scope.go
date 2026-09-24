package model

type StaffBusinessScope struct {
	Mode        string  `json:"mode"`
	ActorUserID int64   `json:"actor_user_id,omitempty"`
	GroupIDs    []int64 `json:"group_ids,omitempty"`
	ManagerView bool    `json:"manager_view"`
}

type CustomerScopeSummary struct {
	TotalCount    int64 `json:"total_count"`
	ActiveCount   int64 `json:"active_count"`
	AgentCount    int64 `json:"agent_count"`
	ReferralCount int64 `json:"referral_count"`
}

type SalesPerformanceTotals struct {
	PaidOrderCount      int64 `json:"paid_order_count"`
	CustomerCount       int64 `json:"customer_count"`
	PaidAmountCents     int64 `json:"paid_amount_cents"`
	RefundedAmountCents int64 `json:"refunded_amount_cents"`
	NetRevenueCents     int64 `json:"net_revenue_cents"`
	EarningAmountCents  int64 `json:"earning_amount_cents"`
	PendingEarningCents int64 `json:"pending_earning_cents"`
	SettledEarningCents int64 `json:"settled_earning_cents"`
}
