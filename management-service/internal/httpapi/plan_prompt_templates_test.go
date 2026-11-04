package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

func TestSixTemplatesRenderSeparatelyAndFactsKeepCurrentBusiness(t *testing.T) {
	value := planprompt.Templates{Sections: planprompt.Sections{
		PersonaMD: "人设母版\n直播间：{{直播间名}}", RedLinesMD: "红线母版：不编造承诺",
		SpeechRequirementsMD: "主线母版：口语化", InteractionRequirementsMD: "互动母版：回答问题",
		FactsMD: "【商品卡】\n{{#商品}}原占位{{/商品}}\n【活动福利】\n{{#福利}}原占位{{/福利}}\n【事实依据】\n旧事实占位\n【数据使用规则】\n只使用已确认资料",
	}, RewritePlanningMD: "规划母版\n{{已有开场}}\n{{开场方式}}"}
	data := planPromptRenderData{PlanName: "甲方案", Links: []model.LiveAgentPlanProductLink{{LinkKey: "2", ProductName: "甲商品", DailyPrice: "39", Status: "active"}, {ProductName: "停用商品", Status: "inactive"}}, Benefits: []model.LiveAgentPlanBenefit{{Activity: "已确认福利", Status: "active"}}, Facts: []model.LiveAgentPlanFact{{Category: "craft", Value: "已确认工艺", Status: "active"}, {Value: "已删事实", Status: "deleted"}}}
	before := value.Revision()
	got := renderPlanPromptTemplates(value, data)
	for key, marker := range map[string]string{"persona": "人设母版", "redlines": "红线母版", "speech": "主线母版", "interaction": "互动母版"} {
		if !strings.Contains(got[key], marker) {
			t.Fatal(key, got)
		}
		for other, otherMarker := range map[string]string{"persona": "人设母版", "redlines": "红线母版", "speech": "主线母版", "interaction": "互动母版"} {
			if other != key && strings.Contains(got[key], otherMarker) {
				t.Fatal("cross-template contamination", key, other)
			}
		}
	}
	for _, want := range []string{"甲商品", "价格：39", "已确认福利", "已确认工艺", "只使用已确认资料"} {
		if !strings.Contains(got["facts"], want) {
			t.Fatal(want, got["facts"])
		}
	}
	for _, no := range []string{"停用商品", "已删事实", "旧事实占位", "原占位"} {
		if strings.Contains(got["facts"], no) {
			t.Fatal(no, got["facts"])
		}
	}
	if value.Revision() != before || !strings.Contains(value.RewritePlanningMD, "{{已有开场}}") {
		t.Fatal("render mutated originals or planning placeholders")
	}
	for _, mother := range []string{"母版事实约束：只使用已确认资料", ""} {
		simple := value
		simple.FactsMD = mother
		filled := renderPlanPromptTemplates(simple, data)["facts"]
		for _, want := range []string{mother, "甲商品", "价格：39", "已确认福利", "已确认工艺"} {
			if !strings.Contains(filled, want) {
				t.Fatal("editing facts layout dropped current plan data", want, filled)
			}
		}
	}
	data.PlanName = "乙方案"
	data.Links = nil
	data.Benefits = nil
	data.Facts = nil
	other := renderPlanPromptTemplates(value, data)
	if strings.Contains(other["facts"], "甲商品") || !strings.Contains(other["persona"], "乙方案") {
		t.Fatal("plan data leaked", other)
	}
}

func TestTemplateMaintenanceEndpointsRequireAuthentication(t *testing.T) {
	s := &Server{}
	for _, method := range []string{"GET", "PUT"} {
		r := httptest.NewRequest(method, "/api/v1/system/plan-prompt-templates", strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		if method == "GET" {
			s.planPromptTemplatesGet(w, r)
		} else {
			s.planPromptTemplatesPut(w, r)
		}
		if w.Code != 401 {
			t.Fatalf("%s: got %d", method, w.Code)
		}
	}
}

// The operator's installed mother must change together with the wire contract.
// This reads the real project mother, not a duplicate fixture that can drift.
func TestInstalledOperatorInteractionMotherMatchesFiveFieldContract(t *testing.T) {
	value, err := installedPlanPromptTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(value.InteractionRequirementsMD, planprompt.InteractionOutputContract) {
		t.Fatal("operator mother and program interaction contract drifted")
	}
	for _, old := range []string{"置信度：", "候选：", "回答：", "优先程度：", "对话关系：", "不到四成", "置信度不到 30"} {
		if strings.Contains(value.InteractionRequirementsMD, old) {
			t.Fatalf("retired contract in operator mother: %s", old)
		}
	}
	if strings.Contains(value.RedLinesMD, "【输出格式") {
		t.Fatal("interaction/speech contract leaked into shared red lines")
	}
	compiled := value.Sections.Interaction()
	if strings.Count(compiled, planprompt.InteractionOutputContract) != 1 {
		t.Fatal("saved plan must compile exactly one current interaction contract")
	}
	if strings.Contains(value.Sections.Speech(), "意图：") {
		t.Fatal("interaction fields leaked into speech contract")
	}
}
