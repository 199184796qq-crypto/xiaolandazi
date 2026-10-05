package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	storedb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

func (s *Server) salesCommissionWallet(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "当前账号不能访问销售提成钱包")
		return
	}
	tenantID, staffErr := s.store.CommerceSalesStaffID(r.Context(), actor.UserID)
	if staffErr != nil {
		writeError(w, http.StatusForbidden, "销售人员档案不存在或已停用")
		return
	}
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}
	item, err := s.store.GetSalesCommissionWalletDashboard(r.Context(), tenantID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取销售提成钱包失败")
		return
	}
	details, err := s.store.ListCommerceEarningDetails(r.Context(), "sales_staff", tenantID, r.URL.Query().Get("period"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"wallet": item.Wallet, "ledger": item.Ledger, "withdrawals": item.Withdrawals, "earnings": details})
}

func (s *Server) salesCreateCommissionWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "当前账号不能发起销售提成提现")
		return
	}
	tenantID, staffErr := s.store.CommerceSalesStaffID(r.Context(), actor.UserID)
	if staffErr != nil {
		writeError(w, http.StatusForbidden, "销售人员档案不存在或已停用")
		return
	}
	var input model.CreateWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.CreateSalesCommissionWithdrawal(
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
			writeError(w, http.StatusConflict, "可提现销售提成余额不足")
		default:
			writeError(w, http.StatusInternalServerError, "提交销售提成提现失败")
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) financeListSalesCommissionWithdrawals(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve"); !ok {
		return
	}
	items, err := s.store.ListSalesCommissionWithdrawals(
		r.Context(),
		strings.TrimSpace(r.URL.Query().Get("status")),
		200,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取销售提成提现申请失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func salesCommissionWithdrawalPathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	value, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("withdrawalID")), 10, 64)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "提现申请编号无效")
		return 0, false
	}
	return value, true
}

func (s *Server) financeApproveSalesCommissionWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := salesCommissionWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.ApproveSalesCommissionWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		switch {
		case errors.Is(err, storedb.ErrFinanceDistinctReviewer):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, storedb.ErrFinanceReviewPolicyUnavailable):
			writeError(w, http.StatusServiceUnavailable, err.Error())
		case errors.Is(err, storedb.ErrReferralWithdrawalState):
			writeError(w, http.StatusConflict, "提现申请不存在或当前状态不能审核")
		default:
			writeError(w, http.StatusInternalServerError, "审核销售提成提现失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financeRejectSalesCommissionWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.approve")
	if !ok {
		return
	}
	withdrawalID, ok := salesCommissionWithdrawalPathID(w, r)
	if !ok {
		return
	}
	var input model.RejectWithdrawalInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	item, err := s.store.RejectSalesCommissionWithdrawal(
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
		writeError(w, http.StatusInternalServerError, "驳回销售提成提现失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) financePaySalesCommissionWithdrawal(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(w, r, "finance.settlement.pay")
	if !ok {
		return
	}
	withdrawalID, ok := salesCommissionWithdrawalPathID(w, r)
	if !ok {
		return
	}
	item, err := s.store.PaySalesCommissionWithdrawal(r.Context(), withdrawalID, actor.UserID)
	if err != nil {
		if errors.Is(err, storedb.ErrReferralWithdrawalState) {
			writeError(w, http.StatusConflict, "提现申请尚未审核通过或已经处理")
			return
		}
		if errors.Is(err, storedb.ErrReferralWithdrawalInsufficient) {
			writeError(w, http.StatusConflict, "存在退款应扣欠额，请先抵扣或完成财务处理后打款")
			return
		}
		writeError(w, http.StatusInternalServerError, "确认销售提成提现打款失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
