package decisionexecutor

import "testing"

func TestBridgeFillerRejectsGenericTransitions(t *testing.T) {
	for _, text := range []string{"说回正题，咱们继续", "好嘞，咱们接着往下说", "那，咱们接着聊", "来，接着"} {
		if !bridgeFiller(text) {
			t.Fatalf("generic transition not rejected: %q", text)
		}
	}
	if bridgeFiller("刚才说到团购优惠，接着讲套餐区别") {
		t.Fatal("specific recall wrongly rejected")
	}
}

func TestFirstBridgeReturnSentence(t *testing.T) {
	plan := returnBridgePlan{Units: []struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}{{Text: "先说第一句。再说第二句。"}}}
	if got := firstBridgeReturnSentence(plan); got != "先说第一句。" {
		t.Fatalf("got %q", got)
	}
}
