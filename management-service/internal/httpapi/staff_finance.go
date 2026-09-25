package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

type staffFinanceOperationRequest struct {
	TenantID      int64  `json:"tenant_id"`
	AmountCents   uint64 `json:"amount_cents"`
	Reason        string `json:"reason"`
	PaymentMethod string `json:"payment_method,omitempty"`
}

type staffAITimeGrantRequest struct {
	OrganizationID  int64  `json:"organization_id"`
	ResourceSeconds int64  `json:"resource_seconds"`
	Reason          string `json:"reason"`
}

func (s *Server) staffFinanceOverview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"finance.dashboard.view",
	); !ok {
		return
	}

	customers, err := s.store.ListStaffFinanceCustomers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取财务终端失败")
		return
	}
	tasks, err := s.store.ListStaffFinanceTasks(r.Context(), 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取财务审批记录失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"customers": customers,
		"tasks":     tasks,
	})
}

func (s *Server) staffFinanceCustomerDashboard(
	w http.ResponseWriter,
	r *http.Request,
) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"finance.dashboard.view",
	); !ok {
		return
	}
	tenantID, ok := staffPathID(w, r, "tenantID")
	if !ok {
		return
	}

	item, err := s.store.GetFinanceDashboard(r.Context(), tenantID, 80)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端财务信息失败")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) staffFinanceCreateRecharge(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"finance.recharge.create",
	)
	if !ok {
		return
	}

	input, ok := readStaffFinanceOperationInput(w, r)
	if !ok {
		return
	}
	if input.PaymentMethod == "" {
		input.PaymentMethod = "manual"
	}

	result, err := s.store.CreateStaffFinanceRecharge(
		r.Context(),
		input.TenantID,
		input.AmountCents,
		input.PaymentMethod,
		input.Reason,
		actor.UserID,
	)
	if err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) staffFinanceCreateRefund(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"finance.refund.create",
	)
	if !ok {
		return
	}

	input, ok := readStaffFinanceOperationInput(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		writeError(w, http.StatusBadRequest, "退款原因不能为空")
		return
	}

	result, err := s.store.CreateStaffFinanceRefund(
		r.Context(),
		input.TenantID,
		input.AmountCents,
		input.Reason,
		actor.UserID,
	)
	if err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) staffFinanceCreateReward(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"finance.reward.grant",
	)
	if !ok {
		return
	}

	input, ok := readStaffFinanceOperationInput(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(input.Reason) == "" {
		writeError(w, http.StatusBadRequest, "奖励发放原因不能为空")
		return
	}

	result, err := s.store.CreateStaffFinanceReward(
		r.Context(),
		input.TenantID,
		input.AmountCents,
		input.Reason,
		actor.UserID,
	)
	if err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) commercialCreateAITimeGrantRequest(
	w http.ResponseWriter,
	r *http.Request,
) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"commercial.ai_time.request",
	)
	if !ok {
		return
	}

	var input staffAITimeGrantRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.OrganizationID <= 0 {
		writeError(w, http.StatusBadRequest, "目标组织无效")
		return
	}
	if input.ResourceSeconds <= 0 {
		writeError(w, http.StatusBadRequest, "增加时长必须大于 0")
		return
	}
	if input.ResourceSeconds > 100000*3600 {
		writeError(w, http.StatusBadRequest, "单次增加时长超过系统上限")
		return
	}
	if input.Reason == "" {
		writeError(w, http.StatusBadRequest, "申请原因不能为空")
		return
	}

	result, err := s.store.CreateStaffAITimeGrantRequest(
		r.Context(),
		input.OrganizationID,
		input.ResourceSeconds,
		input.Reason,
		actor.UserID,
	)
	if err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) staffFinanceApproveTask(
	w http.ResponseWriter,
	r *http.Request,
) {
	taskID, ok := staffPathID(w, r, "taskID")
	if !ok {
		return
	}
	task, err := s.store.GetStaffFinanceTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "审批任务不存在")
		return
	}

	permission := financeApprovalPermission(task.OperationCode)
	if permission == "" {
		writeError(w, http.StatusBadRequest, "不支持的财务审批类型")
		return
	}

	actor, _, ok := s.requireStaffPermission(w, r, permission)
	if !ok {
		return
	}

	if err := s.store.ApproveStaffFinanceTask(
		r.Context(),
		taskID,
		actor.UserID,
	); err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) staffFinanceRejectTask(
	w http.ResponseWriter,
	r *http.Request,
) {
	taskID, ok := staffPathID(w, r, "taskID")
	if !ok {
		return
	}
	task, err := s.store.GetStaffFinanceTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "审批任务不存在")
		return
	}

	permission := financeApprovalPermission(task.OperationCode)
	if permission == "" {
		writeError(w, http.StatusBadRequest, "不支持的财务审批类型")
		return
	}

	actor, _, ok := s.requireStaffPermission(w, r, permission)
	if !ok {
		return
	}

	if err := s.store.RejectStaffFinanceTask(
		r.Context(),
		taskID,
		actor.UserID,
	); err != nil {
		writeStaffFinanceOperationError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func readStaffFinanceOperationInput(
	w http.ResponseWriter,
	r *http.Request,
) (staffFinanceOperationRequest, bool) {
	var input staffFinanceOperationRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return staffFinanceOperationRequest{}, false
	}
	input.Reason = strings.TrimSpace(input.Reason)
	input.PaymentMethod = strings.TrimSpace(input.PaymentMethod)
	if input.TenantID <= 0 {
		writeError(w, http.StatusBadRequest, "终端 ID 无效")
		return staffFinanceOperationRequest{}, false
	}
	if input.AmountCents == 0 {
		writeError(w, http.StatusBadRequest, "金额必须大于 0")
		return staffFinanceOperationRequest{}, false
	}
	if input.AmountCents > 100000000000 {
		writeError(w, http.StatusBadRequest, "金额超过系统单笔上限")
		return staffFinanceOperationRequest{}, false
	}
	return input, true
}

func financeApprovalPermission(operationCode string) string {
	switch operationCode {
	case "finance.recharge":
		return "finance.recharge.approve"
	case "finance.refund":
		return "finance.refund.approve"
	case "finance.reward":
		return "finance.reward.approve"
	case "finance.ai_time_grant":
		return "finance.ai_time.approve"
	default:
		return ""
	}
}

func writeStaffFinanceOperationError(w http.ResponseWriter, err error) {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "insufficient"):
		writeError(w, http.StatusConflict, "终端现金余额不足，无法退款")
	case strings.Contains(message, "经办人与审核人必须不同"):
		writeError(w, http.StatusConflict, err.Error())
	case strings.Contains(message, "财务审核配置读取失败"):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case strings.Contains(message, "not pending"):
		writeError(w, http.StatusConflict, "该审批任务已处理")
	case strings.Contains(message, "ai time"):
		writeError(w, http.StatusBadRequest, "AI 时长申请数据无效")
	case strings.Contains(message, "customer"):
		writeError(w, http.StatusBadRequest, "终端不可用")
	default:
		writeError(w, http.StatusInternalServerError, "财务操作失败")
	}
}

func parsePositiveQueryID(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if parsed < 0 {
		return 0
	}
	return parsed
}

var _ model.StaffFinanceOperationResult
