package anchorcycle

import "testing"

func TestDirectionReviewMustMatchCurrentContentAndContext(t *testing.T) {
	h := AngleHook{ID: "program", Action: "从预算安排讲规格选择"}
	r := DirectionReviewReceipt{HookID: h.ID, ContentSHA: DirectionContentSHA(h), ContextSHA: "current", Decision: "通过"}
	if len(ReviewedDirectionCards([]AngleHook{h}, []DirectionReviewReceipt{r}, "current")) != 1 {
		t.Fatal("valid direction lost")
	}
	h.Action = "讲一个未经确认的新功能"
	if len(ReviewedDirectionCards([]AngleHook{h}, []DirectionReviewReceipt{r}, "current")) != 0 {
		t.Fatal("old approval applied to changed text")
	}
	h.Action = "从预算安排讲规格选择"
	if len(ReviewedDirectionCards([]AngleHook{h}, []DirectionReviewReceipt{r}, "changed-facts")) != 0 {
		t.Fatal("old approval applied to changed context")
	}
	r.Error = "timeout"
	if len(ReviewedDirectionCards([]AngleHook{h}, []DirectionReviewReceipt{r}, "current")) != 0 {
		t.Fatal("incomplete review accepted")
	}
	for _, raw := range []string{"{\"hook_id\":\"fake\",\"pass\":true}", "可能通过", "通过？"} {
		if _, e := ParseDirectionReview(raw); e == nil {
			t.Fatal("unfinished judgment accepted")
		}
	}
	for _, raw := range []string{"有问题。期限与已确认事实不一致。", "通过。", "不确定：资料不足", "有问题. unsupported"} {
		if _, e := ParseDirectionReview(raw); e != nil {
			t.Fatal("ordinary punctuation became a format failure", raw, e)
		}
	}
}
