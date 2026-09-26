package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestBuildLivePolicyLearningPromptDefinesLayerBoundaries(t *testing.T) {
	instruction := "配置化要求：跨行业、跨商户、跨商品、跨直播间；同一行业内多数商户都适用；具体商户、直播间、商品、活动、主播；单一客户经验不能冒充行业规则；absorb_recommended=false；换行业、换商户、换商品、换主播后还成立吗；规则层；行业层；用户层；不要输出内部层级编码"
	prompt, err := buildLivePolicyLearningPrompt(instruction, model.CreateLivePolicyLearningCandidateInput{
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

func TestAnswerReferenceLearningOverridePreservesConfirmedReply(t *testing.T) {
	candidate := model.LivePolicyLearningCandidate{
		SourceRef:     "answer_reference:live_agent_plan:2",
		Question:      "哪年的菜籽",
		FinalReply:    "宝子，咱家菜籽用的都是今年新采购的哈！刚下来的新鲜货，品质绝对放心。",
		RuleTitle:     "商品属性事实修正",
		RuleText:      "当用户纠正具体商品年份时更新知识库字段。",
		ExecutionMode: model.LivePolicyModeIntent,
	}
	title, text, fixed := answerReferenceLearningOverride(candidate)
	if title != "回答参考：哪年的菜籽" {
		t.Fatalf("title=%q want deterministic answer-reference title", title)
	}
	if !strings.Contains(text, "哪年的菜籽") || !strings.Contains(text, "今年新采购的") {
		t.Fatalf("answer reference facts lost: %s", text)
	}
	if strings.Contains(text, "更新知识库字段") {
		t.Fatalf("abstract model rule leaked into adopted answer reference: %s", text)
	}
	if fixed != "" {
		t.Fatalf("intent answer reference fixed_text=%q want empty", fixed)
	}
}

func TestAnswerReferenceLearningOverrideVerbatimUsesFinalReply(t *testing.T) {
	candidate := model.LivePolicyLearningCandidate{
		SourceRef:     "answer_reference:live_agent_plan:2",
		Question:      "哪年的菜籽",
		FinalReply:    "咱家用的是今年新采购的菜籽。",
		RuleText:      "模型抽象规则",
		ExecutionMode: model.LivePolicyModeVerbatim,
	}
	_, text, fixed := answerReferenceLearningOverride(candidate)
	if !strings.Contains(text, candidate.FinalReply) {
		t.Fatalf("verbatim reference missing final reply: %s", text)
	}
	if fixed != candidate.FinalReply {
		t.Fatalf("fixed_text=%q want final_reply=%q", fixed, candidate.FinalReply)
	}
}

func TestActiveLearningPolicyVersionIgnoresDraft(t *testing.T) {
	active := model.LivePolicyVersion{
		ID:              10,
		VersionNo:       2,
		LifecycleStatus: "active",
		Overrides: []model.LivePolicyOverride{{
			Key:  "active-rule",
			Text: "当前已生效用户层规则",
		}},
	}
	ctx := model.LivePolicyContext{
		Active: &active,
		Versions: []model.LivePolicyVersion{
			{
				ID:              11,
				VersionNo:       3,
				LifecycleStatus: "draft",
				Overrides: []model.LivePolicyOverride{{
					Key:  "draft-rule",
					Text: "尚未发布的其他草稿",
				}},
			},
			active,
		},
	}
	got := activeLearningPolicyVersion(ctx)
	if got == nil || got.ID != active.ID {
		t.Fatalf("active base=%+v want active version id=%d", got, active.ID)
	}
	if len(got.Overrides) != 1 || got.Overrides[0].Key != "active-rule" {
		t.Fatalf("unexpected active overrides: %+v", got.Overrides)
	}
}
