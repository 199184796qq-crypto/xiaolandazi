package model

import "time"

// WechatPaymentPreparation is the durable local payment attempt created before
// calling WeChat Pay. PaymentNo is also used as WeChat's out_trade_no.
type WechatPaymentPreparation struct {
	PaymentID          int64      `json:"payment_id"`
	PaymentNo          string     `json:"payment_no"`
	OrderID            int64      `json:"order_id"`
	RechargeID         int64      `json:"recharge_id,omitempty"`
	OrderNo            string     `json:"order_no"`
	OrderType          string     `json:"order_type"`
	Description        string     `json:"description"`
	Currency           string     `json:"currency"`
	AmountCents        uint64     `json:"amount_cents"`
	PrepayID           string     `json:"-"`
	CodeURL            string     `json:"-"`
	ExpiresAt          time.Time  `json:"expires_at"`
	ProviderTradeState string     `json:"provider_trade_state"`
	PaidAt             *time.Time `json:"paid_at,omitempty"`
}

type WechatPaymentResult struct {
	AppID          string
	MchID          string
	OutTradeNo     string
	TransactionID  string
	TradeState     string
	TradeType      string
	Currency       string
	AmountCents    uint64
	SuccessTime    time.Time
	NotificationID string
	PayerOpenID    string
}

type WechatPaymentIdentity struct {
	AppID  string
	MchID  string
	OpenID string
}

type WechatOAuthState struct {
	UserID     int64
	ReturnPath string
	ExpiresAt  time.Time
}
