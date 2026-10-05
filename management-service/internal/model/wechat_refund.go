package model

import "time"

// Cash refunds are separate from manual reward/commission withdrawals.
type WechatCashRefund struct {
	TenantID      int64                  `json:"tenant_id,omitempty"`
	TenantName    string                 `json:"tenant_name,omitempty"`
	ID            int64                  `json:"id"`
	RefundNo      string                 `json:"refund_no"`
	AmountCents   uint64                 `json:"amount_cents"`
	RefundedCents uint64                 `json:"refunded_cents"`
	ReleasedCents uint64                 `json:"released_cents"`
	FrozenCents   uint64                 `json:"frozen_cents"`
	Status        string                 `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	Items         []WechatCashRefundItem `json:"items"`
}

type WechatCashRefundItem struct {
	ID              int64      `json:"id"`
	RequestID       int64      `json:"-"`
	TenantID        int64      `json:"-"`
	WalletID        int64      `json:"-"`
	RefundNo        string     `json:"refund_no"`
	RechargeID      int64      `json:"-"`
	RechargeNo      string     `json:"recharge_no"`
	PaymentNo       string     `json:"-"`
	TransactionID   string     `json:"-"`
	AppID           string     `json:"-"`
	MchID           string     `json:"-"`
	PayerHash       string     `json:"-"`
	TotalCents      uint64     `json:"-"`
	AmountCents     uint64     `json:"amount_cents"`
	Status          string     `json:"status"`
	ReceivedAccount string     `json:"received_account"`
	Message         string     `json:"message"`
	SuccessTime     *time.Time `json:"success_time,omitempty"`
	Attempts        int        `json:"-"`
	LeaseToken      string     `json:"-"`
}

type WechatRefundWallet struct {
	AvailableCents  uint64             `json:"available_cents"`
	FrozenCents     uint64             `json:"frozen_cents"`
	RefundableCents uint64             `json:"refundable_cents"`
	Records         []WechatCashRefund `json:"records"`
}

// Populated only from the server's authenticated provider query, never JSON.
type WechatRefundPaymentVerification struct {
	PaymentNo, TransactionID, AppID, MchID, OpenID string
	TotalCents, PayerCents                         uint64
	PayerAmountKnown                               bool
	SuccessTime                                    time.Time
}

type WechatRefundResult struct {
	RefundNo, ProviderRefundID, PaymentNo, TransactionID, MchID string
	Currency, Status, ReceivedAccount                           string
	TotalCents, RefundCents, PayerTotalCents, PayerRefundCents  uint64
	SuccessTime                                                 time.Time
}
