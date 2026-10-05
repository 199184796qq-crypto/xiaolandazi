package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/wechatpay"
)

type WechatRefundProvider interface {
	CreateRefund(context.Context, model.WechatCashRefundItem) (model.WechatRefundResult, error)
	QueryRefund(context.Context, string) (model.WechatRefundResult, error)
	ParseRefundNotification(context.Context, *http.Request) (model.WechatRefundResult, error)
}

func (s *Server) refundProvider(w http.ResponseWriter) (WechatRefundProvider, bool) {
	if !s.wechatReady(w) {
		return nil, false
	}
	p, ok := s.wechatPay.(WechatRefundProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "微信原路退款尚未配置")
		return nil, false
	}
	return p, true
}

func (s *Server) registerWechatRefundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/wallet/wechat-refunds", s.wechatRefundWallet)
	mux.HandleFunc("POST /api/v1/wallet/wechat-refunds", s.wechatRefundCreate)
	mux.HandleFunc("POST /api/v1/wallet/wechat-refunds/{refundID}/query", s.wechatRefundQuery)
	mux.HandleFunc("POST /api/v1/payments/wechat/refund-notify", s.wechatRefundNotify)
	mux.HandleFunc("GET /api/v1/finance/wechat-refunds", s.staffWechatRefunds)
	mux.HandleFunc("POST /api/v1/finance/wechat-refunds/{refundID}/query", s.staffWechatRefundQuery)
}

func (s *Server) staffWechatRefunds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	items, err := s.store.ListStaffWechatCashRefunds(r.Context())
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) staffWechatRefundQuery(w http.ResponseWriter, r *http.Request) {
	if !s.paymentSameOrigin(w, r) {
		return
	}
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	p, ok := s.refundProvider(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("refundID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "退回记录编号无效")
		return
	}
	tenant, err := s.store.WechatCashRefundTenant(r.Context(), id)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	item, err := s.store.GetWechatCashRefund(r.Context(), tenant, id)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	for _, i := range item.Items {
		if r.Context().Err() != nil {
			break
		}
		s.reconcileWechatRefundItem(r.Context(), p, i.ID)
	}
	item, err = s.store.GetWechatCashRefund(r.Context(), tenant, id)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) writeWechatRefundError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "余额退回记录不存在")
	case errors.Is(err, appdb.ErrWechatRechargeInput):
		writeError(w, http.StatusBadRequest, "请输入正确的退回金额，最多两位小数；提交标识无效请刷新页面")
	case errors.Is(err, appdb.ErrWechatRefundUnavailable):
		writeError(w, http.StatusConflict, "金额超过当前可退本金，或原充值仍有退回处理中；请刷新后重试（同笔充值的两次退回需间隔至少一分钟）")
	case errors.Is(err, appdb.ErrWechatRefundConflict):
		writeError(w, http.StatusConflict, "金额或可退本金已变化，请刷新后核对；重复请求请使用原提交金额")
	case errors.Is(err, appdb.ErrWechatRefundHistory):
		writeError(w, http.StatusConflict, "充值来源记录需人工核对，请联系客服；未发起新退款")
	default:
		writeError(w, http.StatusServiceUnavailable, "余额退回状态暂未确认，请刷新记录核对，不要重复发起")
	}
}

func (s *Server) wechatRefundWallet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	appID, mchID := "", ""
	if s.wechatPay != nil {
		appID = s.wechatPay.AppID()
		mchID = s.wechatPay.MchID()
	}
	result, err := s.store.GetWechatRefundWallet(r.Context(), *actor.TenantID, appID, mchID)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) wechatRefundCreate(w http.ResponseWriter, r *http.Request) {
	if !s.paymentSameOrigin(w, r) {
		return
	}
	provider, ok := s.refundProvider(w)
	if !ok {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	var input struct {
		AmountCents uint64 `json:"amount_cents"`
		Key         string `json:"idempotency_key"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "余额退回参数无效")
		return
	}
	if err := appdb.ValidateWechatRechargeInput(input.AmountCents, input.Key); err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	if saved, err := s.store.FindWechatCashRefund(r.Context(), *actor.TenantID, input.AmountCents, input.Key); err == nil {
		writeJSON(w, http.StatusOK, saved)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.writeWechatRefundError(w, err)
		return
	}
	plan, err := s.store.PlanWechatCashRefund(r.Context(), *actor.TenantID, input.AmountCents, s.wechatPay.AppID(), s.wechatPay.MchID())
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	verified := []model.WechatRefundPaymentVerification{}
	for _, i := range plan {
		v, e := s.wechatPay.Query(r.Context(), i.PaymentNo)
		if e != nil {
			writeError(w, http.StatusBadGateway, "原充值支付结果暂不可核验，尚未冻结或发起退款，请稍后再试")
			return
		}
		// Partial refunds change the original transaction state to REFUND.
		if (v.TradeState != "SUCCESS" && v.TradeState != "REFUND") || !v.PayerAmountKnown || v.PayerAmountCents != v.AmountCents {
			writeError(w, http.StatusConflict, "此充值含支付优惠或付款状态需核对，请联系客服办理；尚未发起退款")
			return
		}
		verified = append(verified, model.WechatRefundPaymentVerification{PaymentNo: v.OutTradeNo, TransactionID: v.TransactionID, AppID: v.AppID, MchID: v.MchID, OpenID: v.PayerOpenID, TotalCents: v.AmountCents, PayerCents: v.PayerAmountCents, PayerAmountKnown: v.PayerAmountKnown, SuccessTime: v.SuccessTime})
	}
	item, err := s.store.CreateWechatCashRefund(r.Context(), *actor.TenantID, actor.UserID, input.AmountCents, input.Key, s.wechatPay.AppID(), s.wechatPay.MchID(), verified)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	// Submit outside the transaction. A timeout/disconnect is NOT a failure:
	// the durable queued request remains frozen and is recovered by the worker.
	for _, i := range item.Items {
		if r.Context().Err() != nil {
			break
		}
		s.reconcileWechatRefundItem(r.Context(), provider, i.ID)
	}
	item, err = s.store.GetWechatCashRefund(r.Context(), *actor.TenantID, item.ID)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, item)
}

func (s *Server) wechatRefundQuery(w http.ResponseWriter, r *http.Request) {
	if !s.paymentSameOrigin(w, r) {
		return
	}
	provider, ok := s.refundProvider(w)
	if !ok {
		return
	}
	actor, ok := s.requireCustomerShopActor(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("refundID"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "退回记录编号无效")
		return
	}
	item, err := s.store.GetWechatCashRefund(r.Context(), *actor.TenantID, id)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	for _, i := range item.Items {
		if r.Context().Err() != nil {
			break
		}
		s.reconcileWechatRefundItem(r.Context(), provider, i.ID)
	}
	item, err = s.store.GetWechatCashRefund(r.Context(), *actor.TenantID, id)
	if err != nil {
		s.writeWechatRefundError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) reconcileWechatRefundItem(ctx context.Context, p WechatRefundProvider, id int64) {
	i, err := s.store.ClaimWechatRefundItem(ctx, id)
	if err != nil {
		return
	}
	// Reject configuration changes before contacting any other merchant account.
	if i.AppID != s.wechatPay.AppID() || i.MchID != s.wechatPay.MchID() {
		s.store.DeferWechatRefundItem(ctx, i)
		return
	}
	var result model.WechatRefundResult
	created := false
	if i.Status == "queued" && i.Attempts == 1 {
		created = true
		err = s.store.MarkWechatRefundSubmission(ctx, i)
		if err == nil {
			result, err = p.CreateRefund(ctx, i)
		}
	} else {
		result, err = p.QueryRefund(ctx, i.RefundNo)
		if err != nil && i.Status == "queued" && wechatpay.IsRefundNotExist(err) {
			created = true
			err = s.store.MarkWechatRefundSubmission(ctx, i)
			if err == nil {
				result, err = p.CreateRefund(ctx, i)
			}
		}
	}
	if err == nil && created {
		// Acceptance is not settlement, even if the create response reports
		// SUCCESS. Only a subsequent signed callback or verified query closes
		// the frozen amount. A concurrent final callback cannot be regressed.
		result.Status = "PROCESSING"
		result.SuccessTime = time.Time{}
	}
	if err == nil {
		err = s.store.CompleteVerifiedWechatRefund(ctx, result)
	}
	if err != nil {
		// Never log API errors/decrypted payloads/payer details or release money
		// on a transport error. Keep the SAME provider refund number on retries.
		log.Printf("wechat refund awaiting verification: item=%d", i.ID)
		s.store.DeferWechatRefundItem(ctx, i)
	}
}

func (s *Server) wechatRefundNotify(w http.ResponseWriter, r *http.Request) {
	p, ok := s.refundProvider(w)
	if !ok {
		return
	}
	result, err := p.ParseRefundNotification(r.Context(), r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid refund notification")
		return
	}
	if err = s.store.CompleteVerifiedWechatRefund(r.Context(), result); err != nil {
		writeError(w, http.StatusInternalServerError, "refund settlement unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) RunWechatRefundReconciliation(ctx context.Context) {
	p, ok := s.wechatPay.(WechatRefundProvider)
	if !ok || !s.wechatPay.Enabled() {
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
		batch, cancel := context.WithTimeout(ctx, 45*time.Second)
		ids, err := s.store.ListDueWechatRefundItems(batch)
		if err == nil {
			for _, id := range ids {
				if batch.Err() != nil {
					break
				}
				s.reconcileWechatRefundItem(batch, p, id)
			}
		} else {
			log.Print("wechat refund reconciliation database unavailable")
		}
		cancel()
	}
}

var _ WechatRefundProvider = (*wechatpay.Service)(nil)
