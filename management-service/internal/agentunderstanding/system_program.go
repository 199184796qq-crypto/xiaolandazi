package agentunderstanding

import (
	"regexp"
	"strings"
)

type SystemNavigation struct {
	Title   string
	To      string
	Section string
}

type SystemEmployee struct {
	DisplayName string
	GroupName   string
	Status      string
}

type SystemProgramContext struct {
	Navigation []SystemNavigation
	Employees  []SystemEmployee
	Groups     []string
	Roles      []string
	HasImages  bool
}

func ProgramInterpretSystem(message string, ctx SystemProgramContext) UnifiedIntent {
	message = strings.TrimSpace(message)
	compact := compactProgramText(message)
	out := programIntent(KindClarify, "EXPLAIN", 0.4)
	out.Engine = "program"
	if message == "" {
		out.Reply = "请告诉我你要处理什么。"
		return out
	}
	if isCancelOnly(compact) {
		out.Kind, out.Intent, out.Confidence, out.Reply = KindChat, "EXPLAIN", 1, "好，这次不执行操作。"
		return out
	}
	if ctx.HasImages {
		out.Missing = []string{"图片语义"}
		out.Reply = "当前使用程序理解，不能可靠识别图片内容。请用文字补充，或切换到大模型理解。"
		out.Confidence = 0.95
		return out
	}

	if containsAnyProgram(compact, "打开", "进入", "跳转", "带我去", "去到") {
		matches := make([]SystemNavigation, 0)
		for _, item := range ctx.Navigation {
			if item.Title != "" && strings.Contains(message, item.Title) {
				matches = append(matches, item)
			}
		}
		if len(matches) == 1 {
			out.Kind, out.Intent, out.Confidence = KindCommand, "NAVIGATE_PAGE", 0.99
			out.Target["title"] = matches[0].Title
			out.Target["to"] = matches[0].To
			out.Target["section"] = matches[0].Section
			return out
		}
		out.Missing = []string{"页面"}
		out.Reply = "请说清楚要打开哪个功能页面。"
		return out
	}

	if containsAnyProgram(compact, "查询员工", "查看员工", "员工名单", "员工列表", "多少员工", "员工资料", "查员工") {
		out.Kind, out.Intent, out.Confidence = KindCommand, "QUERY_STAFF", 0.98
		if containsAnyProgram(compact, "多少员工", "员工数量", "有几个员工") {
			out.Changes["count_only"] = true
		}
		for _, group := range ctx.Groups {
			if group != "" && strings.Contains(message, group) {
				out.Changes["group_name"] = group
				break
			}
		}
		for _, employee := range ctx.Employees {
			if employee.DisplayName != "" && strings.Contains(message, employee.DisplayName) {
				out.Changes["display_name"] = employee.DisplayName
				break
			}
		}
		return out
	}

	if containsAnyProgram(compact, "新增员工", "添加员工", "创建员工", "录入员工") {
		out.Intent = "CREATE_STAFF_EMPLOYEE"
		out.Confidence = 0.82
		fields := map[string]string{
			"display_name": extractField(message, "员工姓名", "姓名", "名字"),
			"phone":        extractPhone(message),
			"email":        extractField(message, "邮箱", "Email", "email"),
			"province":     extractField(message, "省份", "省"),
			"city":         extractField(message, "城市", "市"),
			"district":     extractField(message, "区县", "区", "县"),
		}
		for key, value := range fields {
			if value != "" {
				out.Changes[key] = value
			}
		}
		for _, group := range ctx.Groups {
			if group != "" && strings.Contains(message, group) {
				out.Changes["group_name"] = group
				break
			}
		}
		roleNames := make([]string, 0)
		for _, role := range ctx.Roles {
			if role != "" && strings.Contains(message, role) {
				roleNames = append(roleNames, role)
			}
		}
		if len(roleNames) > 0 {
			out.Changes["role_names"] = roleNames
		}
		missing := make([]string, 0)
		for _, key := range []string{"display_name", "phone", "group_name"} {
			if _, ok := out.Changes[key]; !ok {
				missing = append(missing, key)
			}
		}
		if len(roleNames) == 0 {
			missing = append(missing, "role_names")
		}
		if len(missing) > 0 {
			out.Kind = KindClarify
			out.Missing = missing
			out.Reply = "新增员工还缺少必要信息，请补充姓名、手机号、部门和岗位。"
			return out
		}
		out.Kind = KindCommand
		out.Confidence = 0.98
		return out
	}

	if containsAnyProgram(compact, "创建营销活动", "新增营销活动", "添加营销活动", "创建营销计划", "新增营销计划") {
		out.Intent = "CREATE_MARKETING_CAMPAIGN"
		out.Missing = []string{"活动名称", "营销标的", "优惠规则"}
		out.Reply = "程序理解已识别到创建营销活动，但需要你明确活动名称、营销标的和优惠规则后才能生成执行预览。"
		out.Confidence = 0.86
		return out
	}

	out.Kind = KindChat
	out.Intent = "EXPLAIN"
	out.Confidence = 0.55
	out.Reply = "当前使用程序理解。你可以直接说查询员工、创建员工、打开页面或创建营销活动；涉及修改时仍会经过权限和确认。"
	return out
}

var phonePattern = regexp.MustCompile(`1[3-9][0-9]{9}`)

func extractPhone(value string) string {
	return phonePattern.FindString(value)
}
