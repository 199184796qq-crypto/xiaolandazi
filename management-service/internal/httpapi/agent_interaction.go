package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"livecompanion/management/internal/model"
)

const (
	agentProtocolVersion       = "1"
	agentStateResponded        = "responded"
	agentStateClarifying       = "clarifying"
	agentStateReadyToConfirm   = "ready_to_confirm"
	agentStateSucceeded        = "succeeded"
	agentStateFailed           = "failed"
	agentStatePermissionDenied = "permission_denied"
)

type internalAgentPermissionIntent struct {
	Code       string
	Permission string
	Reply      string
}

func writeAgentChatOutput(w http.ResponseWriter, status int, output systemAgentChatOutput) {
	if strings.TrimSpace(output.ProtocolVersion) == "" {
		output.ProtocolVersion = agentProtocolVersion
	}
	if strings.TrimSpace(output.State) == "" {
		switch {
		case output.Action != nil:
			output.State = agentStateReadyToConfirm
		case output.Navigate != nil:
			output.State = agentStateSucceeded
		default:
			output.State = agentStateResponded
		}
	}
	writeJSON(w, status, output)
}

func normalizeAgentPermissionText(message string) string {
	return strings.ToLower(strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"：", "", ":", "", "，", "", ",", "", "。", "", ".", "",
	).Replace(strings.TrimSpace(message)))
}

func containsAny(value string, words ...string) bool {
	for _, word := range words {
		if strings.Contains(value, word) {
			return true
		}
	}
	return false
}

func detectInternalAgentPermissionIntent(message string) (internalAgentPermissionIntent, bool) {
	value := normalizeAgentPermissionText(message)
	if value == "" {
		return internalAgentPermissionIntent{}, false
	}

	if containsAny(value, "新增员工", "添加员工", "创建员工", "录入员工", "招入员工") {
		return internalAgentPermissionIntent{
			Code:       "staff.employee.create",
			Permission: "staff.employee.create",
			Reply:      "你当前没有新增员工权限，不能通过智能体创建员工。请联系有权限的负责人处理。",
		}, true
	}
	if containsAny(value, "分配岗位", "调整岗位", "修改岗位", "设置岗位", "分配角色", "调整角色") && strings.Contains(value, "员工") {
		return internalAgentPermissionIntent{
			Code:       "staff.employee.role_assign",
			Permission: "staff.employee.role_assign",
			Reply:      "你当前没有员工岗位分配权限，不能通过智能体调整岗位或角色。",
		}, true
	}
	if containsAny(value, "查询员工", "查看员工", "员工名单", "员工列表", "多少员工", "员工资料", "查员工") {
		return internalAgentPermissionIntent{
			Code:       "staff.employee.view",
			Permission: "staff.employee.view",
			Reply:      "你当前没有员工查询权限，不能通过智能体查看员工信息。",
		}, true
	}
	if containsAny(value, "查询部门", "查看部门", "部门列表", "有哪些部门", "组织架构") {
		return internalAgentPermissionIntent{
			Code:       "staff.group.view",
			Permission: "staff.group.view",
			Reply:      "你当前没有部门/组织架构查看权限。",
		}, true
	}
	if containsAny(value, "创建营销活动", "新增营销活动", "添加营销活动", "新建营销活动", "创建营销计划", "新增营销计划") {
		return internalAgentPermissionIntent{
			Code:       "commercial.marketing.manage",
			Permission: "commercial.marketing.manage",
			Reply:      "你当前没有营销活动管理权限，不能通过智能体创建或修改营销活动。",
		}, true
	}
	return internalAgentPermissionIntent{}, false
}

func internalAgentActionRequiredPermission(action string) string {
	switch normalizeSystemAgentAction(action) {
	case systemAgentActionQueryStaff:
		return "staff.employee.view"
	case systemAgentActionCreateStaffEmployee:
		return "staff.employee.create"
	case systemAgentActionCreateMarketing:
		return "commercial.marketing.manage"
	default:
		return ""
	}
}

func internalAgentPermissionReply(permission string) string {
	switch permission {
	case "staff.employee.view":
		return "你当前没有员工查询权限，不能通过智能体查看员工信息。"
	case "staff.employee.create":
		return "你当前没有新增员工权限，不能通过智能体创建员工。请联系有权限的负责人处理。"
	case "staff.employee.role_assign":
		return "你当前没有员工岗位分配权限，不能通过智能体调整岗位或角色。"
	case "staff.group.view":
		return "你当前没有部门/组织架构查看权限。"
	case "commercial.marketing.manage":
		return "你当前没有营销活动管理权限，不能通过智能体创建或修改营销活动。"
	default:
		return "你当前没有执行这项操作的权限。"
	}
}

func (s *Server) auditAgentPermissionDenied(
	r *http.Request,
	actor model.Actor,
	intentCode string,
	permission string,
) {
	detail, _ := json.Marshal(map[string]any{
		"intent":              intentCode,
		"required_permission": permission,
		"agent_protocol":      agentProtocolVersion,
	})
	_, _ = s.store.BeginAdminAudit(r.Context(), model.AdminAuditLog{
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		ActorRole:     actor.Role,
		ActorType:     "user",
		Source:        "internal_agent",
		Action:        "agent.permission_denied",
		ObjectType:    "permission",
		ObjectID:      permission,
		ObjectName:    intentCode,
		Reason:        "智能体请求被后端权限门禁拒绝",
		RequestID:     strings.TrimSpace(r.Header.Get("X-Request-ID")),
		DetailJSON:    string(detail),
		HTTPMethod:    r.Method,
		Path:          r.URL.Path,
		ClientIP:      requestClientIP(r),
		Result:        "denied",
	})
}

func (s *Server) auditInternalAgentCommand(
	r *http.Request,
	actor model.Actor,
	action string,
	result string,
	objectType string,
	objectID string,
	objectName string,
	detail any,
) {
	detailJSON := ""
	if detail != nil {
		if raw, err := json.Marshal(detail); err == nil {
			detailJSON = string(raw)
		}
	}
	_, _ = s.store.BeginAdminAudit(r.Context(), model.AdminAuditLog{
		ActorUserID:   actor.UserID,
		ActorUsername: actor.Username,
		ActorRole:     actor.Role,
		ActorType:     "user",
		Source:        "internal_agent",
		Action:        "agent.command." + strings.ToLower(strings.TrimSpace(action)),
		ObjectType:    strings.TrimSpace(objectType),
		ObjectID:      strings.TrimSpace(objectID),
		ObjectName:    strings.TrimSpace(objectName),
		Reason:        "后台智能体统一命令执行器",
		RequestID:     strings.TrimSpace(r.Header.Get("X-Request-ID")),
		DetailJSON:    detailJSON,
		HTTPMethod:    r.Method,
		Path:          r.URL.Path,
		ClientIP:      requestClientIP(r),
		Result:        strings.TrimSpace(result),
	})
}

func (s *Server) writeInternalAgentPermissionDenied(
	w http.ResponseWriter,
	r *http.Request,
	actor model.Actor,
	capabilities []string,
	intentCode string,
	permission string,
	reply string,
) {
	if strings.TrimSpace(reply) == "" {
		reply = internalAgentPermissionReply(permission)
	}
	s.auditAgentPermissionDenied(r, actor, intentCode, permission)
	writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
		State:              agentStatePermissionDenied,
		Code:               "permission_denied",
		RequiredPermission: permission,
		Reply:              reply,
		Capabilities:       capabilities,
	})
}
