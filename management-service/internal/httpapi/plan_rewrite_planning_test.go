package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

func TestPlanRewritePlanningResolution(t *testing.T) {
	empty := ""
	if got, err := resolvePlanRewritePlanning(db.LiveAgentPlanPromptDocument{RewritePlanningMD: &empty, RewritePromptMD: "旧派生内容"}); err != nil || got != "" {
		t.Fatal("explicit empty original replaced", got, err)
	}
	sections := planprompt.Sections{PersonaMD: "人设", FactsMD: "事实"}
	rules := "第一步：骨架\n{{已有开场}}"
	for _, legacy := range []string{planprompt.CompileRewritePlanning(sections, rules), strings.Replace(planprompt.CompileRewritePlanning(sections, rules), "{{样本话术}}", "", 1), rules} {
		got, err := resolvePlanRewritePlanning(db.LiveAgentPlanPromptDocument{Sections: sections, RewritePromptMD: legacy})
		if err != nil || got != rules {
			t.Fatal("legacy original not resolved", got, err)
		}
	}
	if _, err := resolvePlanRewritePlanning(db.LiveAgentPlanPromptDocument{RewritePromptMD: planprompt.RewriteIntro + "未知旧结构"}); err == nil {
		t.Fatal("unknown full prompt silently overwritten")
	}
}

func TestPlanRewriteSampleInjectedOnlyIntoActualRequest(t *testing.T) {
	rules := "第一步：抽取直播流程骨架\n这一版指定的开场方式：{{开场方式}}\n{{已有开场}}"
	saved := planprompt.CompileRewritePlanning(planprompt.Sections{PersonaMD: "本店主播", FactsMD: "八十七元六角", RedLinesMD: "不得编造"}, rules)
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{PlanID: 4}, scriptRewriteSnapshot{Prompt: saved, Facts: "八十七元六角"})
	for _, sample := range []string{"这条是第一片的实际样本", "这条是第二片的实际样本"} {
		prompt, err := (&Server{}).scriptRewritePrompt(t.Context(), 14, v, sample, "已有旧开场", "换个角度进入")
		if err != nil || !strings.Contains(prompt, "【样本话术】\n"+sample+"\n\n第一步：") || !strings.Contains(prompt, "换个角度进入") || !strings.Contains(prompt, "已有旧开场") {
			t.Fatal("request missing current piece or runtime context", prompt, err)
		}
		if strings.Contains(prompt, "{{样本话术}}") || strings.Contains(prompt, "{{已有开场}}") || strings.Contains(prompt, "{{开场方式}}") {
			t.Fatal("runtime placeholders unresolved", prompt)
		}
	}
	if strings.Count(saved, "【样本话术】\n{{样本话术}}") != 1 || !strings.HasSuffix(saved, rules) {
		t.Fatal("stored sample slot or original rules mutated", saved)
	}
}

func TestPlanRewriteDefaultShowsRulesWithoutInjectedContext(t *testing.T) {
	rules, err := defaultPlanRewritePlanning()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rules, "--------------------------------------------------\n第一步：抽取直播流程骨架\n") || !strings.Contains(rules, "话术内容只能是一段") {
		t.Fatal("default rules body incomplete", rules)
	}
	for _, context := range []string{planprompt.RewriteIntro, "【主播人设】\n{{主播人设}}", "【事实依据】\n{{事实依据}}", "【样本话术】"} {
		if strings.Contains(rules, context) {
			t.Fatal("context envelope leaked into rules editor", context)
		}
	}
	if !strings.Contains(rules, "{{已有开场}}") || !strings.Contains(rules, "{{开场方式}}") {
		t.Fatal("runtime opening placeholders lost", rules)
	}
	prompt, err := renderPlanRewritePrompt(planprompt.Sections{PersonaMD: "测试人设", FactsMD: "测试事实", RedLinesMD: "测试红线"})
	if err != nil || !strings.Contains(prompt, "【样本话术】\n{{样本话术}}\n\n"+rules) {
		t.Fatal("default fallback removed sample slot", prompt, err)
	}
}
