package agentgateway

import "testing"

func TestExpressionReviewVerdictSurvivesOptionalAdvice(t *testing.T) {
	raw := `{"passed":false,"quote":"早上赶时间","reason":"早餐场景暗示","suggestions":["删除早餐举例"]}`
	out, e := ParseExpressionReview(raw, "早上赶时间，开袋就能吃。")
	if e != nil || *out.Passed {
		t.Fatal("valid rejection lost to optional advice format", out, e)
	}
	if _, e = ParseExpressionReview(`{"passed":true,"quote":"不是当前原文","reason":"","suggestions":[]}`, "当前正文"); e != ErrExpressionReviewQuote {
		t.Fatal("unbound audit quote accepted", e)
	}
}
