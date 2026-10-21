package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentpools"
	"strings"
	"testing"
)

func TestSpeechConstraintDraftStrictAndClarification(t *testing.T) {
	for _, raw := range []string{`{"understanding":"早餐边界","constraint":"不用早餐举例","question":"","products":[999]}`, `{"understanding":"早餐边界","constraint":"不用早餐举例"}`, `{"understanding":"","constraint":"","question":""}`} {
		if _, e := parseSpeechConstraint(raw); e == nil {
			t.Fatal("invalid constraint accepted", raw)
		}
	}
	c, e := parseSpeechConstraint(`{"understanding":"需要说明哪里不对","constraint":"不能采用的草稿","question":"哪种场景不合适？"}`)
	if e != nil || *c.Question == "" {
		t.Fatal(c, e)
	}
}
func TestAdoptedExpressionOverlayInvalidatesDirectionsAndReachesGeneration(t *testing.T) {
	state := cycleTestState(t)
	state.BaselineJSON = textTestBaseline()
	key := state.AnglePlanKey()
	state.LatestExpressionConstraints = []map[string]any{{"用户原话（理解纠正范围，不能扩张限制）": "不要拿早餐举例", "表达限制": "不把商品作为早餐或早晨赶时间开袋吃的场景举例。", "用户采用的实时表达约束": true}}
	if key == state.AnglePlanKey() {
		t.Fatal("old direction cached despite newly adopted constraint")
	}
	messages, e := renderCycleTextMessages(&state)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(messages)
	if !strings.Contains(string(raw), "早餐") || strings.Contains(string(raw), "draft_id") {
		t.Fatal("constraint missing or internal key leaked", string(raw))
	}
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}, Corrections: []contentpools.Correction{{Kind: contentpools.ExpressionConstraintKind, Products: []int64{1}, Feedback: "不要拿早餐举例", Content: contentpools.CorrectionContent{Forbidden: "不用早餐举例"}}, {Kind: "draft", Products: []int64{2}, Content: contentpools.CorrectionContent{Forbidden: "未采用约束"}}, {Kind: contentpools.ExpressionConstraintKind, Products: []int64{2}, Content: contentpools.CorrectionContent{Forbidden: "未澄清", Question: "待补充"}}}}
	live := contentpools.ExpressionConstraintSource(source)
	if len(contentpools.CorrectionView(live, []int64{1})) != 1 || len(contentpools.CorrectionView(live, []int64{2})) != 0 {
		t.Fatal("draft/other product leaked into live rules")
	}
	c := anchorcycle.Candidate{ID: anchorcycle.ID(), Text: "不用额外加工，打开包装就能吃。", OutputText: "不用额外加工，打开包装就能吃。"}
	if state.ExpressionCandidateApproved(c) {
		t.Fatal("unreviewed expression rule bypassed live admission")
	}
	c.ExpressionAudit = &anchorcycle.ExpressionAudit{Passed: true, TextSHA: anchorcycle.ProductDigest(c.Text), ScopeSHA: anchorcycle.ProductDigest(state.LatestExpressionConstraints)}
	if !state.ExpressionCandidateApproved(c) {
		t.Fatal("valid expression receipt rejected")
	}
	state.LatestExpressionConstraints[0]["表达限制"] = "不用午夜场景举例"
	if state.ExpressionCandidateApproved(c) {
		t.Fatal("stale expression receipt accepted")
	}
}
