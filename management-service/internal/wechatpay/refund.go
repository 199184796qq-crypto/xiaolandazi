package wechatpay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
	"livecompanion/management/internal/model"
)

func (s *Service) RefundNotifyURL() string {
	u, err := url.Parse(s.cfg.NotifyURL)
	if err != nil {
		return ""
	}
	u.Path = "/api/v1/payments/wechat/refund-notify"
	u.RawPath = ""
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func (s *Service) CreateRefund(ctx context.Context, i model.WechatCashRefundItem) (model.WechatRefundResult, error) {
	if !s.Enabled() || i.AppID != s.cfg.AppID || i.MchID != s.cfg.MchID || i.RefundNo == "" || i.TransactionID == "" || i.AmountCents == 0 || i.AmountCents > i.TotalCents || i.TotalCents > 2147483647 {
		return model.WechatRefundResult{}, errors.New("invalid wechat refund request")
	}
	api := refunddomestic.RefundsApiService{Client: s.jsapi.Client}
	r, _, err := api.Create(ctx, refunddomestic.CreateRequest{
		TransactionId: core.String(i.TransactionID), OutRefundNo: core.String(i.RefundNo), Reason: core.String("未消费充值本金原路退回"), NotifyUrl: core.String(s.RefundNotifyURL()),
		Amount: &refunddomestic.AmountReq{Refund: core.Int64(int64(i.AmountCents)), Total: core.Int64(int64(i.TotalCents)), Currency: core.String("CNY")},
	})
	if err != nil {
		return model.WechatRefundResult{}, err
	}
	return normalizeRefund(r)
}

func (s *Service) QueryRefund(ctx context.Context, no string) (model.WechatRefundResult, error) {
	if !s.Enabled() || no == "" {
		return model.WechatRefundResult{}, errors.New("wechat refund is disabled")
	}
	api := refunddomestic.RefundsApiService{Client: s.jsapi.Client}
	r, _, err := api.QueryByOutRefundNo(ctx, refunddomestic.QueryByOutRefundNoRequest{OutRefundNo: core.String(no)})
	if err != nil {
		return model.WechatRefundResult{}, err
	}
	return normalizeRefund(r)
}

func IsRefundNotExist(err error) bool { return core.IsAPIError(err, "RESOURCE_NOT_EXISTS") }

func normalizeRefund(r *refunddomestic.Refund) (model.WechatRefundResult, error) {
	if r == nil || r.RefundId == nil || r.OutRefundNo == nil || r.OutTradeNo == nil || r.TransactionId == nil || r.Status == nil || r.Amount == nil || r.Amount.Currency == nil || r.Amount.Total == nil || r.Amount.Refund == nil || r.Amount.PayerTotal == nil || r.Amount.PayerRefund == nil {
		return model.WechatRefundResult{}, errors.New("incomplete wechat refund")
	}
	a := r.Amount
	if *a.Total <= 0 || *a.Refund <= 0 || *a.Refund > *a.Total || *a.PayerTotal < 0 || *a.PayerRefund < 0 || *a.Currency != "CNY" {
		return model.WechatRefundResult{}, errors.New("invalid wechat refund amount")
	}
	v := model.WechatRefundResult{RefundNo: *r.OutRefundNo, ProviderRefundID: *r.RefundId, PaymentNo: *r.OutTradeNo, TransactionID: *r.TransactionId, Status: string(*r.Status), Currency: *a.Currency, TotalCents: uint64(*a.Total), RefundCents: uint64(*a.Refund), PayerTotalCents: uint64(*a.PayerTotal), PayerRefundCents: uint64(*a.PayerRefund)}
	if r.SuccessTime != nil {
		v.SuccessTime = *r.SuccessTime
	}
	if r.UserReceivedAccount != nil {
		v.ReceivedAccount = *r.UserReceivedAccount
	}
	if err := validateRefundShape(v); err != nil {
		return model.WechatRefundResult{}, err
	}
	return v, nil
}

func validateRefundShape(r model.WechatRefundResult) error {
	if r.RefundNo == "" || r.ProviderRefundID == "" || r.PaymentNo == "" || r.TransactionID == "" || r.Currency != "CNY" || len(r.ReceivedAccount) > 256 {
		return errors.New("invalid wechat refund identity")
	}
	switch r.Status {
	case "SUCCESS":
		if r.SuccessTime.IsZero() {
			return errors.New("missing refund success time")
		}
	case "PROCESSING", "CLOSED", "ABNORMAL":
	default:
		return errors.New("invalid refund state")
	}
	return nil
}

// Notification schema differs from QueryRefund (refund_status, no currency).
// SDK verifies the ORIGINAL bytes and decrypts using APIv3; no browser results
// or unsigned webhook fields can be used to change money.
func (s *Service) ParseRefundNotification(ctx context.Context, r *http.Request) (model.WechatRefundResult, error) {
	if !s.Enabled() {
		return model.WechatRefundResult{}, errors.New("wechat refund is disabled")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil || len(body) > 64<<10 {
		return model.WechatRefundResult{}, errors.New("invalid refund notification body")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		Resource *struct {
			Nonce string `json:"nonce"`
		} `json:"resource"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Resource == nil || len(envelope.Resource.Nonce) != 12 {
		return model.WechatRefundResult{}, errors.New("invalid refund notification resource")
	}
	var content struct {
		MchID           string     `json:"mchid"`
		PaymentNo       string     `json:"out_trade_no"`
		TransactionID   string     `json:"transaction_id"`
		RefundNo        string     `json:"out_refund_no"`
		ProviderID      string     `json:"refund_id"`
		Status          string     `json:"refund_status"`
		SuccessTime     *time.Time `json:"success_time"`
		ReceivedAccount string     `json:"user_received_account"`
		Amount          *struct {
			Total       *int64 `json:"total"`
			Refund      *int64 `json:"refund"`
			PayerTotal  *int64 `json:"payer_total"`
			PayerRefund *int64 `json:"payer_refund"`
			Currency    string `json:"currency"`
		} `json:"amount"`
	}
	n, err := s.notifyHandler.ParseNotifyRequest(ctx, r, &content)
	if err != nil {
		return model.WechatRefundResult{}, errors.New("refund notification verification or decryption failed")
	}
	a := content.Amount
	if n == nil || n.ID == "" || n.EventType != "REFUND."+content.Status || content.MchID != s.cfg.MchID || a == nil || a.Total == nil || a.Refund == nil || a.PayerTotal == nil || a.PayerRefund == nil || *a.Total <= 0 || *a.Refund <= 0 || *a.Refund > *a.Total || *a.PayerTotal < 0 || *a.PayerRefund < 0 || (a.Currency != "" && a.Currency != "CNY") {
		return model.WechatRefundResult{}, errors.New("refund notification does not match merchant")
	}
	v := model.WechatRefundResult{RefundNo: content.RefundNo, ProviderRefundID: content.ProviderID, PaymentNo: content.PaymentNo, TransactionID: content.TransactionID, MchID: content.MchID, Status: content.Status, ReceivedAccount: content.ReceivedAccount, Currency: "CNY", TotalCents: uint64(*a.Total), RefundCents: uint64(*a.Refund), PayerTotalCents: uint64(*a.PayerTotal), PayerRefundCents: uint64(*a.PayerRefund)}
	if content.SuccessTime != nil {
		v.SuccessTime = *content.SuccessTime
	}
	if err = validateRefundShape(v); err != nil {
		return model.WechatRefundResult{}, err
	}
	return v, nil
}
