package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/model"
)

type internalAgentExecuteActionInput struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type internalAgentExecuteRequest struct {
	Action internalAgentExecuteActionInput `json:"action"`
}

func (s *Server) internalAgentExecuteAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireInternalAgentActor(s, w, r)
	if !ok {
		return
	}
	var input internalAgentExecuteRequest
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "智能体确认动作格式错误")
		return
	}
	actionType := strings.TrimSpace(input.Action.Type)
	if actionType == "" || len(input.Action.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "智能体确认动作不完整")
		return
	}

	access, err := s.staffAccessForActor(r, actor)
	if err != nil {
		writeError(w, http.StatusForbidden, "当前账号没有可用的内部员工权限")
		return
	}
	capabilities := systemAgentCapabilities(access)
	modelAction := normalizeSystemAgentAction(actionType)
	requiredPermission := internalAgentActionRequiredPermission(modelAction)
	if requiredPermission == "" {
		writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
			State:        agentStateFailed,
			Code:         "unsupported_action",
			Reply:        "这个动作还没有接入统一执行器，我不会绕过正式业务接口直接修改数据。",
			Capabilities: capabilities,
		})
		return
	}
	if !staffHasPermission(access, requiredPermission) {
		s.writeInternalAgentPermissionDenied(
			w,
			r,
			actor,
			capabilities,
			modelAction,
			requiredPermission,
			internalAgentPermissionReply(requiredPermission),
		)
		return
	}

	switch modelAction {
	case systemAgentActionCreateStaffEmployee:
		s.executeInternalAgentCreateEmployee(w, r, actor, access, capabilities, input.Action.Payload)
	case systemAgentActionCreateMarketing:
		s.executeInternalAgentCreateMarketing(w, r, actor, capabilities, input.Action.Payload)
	default:
		writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
			State:        agentStateFailed,
			Code:         "unsupported_action",
			Reply:        "这个动作还没有接入统一执行器。",
			Capabilities: capabilities,
		})
	}
}

func (s *Server) executeInternalAgentCreateEmployee(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	access model.StaffAccessContext,
	capabilities []string,
	payload json.RawMessage,
) {
	var input systemAgentCreateEmployeePayload
	if err := json.Unmarshal(payload, &input); err != nil {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "invalid_payload", "员工执行参数格式错误。")
		return
	}
	if input.PrimaryGroupID <= 0 || len(input.RoleIDs) == 0 ||
		strings.TrimSpace(input.Username) == "" || strings.TrimSpace(input.DisplayName) == "" ||
		strings.TrimSpace(input.Phone) == "" || strings.TrimSpace(input.Province) == "" ||
		strings.TrimSpace(input.City) == "" || strings.TrimSpace(input.District) == "" {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "incomplete_payload", "员工执行参数不完整，请重新让智能体整理一次。")
		return
	}
	employeeNo, validEmployeeNo := normalizeStaffEmployeeNo(input.EmployeeNo)
	if !validEmployeeNo {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "invalid_employee_no", "员工编号只填写 1-6 位数字。")
		return
	}
	input.EmployeeNo = employeeNo
	input.DeliveryMethod = normalizeDeliveryMethod(input.DeliveryMethod)
	email, emailOK := normalizeCredentialEmail(input.Email)
	if !emailOK {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "invalid_email", "邮箱格式不正确。")
		return
	}
	input.Email = email
	if input.DeliveryMethod == "email" && input.Email == "" {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "missing_email", "选择邮件发送时必须填写邮箱。")
		return
	}
	if !canManageStaffGroup(access, "staff.employee.create", input.PrimaryGroupID) {
		s.writeInternalAgentPermissionDenied(
			w, r, actor, capabilities, modelActionCreateStaffEmployeeScope,
			"staff.employee.create", "你当前不能向这个部门添加员工。",
		)
		return
	}

	allRoles, err := s.store.ListStaffRoles(r.Context(), 0)
	if err != nil {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "role_lookup_failed", "读取岗位信息失败，请稍后重试。")
		return
	}
	roleByID := make(map[int64]model.StaffRoleSummary, len(allRoles))
	for _, role := range allRoles {
		roleByID[role.ID] = role
	}
	allowManagerRole := access.IsSuperAdmin || staffPermissionScope(access, "staff.employee.role_assign") == "all_internal"
	for _, roleID := range input.RoleIDs {
		role, exists := roleByID[roleID]
		if !exists || role.Status != "active" {
			s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "invalid_role", "选择的岗位不存在或已停用。")
			return
		}
		if role.GroupID != input.PrimaryGroupID && !canManageStaffGroup(access, "staff.employee.role_assign", role.GroupID) {
			s.writeInternalAgentPermissionDenied(
				w, r, actor, capabilities, "staff.employee.role_assign.scope",
				"staff.employee.role_assign", "你当前不能给该员工增加这个部门的兼任职责。",
			)
			return
		}
		if role.IsGroupManager && !allowManagerRole {
			s.writeInternalAgentPermissionDenied(
				w, r, actor, capabilities, "staff.employee.role_assign.manager",
				"staff.employee.role_assign", "部门负责人岗位只能由具备全局员工管理权限的人员分配。",
			)
			return
		}
	}

	initialPassword, err := auth.GenerateInitialPassword()
	if err != nil {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "password_generation_failed", "生成初始密码失败。")
		return
	}
	passwordHash, err := auth.HashPassword(initialPassword)
	if err != nil {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "password_hash_failed", "初始密码加密失败。")
		return
	}
	item, err := s.store.CreateStaffEmployee(
		r.Context(), input.EmployeeNo, input.PrimaryGroupID, input.RoleIDs,
		input.Username, input.DisplayName, input.Phone, input.Email,
		input.Province, input.City, input.District, passwordHash,
	)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate:") {
			s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "employee_conflict", "员工编号或登录账号已存在。")
			return
		}
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateStaffEmployee, "employee_create_failed", "创建员工失败，请检查输入数据。")
		return
	}
	credential := s.deliverInitialCredential(
		input.DeliveryMethod, input.Email, input.DisplayName, input.Username,
		initialPassword, "internal",
	)
	s.auditInternalAgentCommand(
		r,
		actor,
		systemAgentActionCreateStaffEmployee,
		"succeeded",
		"staff_employee",
		item.EmployeeNo,
		item.DisplayName,
		map[string]any{"user_id": item.UserID, "username": item.Username},
	)
	writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
		State:        agentStateSucceeded,
		Code:         "staff_employee_created",
		Reply:        "已完成新增员工“" + item.DisplayName + "”（" + item.EmployeeNo + "）。首次登录需要修改初始密码。",
		Capabilities: capabilities,
		Data:         item,
		Credential:   &credential,
	})
}

const modelActionCreateStaffEmployeeScope = "staff.employee.create.scope"

func (s *Server) executeInternalAgentCreateMarketing(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	capabilities []string,
	payload json.RawMessage,
) {
	var input model.MarketingCampaignInput
	if err := json.Unmarshal(payload, &input); err != nil {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateMarketing, "invalid_payload", "营销活动执行参数格式错误。")
		return
	}
	input.Status = "draft"
	input.SortOrder = 10
	input.PricingRule = "floor_yuan"
	input.DisplayLocations = []string{"backoffice"}
	if strings.TrimSpace(input.Code) == "" || strings.TrimSpace(input.Name) == "" || len(input.Items) == 0 {
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateMarketing, "incomplete_payload", "营销活动执行参数不完整，请继续补充后再确认。")
		return
	}
	item, err := s.store.CreateMarketingCampaign(r.Context(), actor.UserID, input)
	if err != nil {
		if isDuplicateDBError(err) {
			s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateMarketing, "marketing_conflict", "营销活动编码已存在。")
			return
		}
		s.writeInternalAgentExecutionFailed(w, r, actor, capabilities, systemAgentActionCreateMarketing, "marketing_create_failed", "创建营销活动失败："+err.Error())
		return
	}
	s.auditInternalAgentCommand(
		r,
		actor,
		systemAgentActionCreateMarketing,
		"succeeded",
		"marketing_campaign",
		item.Code,
		item.Name,
		map[string]any{"campaign_id": item.ID},
	)
	writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
		State:        agentStateSucceeded,
		Code:         "marketing_campaign_created",
		Reply:        "已创建营销活动草稿“" + item.Name + "”。默认仅后台展示，不会自动出现在终端商城或会员中心。",
		Capabilities: capabilities,
		Data:         item,
	})
}

func (s *Server) writeInternalAgentExecutionFailed(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	capabilities []string,
	action string,
	code string,
	reply string,
) {
	s.auditInternalAgentCommand(
		r,
		actor,
		action,
		"failed",
		"agent_command",
		"",
		strings.ToLower(strings.TrimSpace(action)),
		map[string]any{"code": code, "reply": reply},
	)
	writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
		State:        agentStateFailed,
		Code:         code,
		Reply:        reply,
		Capabilities: capabilities,
	})
}
