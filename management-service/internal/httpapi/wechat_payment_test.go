package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/wechatpay"
)

func TestProductionSandboxPaymentDisabled(t *testing.T) {
	for _, env := range []string{"production", "prod", "staging", ""} {
		s := &Server{env: env}
		for _, handler := range []http.HandlerFunc{s.customerShopSandboxPayOrder, s.customerShopSandboxRefundOrder} {
			w := httptest.NewRecorder()
			handler(w, httptest.NewRequest(http.MethodPost, "/api/v1/shop/orders/1/sandbox-pay", nil))
			if w.Code != http.StatusForbidden {
				t.Fatalf("sandbox allowed in %q: %d", env, w.Code)
			}
		}
	}
}

func TestWechatPaymentDisabled(t *testing.T) {
	s := &Server{}
	for _, handler := range []http.HandlerFunc{s.wechatShopPrepay, s.wechatShopQuery, s.wechatPaymentNotify, s.wechatOAuthCallback, s.wechatRechargeCreate, s.wechatRechargePrepay, s.wechatRechargeNativePrepay, s.wechatRechargeQuery} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, "/api/v1/payments/wechat/notify", nil))
		if w.Code != http.StatusServiceUnavailable || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("unconfigured payment is not safely disabled", w.Code)
		}
	}
}

func TestWechatPaymentReturnAndOrigin(t *testing.T) {
	for _, path := range []string{"https://evil.test/shop/checkout?order=1", "//evil.test/shop/checkout?order=1", "/shop/checkout?order=-1", "/shop/checkout?order=0", "/shop/checkout?order=abc", "/login?order=1", "/shop/checkout?order=1#x", "/shop/checkout\\?order=1", "/wallet?recharge=0", "/wallet?recharge=-1", "/wallet?recharge=1&next=https://evil.test", "/wallet?recharge=1&recharge=2", "/wallet?order=1"} {
		if validWechatReturnPath(path) {
			t.Fatalf("unsafe OAuth return accepted: %s", path)
		}
	}
	if !validWechatReturnPath("/shop/checkout?order=42") {
		t.Fatal("valid checkout rejected")
	}
	if !validWechatReturnPath("/wallet?recharge=42") {
		t.Fatal("valid recharge destination rejected")
	}
	s := &Server{publicWebURL: "https://www.xiaolandaizi.cn"}
	for _, origin := range []string{"https://evil.test", "https://www.xiaolandaizi.cn.evil.test", "null", "http://www.xiaolandaizi.cn"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "https://www.xiaolandaizi.cn/api", nil)
		r.Header.Set("Origin", origin)
		if s.paymentSameOrigin(w, r) || w.Code != http.StatusForbidden {
			t.Fatal("cross-origin payment accepted")
		}
	}
	r := httptest.NewRequest("POST", "https://www.xiaolandaizi.cn/api", nil)
	r.Header.Set("Origin", "https://www.xiaolandaizi.cn")
	if !s.paymentSameOrigin(httptest.NewRecorder(), r) {
		t.Fatal("same origin rejected")
	}
	if shouldAuditRequest("POST", "/api/v1/payments/wechat/notify") {
		t.Fatal("provider signature should not depend on admin cookie auditing")
	}
}

type rejectingWechatProvider struct{ *wechatpay.Service }

func (rejectingWechatProvider) Enabled() bool { return true }
func (rejectingWechatProvider) ParsePaymentNotification(context.Context, *http.Request) (wechatpay.Transaction, error) {
	return wechatpay.Transaction{}, errors.New("bad signature")
}

func TestWechatNotificationForged(t *testing.T) {
	s := &Server{wechatPay: rejectingWechatProvider{}}
	w := httptest.NewRecorder()
	s.wechatPaymentNotify(w, httptest.NewRequest("POST", "/api/v1/payments/wechat/notify", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatal("forged callback not rejected", w.Code)
	}
}

func TestWechatNativePrepayRejectsCrossOrigin(t *testing.T) {
	s := &Server{wechatPay: rejectingWechatProvider{}, publicWebURL: "https://www.xiaolandaizi.cn"}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "https://www.xiaolandaizi.cn/api/v1/wallet/recharges/42/wechat-native-prepay", nil)
	r.Header.Set("Origin", "https://evil.test")
	s.wechatRechargeNativePrepay(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("cross-origin Native prepay accepted", w.Code)
	}
}
