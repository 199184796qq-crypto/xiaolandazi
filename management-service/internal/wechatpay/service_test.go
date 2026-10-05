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
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
)

func testService(t *testing.T) (*Service, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privatePath, publicPath := filepath.Join(dir, "test-private.pem"), filepath.Join(dir, "test-public.pem")
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	public, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(privatePath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: public}), 0600); err != nil {
		t.Fatal(err)
	}
	service, err := New(context.Background(), Config{Enabled: true, AppID: "wx-test", MchID: "merchant-test", MerchantCertificateSerial: "test-serial",
		MerchantPrivateKeyPath: privatePath, WechatPayPublicKeyPath: publicPath, WechatPayPublicKeyID: "PUB_KEY_ID_TEST",
		APIV3Key: "0123456789abcdef0123456789abcdef", OfficialAccountAppSecret: "test-secret-not-real",
		NotifyURL: "https://example.test/api/notify", OAuthCallbackURL: "https://example.test/api/oauth/callback"})
	if err != nil {
		t.Fatal(err)
	}
	return service, key
}

func TestRequestPaymentAndOAuth(t *testing.T) {
	service, key := testService(t)
	params, err := service.RequestPayment(context.Background(), "test-prepay")
	if err != nil {
		t.Fatal(err)
	}
	message := fmt.Sprintf("%s\n%s\n%s\n%s\n", params.AppID, params.TimeStamp, params.NonceStr, params.Package)
	sum := sha256.Sum256([]byte(message))
	signature, err := base64.StdEncoding.DecodeString(params.PaySign)
	if err != nil || rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], signature) != nil {
		t.Fatal("invalid bridge signature", err)
	}
	if params.Package != "prepay_id=test-prepay" || params.SignType != "RSA" {
		t.Fatal("incorrect bridge parameters")
	}
	raw, err := service.OAuthURL("random-state")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "open.weixin.qq.com" || u.Query().Get("scope") != "snsapi_base" || u.Query().Get("redirect_uri") != service.cfg.OAuthCallbackURL || strings.Contains(raw, service.cfg.OfficialAccountAppSecret) {
		t.Fatal("unsafe OAuth URL")
	}
	publicJSON, _ := json.Marshal(params)
	if bytes.Contains(publicJSON, []byte("test-secret")) || bytes.Contains(publicJSON, []byte("PRIVATE KEY")) {
		t.Fatal("secret in bridge response")
	}
}

func TestPaymentNotificationVerification(t *testing.T) {
	service, key := testService(t)
	content := []byte(`{"appid":"wx-test","mchid":"merchant-test","out_trade_no":"payment-test","transaction_id":"transaction-test","trade_state":"SUCCESS","success_time":"2026-10-03T12:00:00+08:00","amount":{"total":8800,"currency":"CNY"},"payer":{"openid":"test-openid"}}`)
	block, err := aes.NewCipher([]byte(service.cfg.APIV3Key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := "123456789012"
	ciphertext := base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(nonce), content, []byte("transaction")))
	envelope := map[string]any{"id": "notify-test", "event_type": "TRANSACTION.SUCCESS", "resource": map[string]string{"algorithm": "AEAD_AES_256_GCM", "nonce": nonce, "associated_data": "transaction", "ciphertext": ciphertext}}
	body, _ := json.Marshal(envelope)
	request := func(body []byte, timestamp time.Time) *http.Request {
		t.Helper()
		stamp := fmt.Sprint(timestamp.Unix())
		message := stamp + "\nrequest-nonce\n" + string(body) + "\n"
		sum := sha256.Sum256([]byte(message))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "https://example.test/api/notify", bytes.NewReader(body))
		r.Header.Set("Wechatpay-Serial", service.cfg.WechatPayPublicKeyID)
		r.Header.Set("Wechatpay-Timestamp", stamp)
		r.Header.Set("Wechatpay-Nonce", "request-nonce")
		r.Header.Set("Wechatpay-Signature", base64.StdEncoding.EncodeToString(sig))
		return r
	}
	valid := request(body, time.Now())
	transaction, err := service.ParsePaymentNotification(context.Background(), valid)
	if err != nil || transaction.AmountCents != 8800 || transaction.TransactionID != "transaction-test" || transaction.PayerOpenID != "test-openid" || transaction.EventType != "TRANSACTION.SUCCESS" {
		t.Fatal("valid notification rejected", err)
	}
	tampered := request(body, time.Now())
	tampered.Body = io.NopCloser(bytes.NewReader(append(body, byte(' '))))
	if _, err := service.ParsePaymentNotification(context.Background(), tampered); err == nil {
		t.Fatal("tampered body accepted")
	}
	if _, err := service.ParsePaymentNotification(context.Background(), request(body, time.Now().Add(-10*time.Minute))); err == nil {
		t.Fatal("expired signature accepted")
	}
	for _, invalid := range []string{`{}`, `{"resource":null}`, `{"resource":{"nonce":"short"}}`} {
		if _, err := service.ParsePaymentNotification(context.Background(), request([]byte(invalid), time.Now())); err == nil {
			t.Fatal("invalid resource accepted")
		}
	}
	wrong := request(body, time.Now())
	wrong.Header.Set("Wechatpay-Serial", "PUB_KEY_ID_UNKNOWN")
	if _, err := service.ParsePaymentNotification(context.Background(), wrong); err == nil {
		t.Fatal("untrusted key accepted")
	}
	envelope["resource"].(map[string]string)["ciphertext"] = "not-base64"
	broken, _ := json.Marshal(envelope)
	if _, err := service.ParsePaymentNotification(context.Background(), request(broken, time.Now())); err == nil {
		t.Fatal("invalid ciphertext accepted")
	}
}

func TestConfigurationAndTransactionValidation(t *testing.T) {
	service, err := New(context.Background(), Config{})
	if err != nil || service.Enabled() {
		t.Fatal("disabled config requires credentials")
	}
	if _, err := New(context.Background(), Config{Enabled: true}); err == nil {
		t.Fatal("missing credentials accepted")
	}
	if _, err := normalizeTransaction(&payments.Transaction{Appid: core.String("app"), Mchid: core.String("mch"), OutTradeNo: core.String("pay"), TradeState: core.String("SUCCESS")}); err == nil {
		t.Fatal("partial success accepted")
	}
	value := truncateUTF8(strings.Repeat("中", 41), 120)
	if len(value) != 120 || !utf8.ValidString(value) {
		t.Fatal("invalid UTF8 truncation")
	}
}
