package agentdecision

import "testing"

func TestClarificationTopicGetsHighestPriority(t *testing.T) {
	if got := normalizePriority(Candidate{Topic: "CLARIFY:1"}); got != QuickAnswerPriority+10 {
		t.Fatalf("clarify priority=%d", got)
	}
	if got := normalizePriority(Candidate{Topic: "CLARIFY_REPLY:1"}); got != QuickAnswerPriority+10 {
		t.Fatalf("clarify reply priority=%d", got)
	}
	if got := normalizePriority(Candidate{Topic: "普通问题"}); got >= QuickAnswerPriority+10 {
		t.Fatalf("normal priority too high: %d", got)
	}
}
