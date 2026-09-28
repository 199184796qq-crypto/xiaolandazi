package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDetectInternalAgentPermissionIntent(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		permission string
		matched    bool
	}{
		{name: "create employee", message: "帮我新增员工张三", permission: "staff.employee.create", matched: true},
		{name: "query employee", message: "查询员工名单", permission: "staff.employee.view", matched: true},
		{name: "assign role", message: "给这个员工分配岗位", permission: "staff.employee.role_assign", matched: true},
		{name: "view groups", message: "查看组织架构", permission: "staff.group.view", matched: true},
		{name: "create marketing", message: "新建营销活动国庆促销", permission: "commercial.marketing.manage", matched: true},
		{name: "ordinary consultation", message: "员工体系一般怎么设计比较合理", matched: false},
		{name: "live benefit wording", message: "修改1号链接活动赠品", matched: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, matched := detectInternalAgentPermissionIntent(tt.message)
			if matched != tt.matched {
				t.Fatalf("matched=%v want %v", matched, tt.matched)
			}
			if tt.matched && intent.Permission != tt.permission {
				t.Fatalf("permission=%q want %q", intent.Permission, tt.permission)
			}
		})
	}
}

func TestInternalAgentActionRequiredPermission(t *testing.T) {
	tests := map[string]string{
		systemAgentActionQueryStaff:          "staff.employee.view",
		systemAgentActionCreateStaffEmployee: "staff.employee.create",
		systemAgentActionCreateMarketing:     "commercial.marketing.manage",
		systemAgentActionNavigatePage:        "",
		systemAgentActionExplain:             "",
	}
	for action, want := range tests {
		if got := internalAgentActionRequiredPermission(action); got != want {
			t.Fatalf("action %s permission=%q want %q", action, got, want)
		}
	}
}

func TestWriteAgentChatOutputProtocol(t *testing.T) {
	w := httptest.NewRecorder()
	writeAgentChatOutput(w, http.StatusOK, systemAgentChatOutput{
		Reply:        "需要确认",
		Capabilities: []string{"测试"},
		Action: &systemAgentActionPreview{
			Type:                 "TEST",
			Title:                "测试",
			Summary:              "测试",
			RiskLevel:            "low",
			RequiresConfirmation: true,
			Payload:              map[string]any{},
		},
	})
	var out systemAgentChatOutput
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.ProtocolVersion != agentProtocolVersion {
		t.Fatalf("protocol=%q", out.ProtocolVersion)
	}
	if out.State != agentStateReadyToConfirm {
		t.Fatalf("state=%q want %q", out.State, agentStateReadyToConfirm)
	}
}
