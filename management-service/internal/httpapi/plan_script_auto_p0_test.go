package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentguard"
)

func autoScriptP0Fixture(t *testing.T) (*anchorcycle.State, *contentguard.Policy) {
	t.Helper()
	p0, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{}
	if err := state.InstallP0(p0, "test"); err != nil {
		t.Fatal(err)
	}
	policy, err := contentguard.Compile(p0)
	if err != nil {
		t.Fatal(err)
	}
	return state, policy
}

func TestAutoScriptRepairsOnlyP0SentenceAndKeepsPaidDraft(t *testing.T) {
	state, policy := autoScriptP0Fixture(t)
	original := "所以第一句先讲做法。我们是全国第一。再说怎么吃。"
	calls := 0
	result, pending := repairAutoScriptP0Sentences(state, policy, original, func(sentence string, hits []contentguard.Annotation) string {
		calls++
		if sentence != "我们是全国第一。" || len(hits) == 0 {
			t.Fatalf("sent another sentence to model: %q %+v", sentence, hits)
		}
		return "我们一直认真做。"
	})
	if calls != 1 || pending || result != "所以第一句先讲做法。我们一直认真做。再说怎么吃。" {
		t.Fatalf("calls=%d pending=%t result=%q", calls, pending, result)
	}
	result, pending = repairAutoScriptP0Sentences(state, policy, original, func(string, []contentguard.Annotation) string { return "仍是全国第一。" })
	if !pending || result != original {
		t.Fatalf("unrepaired paid draft lost: pending=%t result=%q", pending, result)
	}
	if len(autoScriptP0Hits(state, policy, "所以第一句先讲做法。")) != 0 {
		t.Fatal("ordinary ordinal should use base allowance")
	}
	if !agentgateway.UsesReviewModel("script_auto_p0_sentence_rewrite") || !agentgateway.UsesOperatorPinnedModel("script_auto_p0_sentence_rewrite") {
		t.Fatal("P0 repair must use the configured review model")
	}
	if strings.Contains(result, "我们一直认真做") {
		t.Fatal("failed repair must not replace the original sentence")
	}
}
