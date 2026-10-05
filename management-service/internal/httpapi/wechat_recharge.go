package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/wechatpay"
)

func rechargeID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("rechargeID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "充值单编号无效")
		return 0, false
	}
	return id, true
}

func (s *Server) writeWechatRechargeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "充值单不存在")
	case errors.Is(err, appdb.ErrWechatRechargeInput):
		writeError(w, http.StatusBadRequest, "充值金额必须为正整数分，最多21474836.47元；提交标识无效请刷新页面")
	case errors.Is(err, appdb.ErrWechatRechargeConflict):
		writeError(w, http.StatusConflict, "充值金额与原充值单不一致，请继续原单或重新发起充值")
	default:
		s.writeWechatOrderError(w, err)
	}
}

func (s *Server) wechatRechargeCreate(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	var payload struct {
		AmountCents    uint64 `json:"amount_cents"`
		IdempotencyKey string `json:"idempotency_key"`
		PaymentMethod  string `json:"payment_method"`
	}
	if err := readJSON(w, r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "充值参数无效")
		return
	}
	item, err := s.store.CreateWechatRecharge(r.Context(), *actor.TenantID, actor.UserID, payload.AmountCents, payload.IdempotencyKey, payload.PaymentMethod)
	if err != nil {
		s.writeWechatRechargeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) wechatRechargeGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
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
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) wechatRechargePrepay(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
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
	if item.Status != "pending" {
		writeJSON(w, http.StatusOK, map[string]any{"recharge": item})
		return
	}
	if item.PaymentMethod != "wechat_jsapi" {
		writeError(w, http.StatusConflict, "此充值单请使用电脑付款二维码完成支付")
		return
	}
	if !strings.Contains(strings.ToLower(r.UserAgent()), "micromessenger") {
		writeError(w, http.StatusBadRequest, "请在微信中打开钱包页面进行充值")
		return
	}
	sessionKey := s.auth.SessionKey(r)
	if len(sessionKey) != 64 {
		writeError(w, http.StatusUnauthorized, "请重新登录后充值")
		return
	}
	openID, err := s.store.GetWechatOpenID(r.Context(), actor.UserID, sessionKey, s.wechatPay.AppID())
	if errors.Is(err, sql.ErrNoRows) {
		link, err := s.createWechatAuthorizationForPath(r.Context(), actor.UserID, sessionKey, "/wallet?recharge="+strconv.FormatInt(id, 10))
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "微信授权暂不可用，请稍后重试")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"recharge": item, "authorization_required": true, "authorization_url": link})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取微信授权失败")
		return
	}
	identity := model.WechatPaymentIdentity{AppID: s.wechatPay.AppID(), MchID: s.wechatPay.MchID(), OpenID: openID}
	attempt, err := s.store.PrepareWechatRechargePayment(r.Context(), *actor.TenantID, actor.UserID, id, identity)
	if err != nil {
		s.writeWechatRechargeError(w, err)
		return
	}
	if !attempt.ExpiresAt.After(time.Now().UTC()) {
		if _, err := s.reconcileWechatAttempt(r.Context(), attempt); err != nil {
			writeError(w, http.StatusServiceUnavailable, "旧支付正在核验，请查询到账结果，不要重复付款")
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
		attempt, err = s.store.PrepareWechatRechargePayment(r.Context(), *actor.TenantID, actor.UserID, id, identity)
		if err != nil {
			s.writeWechatRechargeError(w, err)
			return
		}
		if !attempt.ExpiresAt.After(time.Now().UTC()) {
			writeError(w, http.StatusConflict, "旧支付尚未确认关闭，请稍后查询到账结果")
			return
		}
	}
	var params wechatpay.JSAPIPaymentParams
	if attempt.PrepayID != "" {
		params, err = s.wechatPay.RequestPayment(r.Context(), attempt.PrepayID)
	} else {
		params, err = s.wechatPay.Prepay(r.Context(), wechatpay.PrepayInput{Description: attempt.Description, OutTradeNo: attempt.PaymentNo,
			OpenID: openID, ClientIP: requestClientIP(r), AmountCents: attempt.AmountCents, ExpiresAt: attempt.ExpiresAt})
		if err == nil {
			err = s.store.SaveWechatPrepayID(r.Context(), attempt.PaymentID, params.PrepayID)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "微信收银台暂不可用，请先查询到账结果再重试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recharge": item, "payment_no": attempt.PaymentNo, "payment_params": params})
}

func (s *Server) wechatRechargeQuery(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
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
	state := "NO_PENDING_PAYMENT"
	if attempt, err := s.store.GetPendingWechatRechargePayment(r.Context(), *actor.TenantID, id); err == nil {
		state, err = s.reconcileWechatAttempt(r.Context(), attempt)
		if err != nil {
			writeError(w, http.StatusBadGateway, "微信到账状态暂未确认，请稍后再查询；不要重复付款")
			return
		}
		item, err = s.store.GetWechatRecharge(r.Context(), *actor.TenantID, id)
		if err != nil {
			s.writeWechatRechargeError(w, err)
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.writeWechatRechargeError(w, err)
		return
	}
	if item.Status == "paid" {
		state = "SUCCESS"
	}
	writeJSON(w, http.StatusOK, map[string]any{"recharge": item, "trade_state": state})
}
