package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestRenderPlanPromptFillsDataAndDropsEmptySections(t *testing.T) {
	master := "名字：{{主播名}}\n\n【商品卡】\n{{#商品}}{{链接号}}号链接：\n{{#字段}}{{名称}}：{{值}}\n{{/字段}}{{/商品}}\n【活动福利】\n{{#福利}}{{序号}}. {{内容}}\n{{/福利}}\n【事实依据】\n（这一节由你自己填）\n【用词消歧】\n（这一节也由你自己填）\n尾巴：{{未知占位符}}"
	data := planPromptRenderData{
		PlanName: "测试直播间",
		Links: []model.LiveAgentPlanProductLink{
			{LinkKey: "1", ProductName: "整只装", Spec: "900g", DailyPrice: "87.6", Quantity: "1", Status: "active"},
		},
		Benefits: []model.LiveAgentPlanBenefit{
			{Activity: "包邮", Gift: "包邮", Status: "active"},
		},

	}
	got := renderPlanPrompt(master, data)
	if strings.Contains(got, "{{") || strings.Contains(got, "}}") {
		t.Fatalf("残留占位符: %s", got)
	}
	if !strings.Contains(got, "（这一节由你自己填）") {
		t.Fatalf("事实依据应由用户手填并保留章节: %s", got)
	}
	if !strings.Contains(got, "1号链接") || !strings.Contains(got, "价格：87.6") {
		t.Fatalf("商品卡未渲染: %s", got)
	}
	if strings.Contains(got, "份数") || strings.Contains(got, "数量") {
		t.Fatalf("数量=1 应跳过: %s", got)
	}
	if !strings.Contains(got, "1. 包邮") {
		t.Fatalf("福利未渲染: %s", got)
	}
	if !strings.Contains(got, "【用词消歧】") {
		t.Fatalf("用词消歧应保留给用户填写: %s", got)
	}
	if !strings.Contains(got, "名字：小林") {
		t.Fatalf("默认值未生效: %s", got)
	}
	if strings.Contains(got, "尾巴") {
		t.Fatalf("无来源占位符所在行应整行删除: %s", got)
	}
}
