package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"

	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func writeBeanError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storedb.ErrBeanEconomyDisabled):
		writeError(w, http.StatusConflict, "小蓝豆功能尚未启用")
	case errors.Is(err, storedb.ErrBeanPricingDisabled):
		writeError(w, http.StatusConflict, "该行为尚未启用小蓝豆计费")
	case errors.Is(err, storedb.ErrBeanInsufficientBalance):
		writeError(w, http.StatusConflict, "小蓝豆余额不足")
	case errors.Is(err, storedb.ErrBeanCashInsufficient):
		writeError(w, http.StatusConflict, "现金余额不足")
	case errors.Is(err, storedb.ErrBeanConflict):
		writeError(w, http.StatusConflict, "数据已变化，请刷新后重试")
	case errors.Is(err, storedb.ErrBeanInvalidInput):
		writeError(w, http.StatusBadRequest, "小蓝豆参数无效")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "小蓝豆记录不存在")
	default:
		writeError(w, http.StatusInternalServerError, "小蓝豆业务处理失败")
	}
}

func (s *Server) commercialBeansDashboard(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "commercial.beans.view"); !ok {
		return
	}
	item, err := s.store.BeanCommercialDashboard(r.Context())
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialUpdateBeanSettings(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.beans.manage")
	if !ok {
		return
	}
	var input model.BeanCommerceSettingsInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.UpdateBeanCommerceSettings(r.Context(), actor.UserID, input)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) commercialCreateBeanRule(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.beans.manage")
	if !ok {
		return
	}
	var input model.BeanPricingRuleInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.SaveBeanPricingRule(r.Context(), actor.UserID, 0, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "行为编码已经存在")
			return
		}
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func beanPathID(w http.ResponseWriter, r *http.Request, key string) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue(key)), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "编号无效")
		return 0, false
	}
	return value, true
}

func (s *Server) commercialUpdateBeanRule(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "commercial.beans.manage")
	if !ok {
		return
	}
	ruleID, ok := beanPathID(w, r, "ruleID")
	if !ok {
		return
	}
	var input model.BeanPricingRuleInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.SaveBeanPricingRule(r.Context(), actor.UserID, ruleID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			writeError(w, http.StatusConflict, "行为编码已经存在")
			return
		}
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) customerBeanWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可访问小蓝豆钱包")
		return
	}
	item, err := s.store.CustomerBeanWallet(r.Context(), *actor.TenantID, 100)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type beanPurchaseRequest struct {
	AmountCents    uint64 `json:"amount_cents"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (s *Server) customerPurchaseBeans(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可购买小蓝豆")
		return
	}
	var input beanPurchaseRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.PurchaseCustomerBeans(r.Context(), *actor.TenantID, actor.UserID, input.AmountCents, input.IdempotencyKey)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

type beanQuoteRequest struct {
	ActionCode string `json:"action_code"`
	Units      uint64 `json:"units"`
}

func (s *Server) customerQuoteBeans(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅终端账号可获取小蓝豆报价")
		return
	}
	var input beanQuoteRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.QuoteBeanCharge(r.Context(), input.ActionCode, input.Units)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) staffOwnBeanWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部员工可访问员工小蓝豆钱包")
		return
	}
	item, err := s.store.StaffBeanWallet(r.Context(), actor.UserID, 100)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type beanConversionCreateRequest struct {
	BeanAmount uint64 `json:"bean_amount"`
}

func (s *Server) staffCreateBeanConversion(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsInternalStaff() {
		writeError(w, http.StatusForbidden, "仅内部员工可申请小蓝豆兑付")
		return
	}
	var input beanConversionCreateRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateBeanConversion(r.Context(), actor.UserID, input.BeanAmount)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) financeBeansDashboard(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.beans.view"); !ok {
		return
	}
	item, err := s.store.BeanFinanceDashboard(r.Context())
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

type beanConversionTransitionRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) financeTransitionBeanConversion(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.beans.manage")
	if !ok {
		return
	}
	conversionID, ok := beanPathID(w, r, "conversionID")
	if !ok {
		return
	}
	action := strings.ToLower(strings.TrimSpace(r.PathValue("action")))
	if action != "approve" && action != "reject" && action != "pay" {
		writeError(w, http.StatusBadRequest, "兑付操作无效")
		return
	}
	var input beanConversionTransitionRequest
	if r.ContentLength > 0 {
		if err := readJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "请求格式错误")
			return
		}
	}
	if action == "reject" && strings.TrimSpace(input.Reason) == "" {
		writeError(w, http.StatusBadRequest, "驳回原因必填")
		return
	}
	item, err := s.store.TransitionBeanConversion(r.Context(), conversionID, action, actor.UserID, input.Reason)
	if err != nil {
		writeBeanError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
