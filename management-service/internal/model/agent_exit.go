package model

import "time"

type AgentExitCheck struct {
	OrganizationID              int64    `json:"organization_id"`
	ActiveDeviceCount           int64    `json:"active_device_count"`
	OpenRMACount                int64    `json:"open_rma_count"`
	UnsettledEarningCount       int64    `json:"unsettled_earning_count"`
	UnsettledEarningAmountCents int64    `json:"unsettled_earning_amount_cents"`
	OpenSettlementBatchCount    int64    `json:"open_settlement_batch_count"`
	NonzeroResourceAccountCount int64    `json:"nonzero_resource_account_count"`
	ActiveCustomerCount         int64    `json:"active_customer_count"`
	CanExit                     bool     `json:"can_exit"`
	Blockers                    []string `json:"blockers"`
}

type AgentExitRecord struct {
	ID                          int64     `json:"id"`
	RecordKey                   string    `json:"record_key"`
	OrganizationID              int64     `json:"organization_id"`
	AgentCode                   string    `json:"agent_code"`
	AgentName                   string    `json:"agent_name"`
	Status                      string    `json:"status"`
	TransferredCustomerCount    int64     `json:"transferred_customer_count"`
	ActiveDeviceCount           int64     `json:"active_device_count"`
	OpenRMACount                int64     `json:"open_rma_count"`
	UnsettledEarningCount       int64     `json:"unsettled_earning_count"`
	UnsettledEarningAmountCents int64     `json:"unsettled_earning_amount_cents"`
	OpenSettlementBatchCount    int64     `json:"open_settlement_batch_count"`
	NonzeroResourceAccountCount int64     `json:"nonzero_resource_account_count"`
	FinalizedByUserID           *int64    `json:"finalized_by_user_id,omitempty"`
	FinalizedByName             string    `json:"finalized_by_name"`
	FinalizedAt                 time.Time `json:"finalized_at"`
	CreatedAt                   time.Time `json:"created_at"`
}
