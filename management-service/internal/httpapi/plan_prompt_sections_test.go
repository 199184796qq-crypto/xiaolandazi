package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPlanPromptFactsUseOnlyActiveCurrentAssets(t *testing.T) {
	data := planPromptRenderData{
		Links:    []model.LiveAgentPlanProductLink{{LinkKey: "1", ProductName: "整只", Spec: "900克", DailyPrice: "87.6", Status: "active"}, {LinkKey: "2", ProductName: "停用商品", Status: "inactive"}},
		Benefits: []model.LiveAgentPlanBenefit{{Activity: "已确认福利", Status: "active"}, {Activity: "停用福利", Status: "inactive"}},
		Facts:    []model.LiveAgentPlanFact{{Category: "craft", Value: "先卤后炸", Status: "active"}, {Category: "delivery", Value: "京东物流", ForbiddenWording: "不承诺具体到达日期", SafeRewrite: "按地址确认物流", Status: "active"}, {Value: "删除的旧事实", Status: "deleted"}},
	}
	got := renderPlanPromptFacts(data)
	for _, want := range []string{"1号链接", "价格：87.6", "已确认福利", "工艺：先卤后炸", "快递：京东物流", "表达限制：不承诺具体到达日期", "参考表达：按地址确认物流"} {
		if !strings.Contains(got, want) {
			t.Fatal(want, got)
		}
	}
	for _, no := range []string{"停用商品", "停用福利", "删除的旧事实", "身份设定", "互动约束", "话术规则"} {
		if strings.Contains(got, no) {
			t.Fatal(no, got)
		}
	}
	if empty := renderPlanPromptFacts(planPromptRenderData{}); empty != "" {
		t.Fatal(empty)
	}
}
