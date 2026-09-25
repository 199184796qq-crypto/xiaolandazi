package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestBuildLivePolicyLearningPromptDefinesLayerBoundaries(t *testing.T) {
	prompt, err := buildLivePolicyLearningPrompt(model.CreateLivePolicyLearningCandidateInput{
		SourceLayer:  model.LivePolicyLayerL3,
		IndustryCode: "food",
		RoomID:       12,
		Question:     "这款怎么吃？",
		FinalReply:   "这是最终满意回复",
		Feedback:     "再自然一点",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"跨行业、跨商户、跨商品、跨直播间",
		"同一行业内多数商户都适用",
		"具体商户、直播间、商品、活动、主播",
		"单一客户经验不能冒充行业规则",
		"absorb_recommended=false",
		"换行业、换商户、换商品、换主播后还成立吗",
		"规则层",
		"行业层",
		"用户层",
		"不要输出内部层级编码",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
}

func TestNormalizeLivePolicyLearningModelOutputFallsBackSafely(t *testing.T) {
	output := normalizeLivePolicyLearningModelOutput(
		livePolicyLearningModelOutput{
			TargetLayer:   "unknown",
			Confidence:    120,
			ExecutionMode: "unexpected",
		},
		model.CreateLivePolicyLearningCandidateInput{
			SourceLayer: model.LivePolicyLayerL2,
			FinalReply:  "满意回复",
			Feedback:    "以后都这样说",
		},
	)
	if output.TargetLayer != model.LivePolicyLayerL2 {
		t.Fatalf("target layer=%q want L2", output.TargetLayer)
	}
	if output.Confidence != 100 {
		t.Fatalf("confidence=%d want 100", output.Confidence)
	}
	if output.ExecutionMode != model.LivePolicyModeIntent {
		t.Fatalf("execution mode=%q want intent", output.ExecutionMode)
	}
	if output.RuleTitle == "" || output.RuleText == "" || output.Reason == "" {
		t.Fatalf("normalized output incomplete: %+v", output)
	}
}

func TestNormalizeLivePolicyLearningModelOutputUsesRoomForUnknownSource(t *testing.T) {
	output := normalizeLivePolicyLearningModelOutput(
		livePolicyLearningModelOutput{
			TargetLayer: "bad-layer",
			Confidence:  -8,
		},
		model.CreateLivePolicyLearningCandidateInput{
			SourceLayer: "unknown",
			RoomID:      88,
			FinalReply:  "满意回复",
		},
	)
	if output.TargetLayer != model.LivePolicyLayerL3 {
		t.Fatalf("target layer=%q want L3", output.TargetLayer)
	}
	if output.Confidence != 0 {
		t.Fatalf("confidence=%d want 0", output.Confidence)
	}
}
