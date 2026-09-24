package model

import "time"

type StaffFinanceCustomerSummary struct {
	TenantID           int64  `json:"tenant_id"`
	UserID             int64  `json:"user_id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	Phone              string `json:"phone"`
	ParentOrgName      string `json:"parent_org_name"`
	ParentOrgType      string `json:"parent_org_type"`
	CashBalanceCents   int64  `json:"cash_balance_cents"`
	RewardBalanceCents int64  `json:"reward_balance_cents"`
}

type StaffFinanceTaskSummary struct {
	ID               int64      `json:"id"`
	PolicyID         *int64     `json:"policy_id,omitempty"`
	OperationCode    string     `json:"operation_code"`
	RequesterUserID  int64      `json:"requester_user_id"`
	RequesterName    string     `json:"requester_name"`
	ApproverUserID   *int64     `json:"approver_user_id,omitempty"`
	ApproverName     string     `json:"approver_name,omitempty"`
	TenantID         int64      `json:"tenant_id"`
	CustomerName     string     `json:"customer_name"`
	TargetType       string     `json:"target_type"`
	AmountYuan       float64    `json:"amount_yuan"`
	ResourceSeconds  int64      `json:"resource_seconds,omitempty"`
	Status           string     `json:"status"`
	Reason           string     `json:"reason"`
	ApproverRoleCode string     `json:"approver_role_code"`
	CreatedAt        time.Time  `json:"created_at"`
	DecidedAt        *time.Time `json:"decided_at,omitempty"`
}

type StaffFinanceOperationResult struct {
	Task             StaffFinanceTaskSummary `json:"task"`
	RequiresApproval bool                    `json:"requires_approval"`
	Applied          bool                    `json:"applied"`
}
