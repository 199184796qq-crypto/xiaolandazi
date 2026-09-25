package model

import "time"

// Only constructed by authenticated server handlers, never decoded from a request.
type CustomerBusinessScope struct {
	UserID            int64
	Role              string
	TenantID          int64
	Finance           bool
	Operations        bool
	OperationsManager bool
}
type CustomerReceiptInput struct {
	TenantID           int64     `json:"tenant_id"`
	Channel            string    `json:"channel"`
	Purpose            string    `json:"purpose"`
	AmountCents        uint64    `json:"amount_cents"`
	OrderID            *int64    `json:"order_id,omitempty"`
	VerifiedPaymentID  *int64    `json:"verified_payment_id,omitempty"`
	ExistingRechargeID *int64    `json:"existing_recharge_id,omitempty"`
	PayerName          string    `json:"payer_name"`
	ReceivingAccount   string    `json:"receiving_account"`
	ExternalTradeNo    string    `json:"external_trade_no"`
	Evidence           string    `json:"evidence"`
	OccurredAt         time.Time `json:"occurred_at"`
	IdempotencyKey     string    `json:"idempotency_key"`
}
type CustomerReceipt struct {
	LastSubmitterUserID int64  `json:"last_submitter_user_id"`
	RequestHash         string `json:"-"`
	CustomerReceiptInput
	ID              int64      `json:"id"`
	ReceiptNo       string     `json:"receipt_no"`
	CustomerName    string     `json:"customer_name"`
	Status          string     `json:"status"`
	RequesterUserID int64      `json:"requester_user_id"`
	ReviewerUserID  *int64     `json:"reviewer_user_id,omitempty"`
	ReviewNote      string     `json:"review_note"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	PostedAt        *time.Time `json:"posted_at,omitempty"`
	RechargeID      *int64     `json:"recharge_id,omitempty"`
	PaymentID       *int64     `json:"payment_id,omitempty"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
}
type CustomerRecognition struct {
	TenantID              int64      `json:"tenant_id"`
	Qualified             bool       `json:"qualified"`
	CollectionStatus      string     `json:"collection_status"`
	ConfirmationReceiptID *int64     `json:"confirmation_receipt_id,omitempty"`
	ConfirmedAt           *time.Time `json:"confirmed_at,omitempty"`
	ConfirmedAmountCents  uint64     `json:"confirmed_amount_cents"`
	PendingCount          int64      `json:"pending_count"`
}
type CustomerMoneyEntry struct {
	EntryKey    string     `json:"entry_key"`
	Kind        string     `json:"kind"`
	ReferenceNo string     `json:"reference_no"`
	AmountCents int64      `json:"amount_cents"`
	Direction   string     `json:"direction"`
	Channel     string     `json:"channel"`
	Status      string     `json:"status"`
	OccurredAt  time.Time  `json:"occurred_at"`
	PostedAt    *time.Time `json:"posted_at,omitempty"`
	Note        string     `json:"note"`
}
type SupportTicketInput struct {
	TenantID       int64      `json:"tenant_id"`
	Category       string     `json:"category"`
	Title          string     `json:"title"`
	Description    string     `json:"description"`
	ContactName    string     `json:"contact_name"`
	ContactPhone   string     `json:"contact_phone"`
	PreferredAt    *time.Time `json:"preferred_at,omitempty"`
	IdempotencyKey string     `json:"idempotency_key"`
}
type SupportTicket struct {
	SupportTicketInput
	ID              int64      `json:"id"`
	TicketNo        string     `json:"ticket_no"`
	CustomerName    string     `json:"customer_name"`
	RequesterUserID int64      `json:"requester_user_id"`
	RequesterRole   string     `json:"requester_role"`
	AssignedUserID  *int64     `json:"assigned_user_id,omitempty"`
	AssignedName    string     `json:"assigned_name"`
	Status          string     `json:"status"`
	Resolution      string     `json:"resolution"`
	ConfirmedAt     *time.Time `json:"confirmed_at,omitempty"`
	Version         int        `json:"version"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
type CustomerBusinessEvent struct {
	ID        int64     `json:"id"`
	ActorName string    `json:"actor_name"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}
