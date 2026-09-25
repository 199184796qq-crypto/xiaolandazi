package model

import "time"

// SalesCustomerListItem contains only the contact fields needed by the assigned seller.
// It deliberately excludes credentials, wallets, administrative permissions and other sellers.
type SalesCustomerListItem struct {
	Qualified        bool       `json:"qualified"`
	CollectionStatus string     `json:"collection_status"`
	ConfirmedAt      *time.Time `json:"confirmed_at,omitempty"`
	UserID           int64      `json:"user_id"`
	TenantID         int64      `json:"tenant_id"`
	Username         string     `json:"username"`
	DisplayName      string     `json:"display_name"`
	Phone            string     `json:"phone"`
	Email            string     `json:"email"`
	Province         string     `json:"province"`
	City             string     `json:"city"`
	District         string     `json:"district"`
	Address          string     `json:"address"`
	Status           string     `json:"status"`
	SourceType       string     `json:"source_type"`
	CreatedAt        time.Time  `json:"created_at"`
}

type SalesCustomerListSummary struct {
	QualifiedCount      int64 `json:"qualified_count"`
	UnconfirmedCount    int64 `json:"unconfirmed_count"`
	PendingReceiptCount int64 `json:"pending_receipt_count"`
	TotalCount          int64 `json:"total_count"`
	ActiveCount         int64 `json:"active_count"`
	DisabledCount       int64 `json:"disabled_count"`
}
