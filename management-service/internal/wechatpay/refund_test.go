package wechatpay

import (
	"bytes"
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"livecompanion/management/internal/model"
)

func signRefundTest(t *testing.T, key *rsa.PrivateKey, body []byte, stamp, nonce string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(stamp + "\n" + nonce + "\n" + string(body) + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func TestRefundNotificationVerification(t *testing.T) {
	s, key := testService(t)
	request := func(content string, event string, age time.Duration) *http.Request {
		block, _ := aes.NewCipher([]byte(s.cfg.APIV3Key))
		gcm, _ := cipher.NewGCM(block)
		body, _ := json.Marshal(map[string]any{"id": "refund-notification-test", "event_type": event, "resource": map[string]string{"algorithm": "AEAD_AES_256_GCM", "nonce": "123456789012", "associated_data": "refund", "ciphertext": base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte("123456789012"), []byte(content), []byte("refund")))}})
		stamp := fmt.Sprint(time.Now().Add(age).Unix())
		r := httptest.NewRequest("POST", s.RefundNotifyURL(), bytes.NewReader(body))
		r.Header.Set("Wechatpay-Serial", s.cfg.WechatPayPublicKeyID)
		r.Header.Set("Wechatpay-Timestamp", stamp)
		r.Header.Set("Wechatpay-Nonce", "test-refund-nonce")
		r.Header.Set("Wechatpay-Signature", signRefundTest(t, key, body, stamp, "test-refund-nonce"))
		return r
	}
	content := `{"mchid":"merchant-test","out_trade_no":"PAY-test","transaction_id":"TXN-test","out_refund_no":"WRF-test","refund_id":"REF-test","refund_status":"SUCCESS","success_time":"2026-10-03T12:00:00+08:00","user_received_account":"支付用户零钱","amount":{"total":1000,"refund":300,"payer_total":1000,"payer_refund":300}}`
	r, err := s.ParseRefundNotification(context.Background(), request(content, "REFUND.SUCCESS", 0))
	if err != nil || r.RefundCents != 300 || r.Currency != "CNY" || r.MchID != s.cfg.MchID {
		t.Fatal("valid signed refund rejected", r, err)
	}
	for _, test := range []struct {
		content, event string
		age            time.Duration
	}{
		{content, "REFUND.CLOSED", 0}, {strings.Replace(content, "merchant-test", "other-merchant", 1), "REFUND.SUCCESS", 0}, {strings.Replace(content, `"success_time":"2026-10-03T12:00:00+08:00",`, "", 1), "REFUND.SUCCESS", 0}, {strings.Replace(content, `"refund":300`, `"refund":-1`, 1), "REFUND.SUCCESS", 0}, {content, "REFUND.SUCCESS", -10 * time.Minute},
	} {
		if _, err = s.ParseRefundNotification(context.Background(), request(test.content, test.event, test.age)); err == nil {
			t.Fatal("invalid refund notification accepted", test)
		}
	}
	tampered := request(content, "REFUND.SUCCESS", 0)
	tampered.Header.Set("Wechatpay-Signature", "WECHATPAY/SIGNTEST/invalid")
	if _, err = s.ParseRefundNotification(context.Background(), tampered); err == nil {
		t.Fatal("forged refund signature accepted")
	}
	for _, body := range []string{`{}`, `{"resource":null}`, `{"resource":{"nonce":"bad"}}`} {
		if _, err = s.ParseRefundNotification(context.Background(), httptest.NewRequest("POST", s.RefundNotifyURL(), strings.NewReader(body))); err == nil {
			t.Fatal("malformed resource accepted")
		}
	}
}

type refundTestTransport func(*http.Request) (*http.Response, error)

func (fn refundTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestRefundSDKUsesOriginalTransactionAndVerifiedResponse(t *testing.T) {
	s, key := testService(t)
	calls := 0
	client, err := core.NewClient(context.Background(), option.WithWechatPayPublicKeyAuthCipher(s.cfg.MchID, s.cfg.MerchantCertificateSerial, key, s.cfg.WechatPayPublicKeyID, &key.PublicKey), option.WithHTTPClient(&http.Client{Transport: refundTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method == "POST" {
			var payload struct {
				TransactionID string `json:"transaction_id"`
				PaymentNo     string `json:"out_trade_no"`
				RefundNo      string `json:"out_refund_no"`
				NotifyURL     string `json:"notify_url"`
				Amount        struct {
					Refund, Total int64
					Currency      string
				} `json:"amount"`
			}
			if json.NewDecoder(req.Body).Decode(&payload) != nil || payload.TransactionID != "TXN-test" || payload.PaymentNo != "" || payload.RefundNo != "WRF-test" || payload.Amount.Refund != 300 || payload.Amount.Total != 1000 || payload.NotifyURL != "https://example.test/api/v1/payments/wechat/refund-notify" {
				t.Fatal("unsafe refund API parameters", payload)
			}
		} else if req.Method != "GET" || req.URL.Path != "/v3/refund/domestic/refunds/WRF-test" {
			t.Fatal("incorrect query", req.URL.Path)
		}
		body := []byte(`{"refund_id":"REF-test","out_refund_no":"WRF-test","transaction_id":"TXN-test","out_trade_no":"PAY-test","channel":"ORIGINAL","user_received_account":"支付用户零钱","create_time":"2026-10-03T12:00:00+08:00","status":"PROCESSING","amount":{"total":1000,"refund":300,"payer_total":1000,"payer_refund":300,"settlement_total":1000,"settlement_refund":300,"discount_refund":0,"currency":"CNY"}}`)
		stamp := fmt.Sprint(time.Now().Unix())
		headers := make(http.Header)
		headers.Set("Content-Type", "application/json")
		headers.Set("Wechatpay-Serial", s.cfg.WechatPayPublicKeyID)
		headers.Set("Wechatpay-Timestamp", stamp)
		headers.Set("Wechatpay-Nonce", "refund-response-nonce")
		headers.Set("Wechatpay-Signature", signRefundTest(t, key, body, stamp, "refund-response-nonce"))
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	s.jsapi.Client = client
	i := model.WechatCashRefundItem{RefundNo: "WRF-test", TransactionID: "TXN-test", AppID: s.cfg.AppID, MchID: s.cfg.MchID, AmountCents: 300, TotalCents: 1000}
	if r, err := s.CreateRefund(context.Background(), i); err != nil || r.Status != "PROCESSING" || r.RefundCents != 300 {
		t.Fatal("SDK refund", r, err)
	}
	if r, err := s.QueryRefund(context.Background(), i.RefundNo); err != nil || r.PaymentNo != "PAY-test" {
		t.Fatal("SDK refund query", r, err)
	}
	i.MchID = "other"
	if _, err = s.CreateRefund(context.Background(), i); err == nil || calls != 2 {
		t.Fatal("wrong merchant contacted provider")
	}
}
