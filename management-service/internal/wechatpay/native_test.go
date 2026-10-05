package wechatpay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
)

func TestNativeSDKAndQRCode(t *testing.T) {
	s, key := testService(t)
	codeURL := "weixin://wxpay/bizpayurl?pr=native-test"
	calls := 0
	forged := false
	client, err := core.NewClient(context.Background(), option.WithWechatPayPublicKeyAuthCipher(s.cfg.MchID, s.cfg.MerchantCertificateSerial, key, s.cfg.WechatPayPublicKeyID, &key.PublicKey), option.WithHTTPClient(&http.Client{Transport: refundTestTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		var payload map[string]json.RawMessage
		if req.Method != "POST" || req.URL.Path != "/v3/pay/transactions/native" || json.NewDecoder(req.Body).Decode(&payload) != nil {
			t.Fatal("not a Native prepay")
		}
		if string(payload["out_trade_no"]) != `"WXP-native-test"` || string(payload["mchid"]) != `"merchant-test"` ||
			string(payload["appid"]) != `"wx-test"` || len(payload["payer"]) > 0 || !strings.Contains(string(payload["amount"]), `"total":123`) ||
			!strings.Contains(string(payload["amount"]), `"currency":"CNY"`) || string(payload["notify_url"]) != fmt.Sprintf("%q", s.cfg.NotifyURL) || len(payload["time_expire"]) == 0 {
			t.Fatal("incorrect Native snapshot", payload)
		}
		body, _ := json.Marshal(map[string]string{"code_url": codeURL})
		stamp := fmt.Sprint(time.Now().Unix())
		headers := make(http.Header)
		headers.Set("Content-Type", "application/json")
		headers.Set("Wechatpay-Serial", s.cfg.WechatPayPublicKeyID)
		headers.Set("Wechatpay-Timestamp", stamp)
		headers.Set("Wechatpay-Nonce", "native-response")
		headers.Set("Wechatpay-Signature", signRefundTest(t, key, body, stamp, "native-response"))
		if forged {
			headers.Set("Wechatpay-Signature", "invalid")
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(bytes.NewReader(body)), Request: req}, nil
	})}))
	if err != nil {
		t.Fatal(err)
	}
	s.jsapi.Client = client
	input := PrepayInput{OutTradeNo: "WXP-native-test", AmountCents: 123, ExpiresAt: time.Now().Add(15 * time.Minute)}
	actual, err := s.NativePrepay(context.Background(), input)
	if err != nil || actual != codeURL {
		t.Fatal("signed native response", actual, err)
	}
	image, err := NativeQRCodeDataURL(actual)
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(image, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	picture, err := png.Decode(bytes.NewReader(data))
	if err != nil || picture.Bounds().Dx() != 256 || picture.Bounds().Dy() != 256 {
		t.Fatal("invalid QR PNG", err)
	}
	forged = true
	if _, err := s.NativePrepay(context.Background(), input); err == nil {
		t.Fatal("forged Native API response accepted")
	}
	for _, mutate := range []func(*PrepayInput){
		func(i *PrepayInput) { i.OpenID = "browser-supplied-payer" },
		func(i *PrepayInput) { i.AmountCents = 0 },
		func(i *PrepayInput) { i.AmountCents = 2147483648 },
		func(i *PrepayInput) { i.ExpiresAt = time.Now().Add(-time.Minute) },
	} {
		bad := input
		mutate(&bad)
		if _, err := s.NativePrepay(context.Background(), bad); err == nil || calls != 2 {
			t.Fatal("invalid input contacted provider", err)
		}
	}
	for _, value := range []string{"", "javascript:alert(1)", "https://evil.test/pay", "weixin://pay/path", "weixin://user:pass@pay/?pr=x", "weixin://pay/?pr=x#fake", "weixin://pay/?pr=x\n"} {
		if _, err := NativeQRCodeDataURL(value); err == nil {
			t.Fatal("unsafe QR URL", value)
		}
	}
}
