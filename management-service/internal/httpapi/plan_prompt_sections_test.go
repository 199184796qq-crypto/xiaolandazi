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

func TestSplitPlanPromptSectionsCoversFourTabs(t *testing.T) {
	text := strings.Join([]string{
		"【身份设定】",
		"你是专属带货主播。",
		"【性格 & 语气设定】",
		"真诚稳重。",
		"【商品卡】",
		"商品A 45.5 元",
		"【活动福利】",
		"包邮",
		"【话术规则】",
		"短句为主。",
		"【称呼规则】",
		"叫朋友。",
		"【互动约束】",
		"一问一答。",
		"【禁忌边界】",
		"不夸大。",
	}, "\n")
	sections := splitPlanPromptSections(text)
	if !strings.Contains(sections["persona"], "专属带货主播") || !strings.Contains(sections["persona"], "真诚稳重") {
		t.Fatalf("主播人设没归好: %q", sections["persona"])
	}
	if !strings.Contains(sections["facts"], "商品A") || !strings.Contains(sections["facts"], "包邮") {
		t.Fatalf("事实依据没归好: %q", sections["facts"])
	}
	if !strings.Contains(sections["speech"], "短句为主") || !strings.Contains(sections["speech"], "叫朋友") {
		t.Fatalf("话术要求没归好: %q", sections["speech"])
	}
	if !strings.Contains(sections["redlines"], "不夸大") {
		t.Fatalf("红线禁止没归好: %q", sections["redlines"])
	}
	if !strings.Contains(sections["interaction"], "一问一答") {
		t.Fatalf("互动要求没归好: %q", sections["interaction"])
	}
}

func TestSplitPlanPromptSectionsKeepsUnknownHeading(t *testing.T) {
	text := "【身份设定】\n你是主播。\n【某个新标题】\n新内容。\n【话术规则】\n短句。"
	sections := splitPlanPromptSections(text)
	if !strings.Contains(sections["persona"], "新内容。") {
		t.Fatalf("未识别标题应留在原分区，实际: %q", sections["persona"])
	}
	if !strings.Contains(sections["speech"], "短句") {
		t.Fatalf("后续分区应正常开始: %q", sections["speech"])
	}
}

func TestSplitPlanPromptSectionsEmpty(t *testing.T) {
	sections := splitPlanPromptSections("没有标题的纯文本")
	for _, key := range []string{"persona", "facts", "speech", "interaction"} {
		if _, ok := sections[key]; !ok {
			t.Fatalf("四个分区都必须存在，缺 %s", key)
		}
	}
}
func TestFillPlanPromptFactsBlockKeepsRules(t *testing.T) {
	section := strings.Join([]string{
		"【商品卡】",
		"1号链接：商品A 45.5",
		"【活动福利】",
		"包邮",
		"【事实依据】",
		"〔提示〕一行一条",
		"【数据使用规则】",
		"1. 只能使用上面给出的事实。",
		"2. 价格只报商品卡里写着的价格。",
	}, "\n")
	got := fillPlanPromptFactsBlock(section, "原料：土麻鸭\n价格：45.5")
	for _, want := range []string{"商品A 45.5", "包邮", "原料：土麻鸭", "【数据使用规则】", "只能使用上面给出的事实", "价格只报商品卡里写着的价格"} {
		if !strings.Contains(got, want) {
			t.Fatalf("缺少 %q\n实际:\n%s", want, got)
		}
	}
	if strings.Contains(got, "〔提示〕") {
		t.Fatalf("旧的提示行应被替换掉:\n%s", got)
	}
}

func TestFillPlanPromptFactsBlockWithoutFactsKeepsRules(t *testing.T) {
	section := "【事实依据】\n〔提示〕一行一条\n【数据使用规则】\n1. 规则一。"
	got := fillPlanPromptFactsBlock(section, "")
	if !strings.Contains(got, "【数据使用规则】") || !strings.Contains(got, "规则一") {
		t.Fatalf("没有事实时也必须保留规则:\n%s", got)
	}
}