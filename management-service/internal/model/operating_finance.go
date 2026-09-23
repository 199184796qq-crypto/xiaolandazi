package model

import "time"

type OperatingFinanceEntry struct {
	ID               int64     `json:"id"`
	EntryNo          string    `json:"entry_no"`
	Direction        string    `json:"direction"`
	Category         string    `json:"category"`
	AmountCents      uint64    `json:"amount_cents"`
	Currency         string    `json:"currency"`
	BusinessType     string    `json:"business_type"`
	BusinessID       *int64    `json:"business_id,omitempty"`
	BusinessNo       string    `json:"business_no"`
	CounterpartyName string    `json:"counterparty_name"`
	PaymentMethod    string    `json:"payment_method"`
	Description      string    `json:"description"`
	OperatorUserID   *int64    `json:"operator_user_id,omitempty"`
	OperatorName     string    `json:"operator_name"`
	OccurredAt       time.Time `json:"occurred_at"`
	CreatedAt        time.Time `json:"created_at"`
}

type TokenPurchase struct {
	ID            int64     `json:"id"`
	PurchaseNo    string    `json:"purchase_no"`
	ProviderName  string    `json:"provider_name"`
	ModelScope    string    `json:"model_scope"`
	TokenQuantity uint64    `json:"token_quantity"`
	AmountCents   uint64    `json:"amount_cents"`
	PaymentMethod string    `json:"payment_method"`
	InvoiceNo     string    `json:"invoice_no"`
	PurchasedAt   time.Time `json:"purchased_at"`
	Note          string    `json:"note"`
	OperatorUserID *int64   `json:"operator_user_id,omitempty"`
	OperatorName  string    `json:"operator_name"`
	CreatedAt     time.Time `json:"created_at"`
}

type TokenPurchaseInput struct {
	ProviderName  string     `json:"provider_name"`
	ModelScope    string     `json:"model_scope"`
	TokenQuantity uint64     `json:"token_quantity"`
	AmountCents   uint64     `json:"amount_cents"`
	PaymentMethod string     `json:"payment_method"`
	InvoiceNo     string     `json:"invoice_no"`
	PurchasedAt   *time.Time `json:"purchased_at,omitempty"`
	Note          string     `json:"note"`
}

type OperatingFinanceOverview struct {
	TotalIncomeCents   uint64                  `json:"total_income_cents"`
	TotalExpenseCents  uint64                  `json:"total_expense_cents"`
	MonthIncomeCents   uint64                  `json:"month_income_cents"`
	MonthExpenseCents  uint64                  `json:"month_expense_cents"`
	Entries            []OperatingFinanceEntry `json:"entries"`
	TokenPurchases     []TokenPurchase         `json:"token_purchases"`
}
