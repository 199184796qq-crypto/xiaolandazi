package model

import "time"

type ResourceAccount struct {
	ID             int64     `json:"id"`
	OrganizationID int64     `json:"organization_id"`
	ResourceType   string    `json:"resource_type"`
	Unit           string    `json:"unit"`
	Balance        int64     `json:"balance"`
	Reserved       int64     `json:"reserved"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type ResourceLedgerEntry struct {
	ID                int64     `json:"id"`
	OrganizationID    int64     `json:"organization_id"`
	CounterpartyOrgID *int64    `json:"counterparty_org_id,omitempty"`
	ResourceType      string    `json:"resource_type"`
	ChangeQuantity    int64     `json:"change_quantity"`
	BalanceBefore     int64     `json:"balance_before"`
	BalanceAfter      int64     `json:"balance_after"`
	BusinessType      string    `json:"business_type"`
	OperatorUserID    *int64    `json:"operator_user_id,omitempty"`
	Reason            string    `json:"reason"`
	CreatedAt         time.Time `json:"created_at"`
}

type ResourceDashboard struct {
	OrganizationID int64                 `json:"organization_id"`
	Organization   string                `json:"organization"`
	OrgType        string                `json:"org_type"`
	Accounts       []ResourceAccount     `json:"accounts"`
	Ledger         []ResourceLedgerEntry `json:"ledger"`
}
