package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestFactNormalizationPreservesProgramFieldsAndClarification(t *testing.T) {
	original := model.LiveAgentPlanFactCandidate{Category: "fulfillment", Key: "发货时效", Value: "原输入", SourceQuote: "原素材", ReviewBucket: "adoptable"}
	raw := `{"fact_value":"一般当天发，下午6点后次日上午发","must_say":"一般当天发，下午6点后拍的次日上午发","can_say":"咱说的是发出，一般当天安排，6点后拍的次日上午发。","rewrite":"不得把发货说成到货，不得承诺具体到货时间","not_mention":"","question":""}`
	next, err := parseNormalizedFactFields(raw, original)
	if err != nil || next.Category != original.Category || next.Key != original.Key || next.SourceQuote != original.SourceQuote || next.ReviewBucket != original.ReviewBucket {
		t.Fatal(next, err)
	}
	if !strings.Contains(next.SafeRewrite, "【必说】") || !strings.Contains(next.SafeRewrite, "【可说】") || strings.Contains(next.SafeRewrite, "【改写】") {
		t.Fatal("safe_rewrite should carry must_say/can_say sections only", next.SafeRewrite)
	}
	if !strings.Contains(next.ForbiddenWording, "【改写】") || strings.Contains(next.ForbiddenWording, "【必说】") || strings.Contains(next.ForbiddenWording, "【不提】") {
		t.Fatal("forbidden_wording should not carry empty sections", next.ForbiddenWording)
	}
	for _, bad := range []string{`{}`, `{"fact_value":"新增事实","must_say":"","can_say":"","rewrite":"","not_mention":""}`, strings.Replace(raw, `"question":""`, `"question":"需要确认两种时间是否冲突"`, 1), strings.Replace(raw, `"question":""`, `"question":"","fact_id":5`, 1)} {
		if returned, err := parseNormalizedFactFields(bad, original); err == nil || returned.Value != original.Value {
			t.Fatal("malformed or unclear input accepted", returned, err)
		}
	}
	if normalizationReviewPassed(`{"passed":true,"reason":"符合四层定义"}`) != nil {
		t.Fatal("valid review rejected")
	}
	for _, bad := range []string{`{}`, `{"passed":false,"reason":"新增了承诺"}`, `{"passed":"true"}`, `{"passed":true,"id":"model-id"}`} {
		if normalizationReviewPassed(bad) == nil {
			t.Fatal("invalid review accepted", bad)
		}
	}
}

func TestGenerationMaterialSeparatesFactsConstraintsAndExamples(t *testing.T) {
	f := model.LiveAgentPlanFact{Key: "发货时效", Value: "一般当天发，下午6点后次日上午发", ForbiddenWording: "不得承诺具体到货日期", SafeRewrite: "哥哥姐姐们，一般当天安排发出，6点后下单就次日上午发。"}
	task := factPreviewTask(f, nil)
	content, guidance := generationMaterialViews(task.FactScope.Facts)
	factJSON, _ := json.Marshal(content)
	guidanceJSON, _ := json.Marshal(guidance)
	if !strings.Contains(string(factJSON), f.Value) || strings.Contains(string(factJSON), f.ForbiddenWording) || strings.Contains(string(factJSON), f.SafeRewrite) {
		t.Fatal("guidance remains inside fact block", string(factJSON))
	}
	if !strings.Contains(string(guidanceJSON), f.ForbiddenWording) || !strings.Contains(string(guidanceJSON), f.SafeRewrite) {
		t.Fatal("constraints or positive examples lost")
	}
	legacy := task.FactScope.Facts[0]
	legacy.ExpressionGuidance = nil
	legacy.Data = "没有隔夜货；禁说=不能说每日只做限量；安全改写=咱每天做，没有隔夜货。"
	content, guidance = generationMaterialViews([]anchorcycle.HookFact{legacy})
	if content[0]["事实内容"] != "没有隔夜货" || len(guidance) != 1 {
		t.Fatal("legacy negative fact/constraints not preserved")
	}
}
