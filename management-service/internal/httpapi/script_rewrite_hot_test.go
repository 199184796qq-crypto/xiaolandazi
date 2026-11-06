package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/planprompt"
)

func TestRuntimeRewriteUsesNewPlanningWithoutInvalidatingPaidPool(t *testing.T) {
	s := planprompt.Sections{PersonaMD: "本店主播", FactsMD: "八十七元", RedLinesMD: "不编造"}
	frozen := scriptRewriteSnapshot{Prompt: planprompt.CompileRewritePlanning(s, "旧规划"), Facts: s.FactsMD}
	v := withScriptRewriteSnapshot(model.LiveAgentPlanVersion{TenantID: 14, PlanID: 4}, frozen)
	key := scriptRewriteFactsKey(v)
	hot := planprompt.RuntimePrompts{Sections: s, Planning: "新规划：口语短句", Enabled: true}
	prompt, ok := hot.RewriteFor(frozen.Prompt)
	if !ok {
		t.Fatal("matching frozen context refused hot planning")
	}
	actual, err := renderBackgroundScriptRewritePrompt(prompt, "本条原始话术", "", "", scriptRewriteOutput{})
	if err != nil || !strings.Contains(actual, "新规划：口语短句") || strings.Contains(actual, "旧规划") || !strings.Contains(actual, "本条原始话术") || scriptRewriteFactsKey(v) != key {
		t.Fatal("actual rewrite request or paid inventory identity changed", actual, err)
	}
	for _, changed := range []planprompt.Sections{{PersonaMD: "其他主播", FactsMD: s.FactsMD, RedLinesMD: s.RedLinesMD}, {PersonaMD: s.PersonaMD, FactsMD: "其他价格", RedLinesMD: s.RedLinesMD}, {PersonaMD: s.PersonaMD, FactsMD: s.FactsMD, RedLinesMD: "其他红线"}} {
		hot.Sections = changed
		if _, ok = hot.RewriteFor(frozen.Prompt); ok {
			t.Fatal("hot planning crossed frozen business context")
		}
	}
}
