package httpapi

import (
	"context"
	"net/http"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/wechatpay"
)

type WechatNativeProvider interface {
	NativePrepay(context.Context, wechatpay.PrepayInput) (string, error)
}

// Desktop creates a customer-owned recharge first. Its QR contains only the
// provider payment URL, not a login token or a wallet mutation URL.
func (s *Server) wechatRechargeNativePrepay(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	provider, ok := s.wechatPay.(WechatNativeProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "微信扫码支付尚未配置")
		return
	}
	id, ok := rechargeID(w, r)
	if !ok {
		return
	}
	item, err := s.store.GetWechatRecharge(r.Context(), *actor.TenantID, id)
	if err != nil {
		s.writeWechatRechargeError(w, err)
		return
	}
	if item.PaymentMethod != "wechat_native" {
		writeError(w, http.StatusConflict, "此充值单不是电脑扫码充值，请重新创建扫码充值单")
		return
	}
	if item.Status != "pending" {
		writeJSON(w, http.StatusOK, map[string]any{"recharge": item})
		return
	}
	identity := model.WechatPaymentIdentity{AppID: s.wechatPay.AppID(), MchID: s.wechatPay.MchID()}
	attempt, err := s.store.PrepareWechatNativeRechargePayment(r.Context(), *actor.TenantID, actor.UserID, id, identity)
	if err != nil {
		s.writeWechatRechargeError(w, err)
		return
	}
	if !attempt.ExpiresAt.After(time.Now().UTC()) {
		if _, err := s.reconcileWechatAttempt(r.Context(), attempt); err != nil {
			writeError(w, http.StatusBadGateway, "原二维码正在核验，请先查询到账结果，不要重复付款")
			return
		}
		item, err = s.store.GetWechatRecharge(r.Context(), *actor.TenantID, id)
		if err != nil {
			s.writeWechatRechargeError(w, err)
			return
		}
		if item.Status != "pending" {
			writeJSON(w, http.StatusOK, map[string]any{"recharge": item})
			return
		}
		attempt, err = s.store.PrepareWechatNativeRechargePayment(r.Context(), *actor.TenantID, actor.UserID, id, identity)
		if err != nil {
			s.writeWechatRechargeError(w, err)
			return
		}
		if !attempt.ExpiresAt.After(time.Now().UTC()) {
			writeError(w, http.StatusConflict, "原二维码尚未确认关闭，请稍后重新获取")
			return
		}
	}
	codeURL := attempt.CodeURL
	if codeURL == "" {
		codeURL, err = provider.NativePrepay(r.Context(), wechatpay.PrepayInput{
			Description: attempt.Description, OutTradeNo: attempt.PaymentNo,
			AmountCents: attempt.AmountCents, ExpiresAt: attempt.ExpiresAt,
		})
		if err == nil && wechatpay.ValidNativeCodeURL(codeURL) {
			err = s.store.SaveWechatNativeCodeURL(r.Context(), attempt.PaymentID, codeURL)
		} else if err == nil {
			writeError(w, http.StatusBadGateway, "微信返回的付款二维码无效，请查询结果后重试")
			return
		}
		if err != nil {
			if wechatpay.IsNativeNotAuthorized(err) {
				writeError(w, http.StatusServiceUnavailable, "商户 Native 扫码支付权限未生效，请管理员在微信支付商户平台确认开通；未扣款")
			} else {
				writeError(w, http.StatusBadGateway, "获取微信付款二维码失败，请重试原充值单或查询到账结果，不要重复付款")
			}
			return
		}
	}
	image, err := wechatpay.NativeQRCodeDataURL(codeURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "付款二维码暂不可用，请重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recharge": item, "payment_no": attempt.PaymentNo, "code_url": codeURL,
		"qr_code_data_url": image, "expires_at": attempt.ExpiresAt})
}

var _ WechatNativeProvider = (*wechatpay.Service)(nil)
