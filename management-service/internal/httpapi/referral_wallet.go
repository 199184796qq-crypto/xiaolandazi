package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) customerReferralWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "当前账号不能访问返佣钱包")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	item, err := s.store.GetCustomerReferralWalletDashboard(r.Context(), tenantID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取返佣钱包失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) customerCreateReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if actor.Role != "customer" {
		writeError(w, http.StatusForbidden, "当前账号不能发起返佣提现")
		return
	}
	tenantID, ok := actorTenantID(w, actor)
	if !ok {
		return
	}
	var input model.CreateWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateCustomerReferralWithdrawal(
		r.Context(),
		tenantID,
		actor.UserID,
		input.AmountCents,
	)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrReferralWithdrawalAmount):
			writeError(w, http.StatusBadRequest, "提现金额必须大于 0")
		case errors.Is(err, storedb.ErrReferralWithdrawalInsufficient):
			writeError(w, http.StatusConflict, "可提现返佣余额不足")
		default:
			writeError(w, http.StatusInternalServerError, "提交返佣提现失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) financeListReferralWithdrawals(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	items, err := s.store.ListReferralWithdrawals(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("status")),
		200,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取返佣提现申请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func referralWithdrawalPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("withdrawalID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "提现申请编号无效")
		return 0, false
	}
	return value, true
}

func (s *Server) financeApproveReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := referralWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.ApproveReferralWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrFinanceDistinctReviewer):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, storedb.ErrFinanceReviewPolicyUnavailable):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, storedb.ErrReferralWithdrawalState):
			writeError(w, http.StatusConflict, "提现申请不存在或当前状态不能审核")
		default:
			writeError(w, http.StatusInternalServerError, "审核返佣提现失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeRejectReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := referralWithdrawalPathID(w, r)
	if !ok {
		return
	}
	var input model.RejectWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.RejectReferralWithdrawal(
		r.Context(),
		withdrawalID,
		actor.UserID,
		strings.TrimSpace(input.Reason),
	)
	if err != nil {
		if errors.Is(err, storedb.ErrReferralWithdrawalState) {
			writeError(w, http.StatusConflict, "提现申请不存在或当前状态不能驳回")
			return
		}
		writeError(w, http.StatusInternalServerError, "驳回返佣提现失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financePayReferralWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.pay")
	if !ok {
		return
	}
	withdrawalID, ok := referralWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.PayReferralWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		if errors.Is(err, storedb.ErrReferralWithdrawalState) {
			writeError(w, http.StatusConflict, "提现申请尚未审核通过或已经处理")
			return
		}
		writeError(w, http.StatusInternalServerError, "确认返佣提现打款失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
