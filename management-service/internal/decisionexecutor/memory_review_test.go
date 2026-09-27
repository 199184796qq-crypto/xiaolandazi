package decisionexecutor

import "testing"

func TestMemoryWordingRiskTerms(t *testing.T) {
	contextText := "前文\n【用词硬校验词】喝｜喝起来｜饮用\n后文"
	got := memoryWordingRiskTerms(contextText)
	if len(got) != 3 {
		t.Fatalf("expected 3 terms, got %#v", got)
	}
	for index, want := range []string{"喝", "喝起来", "饮用"} {
		if got[index] != want {
			t.Fatalf("term %d: got %q want %q", index, got[index], want)
		}
	}
}

func TestHasAgentMemoryPrompt(t *testing.T) {
	if !hasAgentMemoryPrompt("前文\n【当前直播间智能体记忆】\n1. 事实记忆") {
		t.Fatal("expected active agent memory marker to require final review")
	}
	if hasAgentMemoryPrompt("当前直播间智能体记忆：") {
		t.Fatal("empty memory placeholder should not require final review")
	}
}

func TestFinalReviewReasonTextForMemoryOnlyReview(t *testing.T) {
	got := finalReviewReasonText(nil, true)
	if got == "" || got == "无" {
		t.Fatalf("expected memory-only review reason, got %q", got)
	}
}
