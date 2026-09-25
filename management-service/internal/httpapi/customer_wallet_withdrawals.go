package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type customerWalletWithdrawalInput struct {
	AccountType string `json:"account_type"`
	AmountCents int64  `json:"amount_cents"`
}

func (s *Server) customerWalletWithdrawals(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "当前账号不能访问钱包提现")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	items, err := s.store.ListCustomerWalletWithdrawals(
		r.Context(),
		tenantID,
		strings.TrimSpace(r.URL.Query().Get("account_type")),
		200,
	)
	if err != nil {
		if errors.Is(err, storedb.ErrCustomerWithdrawalAccount) {
			writeError(w, http.StatusBadRequest, "提现账户类型无效")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取提现记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) customerCreateWalletWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "当前账号不能发起提现")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	var input customerWalletWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateCustomerWalletWithdrawal(
		r.Context(),
		tenantID,
		actor.UserID,
		strings.TrimSpace(input.AccountType),
		input.AmountCents,
	)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrCustomerWithdrawalAccount):
			writeError(w, http.StatusBadRequest, "提现账户类型无效")
		case errors.Is(err, storedb.ErrCustomerWithdrawalAmount):
			writeError(w, http.StatusBadRequest, "提现金额必须大于 0")
		case errors.Is(err, storedb.ErrCustomerWithdrawalInsufficient):
			writeError(w, http.StatusConflict, "可提现余额不足")
		default:
			writeError(w, http.StatusInternalServerError, "提交提现申请失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) financeListCustomerWalletWithdrawals(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	items, err := s.store.ListAllCustomerWalletWithdrawals(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("status")),
		200,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取客户提现申请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func customerWithdrawalPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("withdrawalID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "提现申请编号无效")
		return 0, false
	}
	return value, true
}

func (s *Server) financeApproveCustomerWalletWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := customerWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.ApproveCustomerWalletWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrFinanceDistinctReviewer):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, storedb.ErrFinanceReviewPolicyUnavailable):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, storedb.ErrCustomerWithdrawalState):
			writeError(w, http.StatusConflict, "提现申请不存在或当前状态不能审核")
		default:
			writeError(w, http.StatusInternalServerError, "审核提现失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeRejectCustomerWalletWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := customerWithdrawalPathID(w, r)
	if !ok {
		return
	}
	var input model.RejectWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.RejectCustomerWalletWithdrawal(
		r.Context(),
		withdrawalID,
		actor.UserID,
		strings.TrimSpace(input.Reason),
	)
	if err != nil {
		if errors.Is(err, storedb.ErrCustomerWithdrawalState) {
			writeError(w, http.StatusConflict, "提现申请不存在或当前状态不能驳回")
			return
		}
		writeError(w, http.StatusInternalServerError, "驳回提现失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financePayCustomerWalletWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.pay")
	if !ok {
		return
	}
	withdrawalID, ok := customerWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.PayCustomerWalletWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		if errors.Is(err, storedb.ErrCustomerWithdrawalState) {
			writeError(w, http.StatusConflict, "提现申请尚未审核通过或已经处理")
			return
		}
		writeError(w, http.StatusInternalServerError, "确认提现打款失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
