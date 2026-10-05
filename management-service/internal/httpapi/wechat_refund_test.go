package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"livecompanion/management/internal/model"
)

func (rejectingWechatProvider) ParseRefundNotification(context.Context, *http.Request) (model.WechatRefundResult, error) {
	return model.WechatRefundResult{}, errors.New("invalid signature")
}

func TestWechatRefundGuards(t *testing.T) {
	s := &Server{publicWebURL: "https://www.xiaolandaizi.cn"}
	for _, fn := range []http.HandlerFunc{s.wechatRefundCreate, s.wechatRefundQuery, s.wechatRefundNotify} {
		w := httptest.NewRecorder()
		fn(w, httptest.NewRequest("POST", "https://www.xiaolandaizi.cn/api/v1/wallet/wechat-refunds", nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatal("disabled refund accepted", w.Code)
		}
	}
	for _, fn := range []http.HandlerFunc{s.wechatRefundCreate, s.wechatRefundQuery} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "https://www.xiaolandaizi.cn/api/v1/wallet/wechat-refunds", nil)
		r.Header.Set("Origin", "https://evil.test")
		fn(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatal("cross-origin refund accepted")
		}
	}
	s.wechatPay = rejectingWechatProvider{}
	w := httptest.NewRecorder()
	s.wechatRefundNotify(w, httptest.NewRequest("POST", "/api/v1/payments/wechat/refund-notify", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatal("forged refund reached database", w.Code)
	}
	if shouldAuditRequest("POST", "/api/v1/payments/wechat/refund-notify") {
		t.Fatal("provider callback requires admin cookie audit")
	}
}
