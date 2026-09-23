package model

type SalesPerformanceSummary struct {
	SalesStaffID        int64  `json:"sales_staff_id"`
	UserID              int64  `json:"user_id"`
	EmployeeCode        string `json:"employee_code"`
	DisplayName         string `json:"display_name"`
	TeamName            string `json:"team_name"`
	PaidOrderCount      int64  `json:"paid_order_count"`
	CustomerCount       int64  `json:"customer_count"`
	PaidAmountCents     int64  `json:"paid_amount_cents"`
	RefundedAmountCents int64  `json:"refunded_amount_cents"`
	NetRevenueCents     int64  `json:"net_revenue_cents"`
	EarningAmountCents  int64  `json:"earning_amount_cents"`
	PendingEarningCents int64  `json:"pending_earning_cents"`
	SettledEarningCents int64  `json:"settled_earning_cents"`
}
