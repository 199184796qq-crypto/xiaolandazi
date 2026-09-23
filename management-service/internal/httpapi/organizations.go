package httpapi

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"livecompanion/management/internal/auth"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
)

type createAgentRequest struct {
	Code           string `json:"code"`
	Name           string `json:"name"`
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	DeliveryMethod string `json:"delivery_method"`
}

type createAgentCustomerRequest struct {
	Username       string `json:"username"`
	DisplayName    string `json:"display_name"`
	Phone          string `json:"phone"`
	Email          string `json:"email"`
	Province       string `json:"province"`
	City           string `json:"city"`
	District       string `json:"district"`
	DeliveryMethod string `json:"delivery_method"`
}

func (s *Server) adminListAgents(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.requireStaffPermission(
		w,
		r,
		"agent.view_all",
	); !ok {
		return
	}

	items, err := s.store.ListAgents(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取代理列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) adminCreateAgent(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	if !actor.IsPlatformAdmin() {
		writeError(w, http.StatusForbidden, "仅超级系统管理员可创建代理")
		return
	}

	var input createAgentRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

	input.Code = strings.TrimSpace(input.Code)
	input.Name = strings.TrimSpace(input.Name)
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

	if input.Code == "" ||
		input.Name == "" ||
		input.Username == "" ||
		input.DisplayName == "" ||
		input.Phone == "" ||
		input.Province == "" ||
		input.City == "" ||
		input.District == "" {
		writeError(w, http.StatusBadRequest, "代理编码、代理名称、登录账号、管理员名称、联系电话、省、市、区/县不能为空")
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

	item, err := s.store.CreateAgent(
		r.Context(),
		input.Code,
		input.Name,
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
			writeError(w, http.StatusConflict, "代理编码或登录账号已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "创建代理失败")
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

func (s *Server) agentListCustomers(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgent(w, r)
	if !ok {
		return
	}

	items, err := s.store.ListAgentCustomers(r.Context(), *actor.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取终端列表失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) agentCreateCustomer(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.requireAgent(w, r)
	if !ok {
		return
	}

	var input createAgentCustomerRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}

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

	if input.Username == "" ||
		input.DisplayName == "" ||
		input.Phone == "" ||
		input.Province == "" ||
		input.City == "" ||
		input.District == "" {
		writeError(w, http.StatusBadRequest, "登录账号、终端名称、联系电话、省、市、区/县不能为空")
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

	user, err := s.store.CreateAgentCustomer(
		r.Context(),
		*actor.TenantID,
		actor.UserID,
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
		switch {
		case errors.Is(err, appdb.ErrInsufficientResource):
			writeError(w, http.StatusConflict, "代理终端名额不足，请先联系超级系统管理员增加终端名额")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusForbidden, "代理组织不可用")
		case strings.Contains(err.Error(), "duplicate:"):
			writeError(w, http.StatusConflict, "登录账号已存在")
		default:
			writeError(w, http.StatusInternalServerError, "创建终端失败")
		}
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
		"user_id":      user.ID,
		"tenant_id":    user.TenantID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"email":        input.Email,
		"status":       user.Status,
		"credential":   credential,
	})
}

func (s *Server) requireAgent(
	w http.ResponseWriter,
	r *http.Request,
) (model.Actor, bool) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return model.Actor{}, false
	}
	if !actor.IsAgentAdmin() || actor.TenantID == nil {
		writeError(w, http.StatusForbidden, "仅代理管理员可执行此操作")
		return model.Actor{}, false
	}
	return actor, true
}
