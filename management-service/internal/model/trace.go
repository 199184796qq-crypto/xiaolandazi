package model

import "time"

type OrderTraceRelation struct {
	SalesStaffID       *int64 `json:"sales_staff_id,omitempty"`
	SalesStaffName     string `json:"sales_staff_name"`
	AgentOrgID         *int64 `json:"agent_org_id,omitempty"`
	AgentOrgName       string `json:"agent_org_name"`
	ReferrerTenantID   *int64 `json:"referrer_tenant_id,omitempty"`
	ReferrerTenantName string `json:"referrer_tenant_name"`
}

type QuotaTraceEntry struct {
	ID                     int64     `json:"id"`
	ExternalID             string    `json:"external_id"`
	BucketID               int64     `json:"bucket_id"`
	ChangeSeconds          int64     `json:"change_seconds"`
	RemainingBeforeSeconds uint64    `json:"remaining_before_seconds"`
	RemainingAfterSeconds  uint64    `json:"remaining_after_seconds"`
	BusinessType           string    `json:"business_type"`
	BusinessID             *int64    `json:"business_id,omitempty"`
	Reason                 string    `json:"reason"`
	OccurredAt             time.Time `json:"occurred_at"`
}

type ResourceTraceEntry struct {
	ID             int64     `json:"id"`
	ResourceType   string    `json:"resource_type"`
	ChangeQuantity int64     `json:"change_quantity"`
	BalanceBefore  int64     `json:"balance_before"`
	BalanceAfter   int64     `json:"balance_after"`
	BusinessType   string    `json:"business_type"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
}

type SettlementTraceEntry struct {
	EarningID       int64      `json:"earning_id"`
	BatchID         int64      `json:"batch_id"`
	BatchNo         string     `json:"batch_no"`
	BeneficiaryType string     `json:"beneficiary_type"`
	BeneficiaryID   int64      `json:"beneficiary_id"`
	AmountCents     int64      `json:"amount_cents"`
	BatchStatus     string     `json:"batch_status"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
	PaidAt          *time.Time `json:"paid_at,omitempty"`
}

type ApprovalTraceEntry struct {
	ID              int64      `json:"id"`
	OperationCode   string     `json:"operation_code"`
	RequesterUserID int64      `json:"requester_user_id"`
	RequesterName   string     `json:"requester_name"`
	ApproverUserID  *int64     `json:"approver_user_id,omitempty"`
	ApproverName    string     `json:"approver_name"`
	AmountYuan      float64    `json:"amount_yuan"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
}

type BusinessAuditTraceEntry struct {
	ID          int64     `json:"id"`
	ActorUserID *int64    `json:"actor_user_id,omitempty"`
	Action      string    `json:"action"`
	EntityType  string    `json:"entity_type"`
	EntityID    string    `json:"entity_id"`
	Reason      string    `json:"reason"`
	CreatedAt   time.Time `json:"created_at"`
}

type OrderTraceBundle struct {
	Order          CustomerShopOrder        `json:"order"`
	Relation       OrderTraceRelation        `json:"relation"`
	Refunds        []RefundRecord            `json:"refunds"`
	QuotaLedger    []QuotaTraceEntry         `json:"quota_ledger"`
	ResourceLedger []ResourceTraceEntry      `json:"resource_ledger"`
	DeviceLedger   []DeviceLedgerEntry       `json:"device_ledger"`
	RMAs           []RMARecord               `json:"rmas"`
	Incentives     []IncentiveEarning        `json:"incentives"`
	Settlements    []SettlementTraceEntry    `json:"settlements"`
	Approvals      []ApprovalTraceEntry      `json:"approvals"`
	AuditEvents    []BusinessAuditTraceEntry `json:"audit_events"`
}
