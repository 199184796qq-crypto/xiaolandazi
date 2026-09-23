package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/auth"
)

type createSalesStaffRequest struct {
	EmployeeCode   string `json:"employee_code"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	DeliveryMethod string `json:"delivery_method"`
}

type assignCustomerSalesRequest struct {
	SalesStaffID int64  `json:"sales_staff_id"`
	Reason       string `json:"reason"`
}

func (s *Server) adminListSalesStaff(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"sales.view_all",
	); !ok {
		return
	}

	items, err := s.store.ListSalesStaff(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取销售列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminCreateSalesStaff(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可创建销售账号")
		return
	}

	var input createSalesStaffRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	input.EmployeeCode = strings.TrimSpace(input.EmployeeCode)
	input.Username = strings.TrimSpace(input.Username)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Phone = strings.TrimSpace(input.Phone)
	input.Province = strings.TrimSpace(input.Province)
	input.City = strings.TrimSpace(input.City)
	input.District = strings.TrimSpace(input.District)
	input.DeliveryMethod = normalizeDeliveryMethod(input.DeliveryMethod)

	email, emailOK := normalizeCredentialEmail(input.Email)
	if !emailOK {
		writeError(w, http.StatusBadRequest, "邮箱格式不正确")
		return
	}
	input.Email = email

	if input.EmployeeCode == "" ||
		input.Username == "" ||
		input.DisplayName == "" ||
		input.Phone == "" ||
		input.Province == "" ||
		input.City == "" ||
		input.District == "" {
		writeError(w, http.StatusBadRequest, "员工编号、登录账号、姓名、联系电话、省、市、区/县不能为空")
		return
	}
	if input.DeliveryMethod == "email" && input.Email == "" {
		writeError(w, http.StatusBadRequest, "选择邮件发送时必须填写邮箱")
		return
	}

	initialPassword, err := auth.GenerateInitialPassword()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成初始密码失败")
		return
	}
	passwordHash, err := auth.HashPassword(initialPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "初始密码加密失败")
		return
	}

	item, err := s.store.CreateSalesStaff(
		r.Context(),
		input.EmployeeCode,
		input.Username,
		input.DisplayName,
		input.Phone,
		input.Email,
		input.Province,
		input.City,
		input.District,
		passwordHash,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			writeError(w, http.StatusConflict, "员工编号或登录账号已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建销售账号失败")
		return
	}

	credential := s.deliverInitialCredential(
		input.DeliveryMethod,
		input.Email,
		input.DisplayName,
		input.Username,
		initialPassword,
	)

	writeJSON(w, http.StatusCreated, map[string]any{
		"item":       item,
		"credential": credential,
	})
}

func (s *Server) adminAssignCustomerSales(w http.ResponseWriter, r *http.Request) {
	actor, _, ok := s.requireStaffPermission(
		w,
		r,
		"sales.assignment.manage",
	)
	if !ok {
		return
	}

	tenantID, ok := parsePositivePathID(w, r.PathValue("tenantID"))
	if !ok {
		return
	}

	var input assignCustomerSalesRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		input.Reason = "system assignment"
	}

	if input.SalesStaffID <= 0 {
		if err := s.store.ClearCustomerSalesAssignment(
			r.Context(),
			tenantID,
			actor.UserID,
			input.Reason,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "取消销售分配失败")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.store.AssignCustomerToSales(
		r.Context(),
		tenantID,
		input.SalesStaffID,
		actor.UserID,
		input.Reason,
	); err != nil {
		switch {
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "终端或销售不存在")
		case strings.Contains(err.Error(), "platform direct"):
			writeError(w, http.StatusConflict, "内部销售只能分配平台直营终端，不能分配代理终端")
		default:
			writeError(w, http.StatusInternalServerError, "分配终端销售失败")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) salesListCustomers(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsSalesStaff() {
		writeError(w, http.StatusForbidden, "仅内部销售可查看销售终端")
		return
	}

	items, err := s.store.ListSalesCustomersByUser(r.Context(), actor.UserID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
			return
		}
		writeError(w, http.StatusInternalServerError, "读取销售终端失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
