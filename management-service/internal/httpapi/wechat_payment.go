package httpapi

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/wechatpay"
)

type WechatPaymentProvider interface {
	Enabled() bool
	AppID() string
	MchID() string
	OAuthURL(string) (string, error)
	ExchangeOAuthCode(context.Context, string) (string, error)
	Prepay(context.Context, wechatpay.PrepayInput) (wechatpay.JSAPIPaymentParams, error)
	RequestPayment(context.Context, string) (wechatpay.JSAPIPaymentParams, error)
	ParsePaymentNotification(context.Context, *http.Request) (wechatpay.Transaction, error)
	Query(context.Context, string) (wechatpay.Transaction, error)
	Close(context.Context, string) error
}

func (s *Server) SetWechatPay(provider WechatPaymentProvider) { s.wechatPay = provider }

func (s *Server) registerWechatPaymentRoutes(mux *http.ServeMux) {
	s.registerWechatRefundRoutes(mux)
	mux.HandleFunc("GET /api/v1/payments/wechat/status", s.wechatPaymentStatus)
	mux.HandleFunc("GET /api/v1/payments/wechat/oauth/callback", s.wechatOAuthCallback)
	mux.HandleFunc("POST /api/v1/payments/wechat/notify", s.wechatPaymentNotify)
	mux.HandleFunc("POST /api/v1/shop/orders/{orderID}/wechat-prepay", s.wechatShopPrepay)
	mux.HandleFunc("POST /api/v1/shop/orders/{orderID}/wechat-query", s.wechatShopQuery)
	mux.HandleFunc("POST /api/v1/wallet/recharges", s.wechatRechargeCreate)
	mux.HandleFunc("GET /api/v1/wallet/recharges/{rechargeID}", s.wechatRechargeGet)
	mux.HandleFunc("POST /api/v1/wallet/recharges/{rechargeID}/wechat-prepay", s.wechatRechargePrepay)
	mux.HandleFunc("POST /api/v1/wallet/recharges/{rechargeID}/wechat-native-prepay", s.wechatRechargeNativePrepay)
	mux.HandleFunc("POST /api/v1/wallet/recharges/{rechargeID}/wechat-query", s.wechatRechargeQuery)
}

func (s *Server) wechatReady(w http.ResponseWriter) bool {
	w.Header().Set("Cache-Control", "no-store")
	if s.wechatPay == nil || !s.wechatPay.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "微信支付尚未开通，请联系管理员完成商户配置")
		return false
	}
	return true
}

func (s *Server) wechatPaymentStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireCustomerShopActor(w, r); !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	enabled := s.wechatPay != nil && s.wechatPay.Enabled()
	_, nativeSupported := s.wechatPay.(WechatNativeProvider)
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled, "mode": "jsapi", "native_supported": enabled && nativeSupported, "auto_renew_supported": false})
}

// Payment mutation endpoints are same-origin cookie-authenticated requests.
func (s *Server) paymentSameOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	base, err := url.Parse(s.publicWebURL)
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" ||
		(origin != "" && (err != nil || !strings.EqualFold(origin, base.Scheme+"://"+base.Host))) {
		writeError(w, http.StatusForbidden, "支付请求来源不合法")
		return false
	}
	return true
}

func (s *Server) createWechatAuthorization(ctx context.Context, userID int64, sessionKey string, orderID int64) (string, error) {
	return s.createWechatAuthorizationForPath(ctx, userID, sessionKey, "/shop/checkout?order="+strconv.FormatInt(orderID, 10))
}

func (s *Server) createWechatAuthorizationForPath(ctx context.Context, userID int64, sessionKey, path string) (string, error) {
	if len(sessionKey) != 64 || !validWechatReturnPath(path) {
		return "", errors.New("real session required")
	}
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", err
	}
	state := hex.EncodeToString(stateBytes)
	if err := s.store.CreateWechatOAuthState(ctx, userID, sessionKey, state, path, time.Now().UTC().Add(10*time.Minute)); err != nil {
		return "", err
	}
	return s.wechatPay.OAuthURL(state)
}

// Only local checkout destinations created by this server may be returned to.
func validWechatReturnPath(path string) bool {
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || strings.ContainsAny(path, "\\\r\n") {
		return false
	}
	key := "order"
	if u.Path == "/wallet" {
		key = "recharge"
	} else if u.Path != "/shop/checkout" {
		return false
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 || len(query[key]) != 1 {
		return false
	}
	id, err := strconv.ParseInt(query.Get(key), 10, 64)
	return err == nil && id > 0
}

func (s *Server) wechatOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !s.wechatReady(w) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if len(state) != 64 {
		writeError(w, http.StatusBadRequest, "微信授权已失效，请重新发起支付")
		return
	}
	sessionKey := s.auth.SessionKey(r)
	snapshot, err := s.store.ConsumeWechatOAuthState(r.Context(), actor.UserID, sessionKey, state)
	if err != nil || !validWechatReturnPath(snapshot.ReturnPath) {
		writeError(w, http.StatusBadRequest, "微信授权已失效或登录账号发生变化，请重新发起支付")
		return
	}
	u, _ := url.Parse(snapshot.ReturnPath)
	query := u.Query()
	query.Set("wechat_auth", "failed")
	openID, err := s.wechatPay.ExchangeOAuthCode(r.Context(), r.URL.Query().Get("code"))
	if err == nil {
		err = s.store.BindWechatOpenID(r.Context(), actor.UserID, sessionKey, s.wechatPay.AppID(), openID)
		if err == nil {
			query.Set("wechat_auth", "ready")
		}
	}
	// Neither code, OpenID, secret nor provider error text is exposed in redirects.
	u.RawQuery = query.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func (s *Server) wechatShopPrepay(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}
	order, err := s.store.GetCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
	if err != nil {
		s.writeWechatOrderError(w, err)
		return
	}
	if order.Status != "pending" {
		writeJSON(w, http.StatusOK, map[string]any{"order": order})
		return
	}
	if order.OrderType != "membership" && order.OrderType != "time_card" {
		writeError(w, http.StatusBadRequest, "微信支付本阶段仅支持会员和时长卡订单")
		return
	}
	if !strings.Contains(strings.ToLower(r.UserAgent()), "micromessenger") {
		writeError(w, http.StatusBadRequest, "请在微信中打开手机商城使用微信支付")
		return
	}
	sessionKey := s.auth.SessionKey(r)
	if len(sessionKey) != 64 {
		writeError(w, http.StatusUnauthorized, "请重新登录后发起微信支付")
		return
	}
	openID, err := s.store.GetWechatOpenID(r.Context(), actor.UserID, sessionKey, s.wechatPay.AppID())
	if errors.Is(err, sql.ErrNoRows) {
		authorizeURL, err := s.createWechatAuthorization(r.Context(), actor.UserID, sessionKey, orderID)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "微信授权暂不可用，请稍后重试")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"authorization_required": true, "authorization_url": authorizeURL, "order": order})
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取微信授权失败")
		return
	}
	identity := model.WechatPaymentIdentity{AppID: s.wechatPay.AppID(), MchID: s.wechatPay.MchID(), OpenID: openID}
	attempt, err := s.store.PrepareWechatShopPayment(r.Context(), *actor.TenantID, actor.UserID, orderID, identity)
	if err != nil {
		s.writeWechatOrderError(w, err)
		return
	}
	if !attempt.ExpiresAt.After(time.Now().UTC()) {
		if _, err := s.reconcileWechatAttempt(r.Context(), attempt); err != nil {
			writeError(w, http.StatusServiceUnavailable, "旧支付正在核验，请稍后查询结果再支付")
			return
		}
		order, err = s.store.GetCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
		if err != nil {
			s.writeWechatOrderError(w, err)
			return
		}
		if order.Status != "pending" {
			writeJSON(w, http.StatusOK, map[string]any{"order": order})
			return
		}
		attempt, err = s.store.PrepareWechatShopPayment(r.Context(), *actor.TenantID, actor.UserID, orderID, identity)
		if err != nil {
			s.writeWechatOrderError(w, err)
			return
		}
		if !attempt.ExpiresAt.After(time.Now().UTC()) {
			writeError(w, http.StatusConflict, "旧支付尚未确认关闭，请稍后查询结果")
			return
		}
	}
	var params wechatpay.JSAPIPaymentParams
	if attempt.PrepayID != "" {
		params, err = s.wechatPay.RequestPayment(r.Context(), attempt.PrepayID)
	} else {
		params, err = s.wechatPay.Prepay(r.Context(), wechatpay.PrepayInput{Description: attempt.Description, OutTradeNo: attempt.PaymentNo, OpenID: openID, ClientIP: requestClientIP(r), AmountCents: attempt.AmountCents, ExpiresAt: attempt.ExpiresAt})
		if err == nil {
			err = s.store.SaveWechatPrepayID(r.Context(), attempt.PaymentID, params.PrepayID)
		}
	}
	if err != nil {
		// Keep the durable attempt pending: even a timeout can have created a bill.
		writeError(w, http.StatusBadGateway, "微信收银台暂不可用，请查询支付结果后重试；不会自动扣除钱包余额")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": order, "payment_no": attempt.PaymentNo, "payment_params": params})
}

func (s *Server) writeWechatOrderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appdb.ErrUnsupportedWechatAutoRenew):
		writeError(w, http.StatusConflict, "微信自动续费暂未开通，请选择普通月付重新下单")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "订单不存在")
	case errors.Is(err, appdb.ErrUnsupportedShopProduct):
		writeError(w, http.StatusBadRequest, "此商品暂不支持微信支付")
	case errors.Is(err, appdb.ErrShopOrderCancelled):
		writeError(w, http.StatusConflict, "订单已取消")
	case errors.Is(err, appdb.ErrShopOrderAlreadyPaid):
		writeError(w, http.StatusConflict, "订单已支付，请查询结果")
	case errors.Is(err, appdb.ErrWechatPaymentMismatch):
		writeError(w, http.StatusConflict, "支付身份或金额不一致，请查询支付结果或联系管理员")
	default:
		writeError(w, http.StatusServiceUnavailable, "支付记录暂不可用，请稍后重试")
	}
}

func (s *Server) applyVerifiedWechatTransaction(ctx context.Context, expectedNo string, transaction wechatpay.Transaction) error {
	if transaction.AppID != s.wechatPay.AppID() || transaction.MchID != s.wechatPay.MchID() || transaction.OutTradeNo != expectedNo {
		return appdb.ErrWechatPaymentMismatch
	}
	if transaction.TradeState != "SUCCESS" {
		return s.store.UpdateWechatTradeState(ctx, expectedNo, transaction.TradeState)
	}
	err := s.store.CompleteVerifiedWechatPayment(ctx, model.WechatPaymentResult{
		AppID: transaction.AppID, MchID: transaction.MchID, OutTradeNo: transaction.OutTradeNo, TransactionID: transaction.TransactionID,
		TradeState: transaction.TradeState, TradeType: transaction.TradeType, Currency: transaction.Currency, AmountCents: transaction.AmountCents, SuccessTime: transaction.SuccessTime,
		PayerOpenID: transaction.PayerOpenID, NotificationID: transaction.NotificationID,
	})
	return err
}

func (s *Server) reconcileWechatAttempt(ctx context.Context, attempt model.WechatPaymentPreparation) (string, error) {
	transaction, err := s.wechatPay.Query(ctx, attempt.PaymentNo)
	if err != nil {
		if !wechatpay.IsOrderNotExist(err) {
			return "", err
		}
		// A never-created attempt may be retired only once its original expiry is
		// past plus a margin. A concurrent prepay with that expiry cannot succeed.
		state := "NOTPAY"
		if time.Now().UTC().After(attempt.ExpiresAt.Add(30 * time.Second)) {
			state = "CLOSED"
		}
		return state, s.store.UpdateWechatTradeState(ctx, attempt.PaymentNo, state)
	}
	if err := s.applyVerifiedWechatTransaction(ctx, attempt.PaymentNo, transaction); err != nil {
		return "", err
	}
	if transaction.TradeState == "NOTPAY" && !attempt.ExpiresAt.After(time.Now().UTC()) {
		if err := s.wechatPay.Close(ctx, attempt.PaymentNo); err != nil {
			return "", err
		}
		return "CLOSED", s.store.UpdateWechatTradeState(ctx, attempt.PaymentNo, "CLOSED")
	}
	return transaction.TradeState, nil
}

func (s *Server) wechatShopQuery(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) || !s.paymentSameOrigin(w, r) {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	orderID, ok := shopOrderID(w, r)
	if !ok {
		return
	}
	order, err := s.store.GetCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
	if err != nil {
		s.writeWechatOrderError(w, err)
		return
	}
	state := "NO_PENDING_PAYMENT"
	if attempt, err := s.store.GetPendingWechatPayment(r.Context(), *actor.TenantID, orderID); err == nil {
		state, err = s.reconcileWechatAttempt(r.Context(), attempt)
		if err != nil {
			writeError(w, http.StatusBadGateway, "微信到账状态暂未确认，请稍后再查询；不要重复付款")
			return
		}
		order, err = s.store.GetCustomerShopOrder(r.Context(), *actor.TenantID, orderID)
		if err != nil {
			s.writeWechatOrderError(w, err)
			return
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.writeWechatOrderError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"order": order, "trade_state": state})
}

func (s *Server) wechatPaymentNotify(w http.ResponseWriter, r *http.Request) {
	if !s.wechatReady(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	transaction, err := s.wechatPay.ParsePaymentNotification(r.Context(), r)
	if err != nil || transaction.EventType != "TRANSACTION.SUCCESS" || transaction.TradeState != "SUCCESS" {
		writeError(w, http.StatusBadRequest, "invalid payment notification")
		return
	}
	if err := s.applyVerifiedWechatTransaction(r.Context(), transaction.OutTradeNo, transaction); err != nil {
		// No success acknowledgement until the financial transaction has committed.
		log.Printf("wechat payment notification settlement failed: order=%s", transaction.OutTradeNo)
		writeError(w, http.StatusInternalServerError, "payment settlement unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Query reconciliation recovers lost notifications and expires bills through
// the provider. Only the leader queries; all settlement paths are idempotent.
func (s *Server) RunWechatPaymentReconciliation(ctx context.Context) {
	if s.wechatPay == nil || !s.wechatPay.Enabled() {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if s.leader != nil && !s.leader.IsLeader() {
			continue
		}
		batchCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		items, err := s.store.ListPendingWechatPayments(batchCtx)
		if err == nil {
			for _, item := range items {
				if batchCtx.Err() != nil {
					break
				}
				if _, err := s.reconcileWechatAttempt(batchCtx, item); err != nil {
					log.Printf("wechat payment reconciliation pending: payment=%s", item.PaymentNo)
				}
			}
		} else {
			log.Print("wechat payment reconciliation database unavailable")
		}
		cancel()
	}
}

var _ WechatPaymentProvider = (*wechatpay.Service)(nil)
