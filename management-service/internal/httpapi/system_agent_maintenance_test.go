package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestClientAgentMaintenanceQueryKind(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"我的方案有哪些事实？", "facts"},
		{"这个方案还缺什么", "missing"},
		{"现在有什么福利", "benefits"},
		{"西藏包邮吗", "shipping"},
		{"2号链接包邮吗", "shipping"},
		{"有哪些商品链接", "products"},
		{"我的方案介绍下", "summary"},
		{"帮我加一条安徽包邮", "write"},
		{"打开方案页面", ""},
		{"今天直播怎么样", ""},
	}
	for _, tc := range cases {
		if got := clientAgentMaintenanceQueryKind(tc.message); got != tc.want {
			t.Fatalf("clientAgentMaintenanceQueryKind(%q) = %q, want %q", tc.message, got, tc.want)
		}
	}
}

func TestClientAgentRegionInMessage(t *testing.T) {
	cases := map[string]string{
		"安徽邮吗": "安徽",
		"西藏能发货吗": "西藏",
		"内蒙古包邮吗": "内蒙古",
		"今天直播怎么样": "",
	}
	for message, want := range cases {
		if got := clientAgentRegionInMessage(message); got != want {
			t.Fatalf("clientAgentRegionInMessage(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestFormatClientAgentFactList(t *testing.T) {
	facts := []model.LiveAgentPlanFact{
		{Category: "origin", Key: "产地", Value: "四川南充", Status: "active"},
		{Category: "craft", Key: "工艺", Value: "先卤后炸", Status: "active"},
		{Category: "old", Key: "旧事实", Value: "不应出现", Status: "disabled"},
	}
	got := formatClientAgentFactList(facts)
	if !strings.Contains(got, "共 2 条可用事实") {
		t.Fatalf("unexpected count: %s", got)
	}
	if !strings.Contains(got, "产地") || !strings.Contains(got, "四川南充") {
		t.Fatalf("missing fact content: %s", got)
	}
	if strings.Contains(got, "不应出现") {
		t.Fatalf("disabled fact leaked: %s", got)
	}
}

func TestFormatClientAgentBenefitList(t *testing.T) {
	benefits := []model.LiveAgentPlanBenefit{
		{LinkKey: "2", Activity: "包邮", Gift: "免邮费", Status: "active"},
		{LinkKey: "3", Activity: "已停用", Status: "disabled"},
	}
	got := formatClientAgentBenefitList(benefits)
	if !strings.Contains(got, "2号链接") || !strings.Contains(got, "免邮费") {
		t.Fatalf("missing benefit content: %s", got)
	}
	if strings.Contains(got, "3号链接") || strings.Contains(got, "已停用") {
		t.Fatalf("disabled benefit leaked: %s", got)
	}
}

func TestFormatPublishedShippingBenefits(t *testing.T) {
	got := formatPublishedShippingBenefits([]string{
		"- [有效福利] 福利=1:current-benefit02；链接=1、2、3；赠品=免邮费；活动=包邮",
	})
	if !strings.Contains(got, "发布快照") || !strings.Contains(got, "链接=1、2、3") {
		t.Fatalf("unexpected published benefit: %s", got)
	}
	if formatPublishedShippingBenefits(nil) != "" {
		t.Fatal("empty published benefits should be empty")
	}
}

func TestFormatClientAgentProductList(t *testing.T) {
	products := []model.LiveAgentPlanProductLink{
		{LinkKey: "1", ProductName: "杨鸭子", Spec: "900g", DailyPrice: "87.6", Status: "active"},
		{LinkKey: "2", ProductName: "停用商品", Status: "disabled"},
	}
	got := formatClientAgentProductList(products)
	if !strings.Contains(got, "1号链接") || !strings.Contains(got, "杨鸭子") {
		t.Fatalf("missing product content: %s", got)
	}
	if strings.Contains(got, "停用商品") {
		t.Fatalf("disabled product leaked: %s", got)
	}
}
