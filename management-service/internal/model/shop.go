package model

import "time"

type CustomerShopOrderItem struct {
	ID                 int64     `json:"id"`
	OrderID            int64     `json:"order_id"`
	ProductType        string    `json:"product_type"`
	ProductID          int64     `json:"product_id"`
	ProductVersionID   int64     `json:"product_version_id"`
	ProductName        string    `json:"product_name"`
	Quantity           uint32    `json:"quantity"`
	DurationSeconds    uint64    `json:"duration_seconds"`
	ValidityDays       uint32    `json:"validity_days"`
	UnitListPriceCents uint64    `json:"unit_list_price_cents"`
	UnitPaidPriceCents uint64    `json:"unit_paid_price_cents"`
	DiscountBPS        uint32    `json:"discount_bps"`
	CreatedAt          time.Time `json:"created_at"`
}

type SandboxPaymentRecord struct {
	ID                  int64      `json:"id"`
	PaymentNo           string     `json:"payment_no"`
	TenantID            int64      `json:"tenant_id"`
	OrderID             int64      `json:"order_id"`
	OrderNo             string     `json:"order_no"`
	Channel             string     `json:"channel"`
	PaymentMethod       string     `json:"payment_method"`
	Currency            string     `json:"currency"`
	ExpectedAmountCents uint64     `json:"expected_amount_cents"`
	InputAmountCents    uint64     `json:"input_amount_cents"`
	PaidAmountCents     uint64     `json:"paid_amount_cents"`
	Status              string     `json:"status"`
	FailureReason       string     `json:"failure_reason"`
	ExternalTradeNo     string     `json:"external_trade_no"`
	OperatorUserID      *int64     `json:"operator_user_id,omitempty"`
	IdempotencyKey      string     `json:"idempotency_key"`
	PaidAt              *time.Time `json:"paid_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type CustomerShopOrder struct {
	ID                    int64                   `json:"id"`
	OrderNo               string                  `json:"order_no"`
	TenantID              int64                   `json:"tenant_id"`
	OrderType             string                  `json:"order_type"`
	Status                string                  `json:"status"`
	PaymentStatus         string                  `json:"payment_status"`
	FulfillmentStatus     string                  `json:"fulfillment_status"`
	Currency              string                  `json:"currency"`
	ListAmountCents       uint64                  `json:"list_amount_cents"`
	DiscountAmountCents   uint64                  `json:"discount_amount_cents"`
	PayableAmountCents    uint64                  `json:"payable_amount_cents"`
	PaidAmountCents       uint64                  `json:"paid_amount_cents"`
	RefundedAmountCents   uint64                  `json:"refunded_amount_cents"`
	RefundableAmountCents uint64                  `json:"refundable_amount_cents"`
	RefundableSeconds     uint64                  `json:"refundable_seconds"`
	PaidAt                *time.Time              `json:"paid_at,omitempty"`
	CancelledAt           *time.Time              `json:"cancelled_at,omitempty"`
	CreatedAt             time.Time               `json:"created_at"`
	UpdatedAt             time.Time               `json:"updated_at"`
	Items                 []CustomerShopOrderItem `json:"items"`
	Payments              []SandboxPaymentRecord  `json:"payments"`
	Shipping              *CustomerOrderShipping  `json:"shipping,omitempty"`
	Devices               []CustomerOrderDevice   `json:"devices"`
	Shipments             []Shipment              `json:"shipments"`
}

type CreateCustomerShopOrderInput struct {
	ProductType         string `json:"product_type"`
	ProductID           int64  `json:"product_id"`
	Quantity            uint32 `json:"quantity"`
	MembershipCycle     string `json:"membership_cycle"`
	MarketingCampaignID int64  `json:"marketing_campaign_id"`
	MarketingPlacement  string `json:"marketing_placement"`
	IdempotencyKey      string `json:"idempotency_key"`
	RecipientName       string `json:"recipient_name"`
	RecipientPhone      string `json:"recipient_phone"`
	Province            string `json:"province"`
	City                string `json:"city"`
	District            string `json:"district"`
	Address             string `json:"address"`
}

type SandboxPayOrderInput struct {
	AmountCents    uint64 `json:"amount_cents"`
	SimulateResult string `json:"simulate_result"`
	IdempotencyKey string `json:"idempotency_key"`
}

type SandboxRefundOrderInput struct {
	AmountCents    uint64 `json:"amount_cents"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
}
