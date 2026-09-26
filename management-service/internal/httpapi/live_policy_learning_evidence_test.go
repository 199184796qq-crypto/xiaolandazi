package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestLearningEvidenceTypeNormalization(t *testing.T) {
	if got := normalizeLearningEvidenceType(" screenshot_review "); got != "screenshot_review" {
		t.Fatalf("evidence type=%q", got)
	}
	if got := normalizeLearningEvidenceType("unknown"); got != "manual_feedback" {
		t.Fatalf("unknown evidence type=%q", got)
	}
}

func TestLearningPromptKeepsBadAnswerSeparateFromCorrection(t *testing.T) {
	input := model.CreateLivePolicyLearningCandidateInput{
		EvidenceType:  "screenshot_review",
		SourceRef:     "interaction:demo",
		SourceLayer:   model.LivePolicyLayerL3,
		IndustryCode:  "food",
		RoomID:        1001,
		Question:      "娃娃能吃吗？",
		ObservedReply: "鸡是拿来吃的，不是拿来玩的娃娃。",
		FinalReply:    "",
		Feedback:      "这里的娃娃是小孩，原回答把问题理解错了；意图理解错误不应该被评审高分放行。",
	}
	prompt, err := buildLivePolicyLearningPrompt("不要把错误回答里的事实或错误理解吸收到规则里；当时真实回答（可能是错误样本）；最终满意回复/人工改写（可能为空）", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"证据类型：screenshot_review",
		"当时真实回答（可能是错误样本）",
		input.ObservedReply,
		"最终满意回复/人工改写（可能为空）",
		input.Feedback,
		"不要把错误回答里的事实或错误理解吸收到规则里",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q", want)
		}
	}
}

func TestLearningEvidenceProposalNormalization(t *testing.T) {
	input := model.CreateLivePolicyLearningEvidenceInput{
		SourceLayer: model.LivePolicyLayerL3,
		RoomID:      1001,
		Feedback:    "先结合上下文判断口语歧义。",
	}
	got := normalizeLivePolicyLearningEvidenceProposal(livePolicyLearningEvidenceProposal{
		AbsorbRecommended: true,
		TargetLayer:       "bad-layer",
		Confidence:        180,
		RuleTitle:         "  ",
		RuleText:          " 先理解意图再回答 ",
		ExecutionMode:     "other",
		PromotionLevel:    "unknown",
		RegressionCases:   []string{"娃娃能吃吗？", "娃娃能吃吗？", "这个娃娃玩具多少钱？"},
	}, input)

	if got.TargetLayer != model.LivePolicyLayerL3 {
		t.Fatalf("target layer=%q", got.TargetLayer)
	}
	if got.Confidence != 100 {
		t.Fatalf("confidence=%d", got.Confidence)
	}
	if got.ExecutionMode != model.LivePolicyModeIntent {
		t.Fatalf("mode=%q", got.ExecutionMode)
	}
	if got.PromotionLevel != "candidate" {
		t.Fatalf("promotion=%q", got.PromotionLevel)
	}
	if len(got.RegressionCases) != 2 {
		t.Fatalf("regression cases=%#v", got.RegressionCases)
	}
	if got.RuleText != "先理解意图再回答" {
		t.Fatalf("rule text=%q", got.RuleText)
	}
}

func TestEvidencePromptAllowsMultipleIndependentLearnings(t *testing.T) {
	input := model.CreateLivePolicyLearningEvidenceInput{
		EvidenceType:  "screenshot_review",
		SourceLayer:   model.LivePolicyLayerL3,
		RoomID:        1001,
		Question:      "娃娃能吃吗？",
		ObservedReply: "鸡是拿来吃的，不是拿来玩的娃娃。",
		Feedback:      "回答理解错了，评审却给了高分。",
	}
	prompt, err := buildLivePolicyLearningEvidencePrompt("从一份证据中提炼 0 到 4 条彼此独立的候选规律；回答侧的理解原则、评审侧的质量原则；回归测试至少包含一个容易过拟合的反例", input)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"0 到 4 条彼此独立的候选规律",
		"回答侧的理解原则、评审侧的质量原则",
		"至少包含一个容易过拟合的反例",
		"娃娃能吃吗？",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("evidence prompt missing %q", want)
		}
	}
}
