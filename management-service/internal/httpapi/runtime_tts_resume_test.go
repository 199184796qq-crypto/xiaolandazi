package httpapi

import "testing"

func TestResolveDevInteractionResumeSkipsImmediateCoveredDeliveryBlock(t *testing.T) {
	plan := devInteractionPlan{
		ReplyCore:     "江浙沪皖正常隔天到，其他地区两三天。",
		CoveredTopics: []string{"SHIPPING"},
		ResumeMode:    "DIRECT",
	}
	input := devAnswerTTSInput{
		NextMainlineUnits: []devMainlineUnitInput{
			{ID: "S012", Text: "江浙沪皖的朋友，正常发货后基本隔天就能到，其他地区大概需要2-3天。", Topics: []string{"delivery"}},
			{ID: "S013", Text: "大家收到后按实际状态检查即可。", Topics: []string{"general"}},
			{ID: "S014", Text: "如果拿到手不知道怎么处理，包装袋后面有教程入口。", Topics: []string{"cooking"}},
		},
	}
	got, changed := resolveDevInteractionResume(plan, input)
	if !changed {
		t.Fatal("expected resume plan to change")
	}
	if got.ResumeMode != "CROSS_RESUME" {
		t.Fatalf("mode=%s", got.ResumeMode)
	}
	if got.ResumeUnit != "S014" {
		t.Fatalf("resume=%s", got.ResumeUnit)
	}
	if len(got.SkipUnits) != 2 || got.SkipUnits[0] != "S012" || got.SkipUnits[1] != "S013" {
		t.Fatalf("skip=%#v", got.SkipUnits)
	}
}

func TestResolveDevInteractionResumeDoesNotJumpAcrossUncoveredUnits(t *testing.T) {
	plan := devInteractionPlan{
		ReplyCore:     "物流正常隔天到。",
		CoveredTopics: []string{"delivery"},
		ResumeMode:    "DIRECT",
	}
	input := devAnswerTTSInput{
		NextMainlineUnits: []devMainlineUnitInput{
			{ID: "S009", Text: "清蒸这种做法也完全没问题。", Topics: []string{"cooking"}},
			{ID: "S010", Text: "单只毛重2斤以上。", Topics: []string{"spec_state"}},
			{ID: "S011", Text: "关于物流这块。", Topics: []string{"delivery"}},
		},
	}
	got, changed := resolveDevInteractionResume(plan, input)
	if changed {
		t.Fatalf("should not jump across uncovered units: %#v", got)
	}
}

func TestResolveDevInteractionResumeInfersDeliveryFromReply(t *testing.T) {
	plan := devInteractionPlan{
		ReplyCore:  "江浙沪隔天到，其他地方两三天。",
		ResumeMode: "DIRECT",
	}
	input := devAnswerTTSInput{
		NextMainlineUnits: []devMainlineUnitInput{
			{ID: "S012", Text: "江浙沪皖正常隔天到。", Topics: []string{"delivery"}},
			{ID: "S014", Text: "如果不会处理看教程。", Topics: []string{"cooking"}},
		},
	}
	got, changed := resolveDevInteractionResume(plan, input)
	if !changed || got.ResumeUnit != "S014" || got.ResumeMode != "FUSION_SKIP" {
		t.Fatalf("got=%#v changed=%v", got, changed)
	}
}

func TestResumeTailOverlapDetectsParaphrasedTargetFacts(t *testing.T) {
	plan := devInteractionPlan{
		ResumeMode: "CROSS_RESUME",
		ResumeUnit: "S014",
		ResumeTail: "收到货后若担心做法，包装袋后有教程入口，或找客服要指导都行。",
	}
	input := devAnswerTTSInput{
		NextMainlineUnits: []devMainlineUnitInput{
			{
				ID:   "S014",
				Text: "如果拿到手不知道怎么处理，包装袋后面有教程入口，或者联系客服获取详细做法指导都可以。",
			},
		},
	}
	if !devResumeTailOverlapsTarget(plan, input) {
		t.Fatal("expected bridge overlap to be detected")
	}
	got, changed := sanitizeDevResumeTail(plan, input)
	if !changed || got.ResumeTail != "" {
		t.Fatalf("expected overlapping bridge to be removed: %#v changed=%v", got, changed)
	}
}

func TestResumeTailAllowsShortGenericTransition(t *testing.T) {
	plan := devInteractionPlan{
		ResumeMode: "CROSS_RESUME",
		ResumeUnit: "S014",
		ResumeTail: "这块大家心里有数就行。",
	}
	input := devAnswerTTSInput{
		NextMainlineUnits: []devMainlineUnitInput{
			{
				ID:   "S014",
				Text: "如果拿到手不知道怎么处理，包装袋后面有教程入口，或者联系客服获取详细做法指导都可以。",
			},
		},
	}
	if devResumeTailOverlapsTarget(plan, input) {
		t.Fatal("generic transition should not be treated as duplicate target content")
	}
}

func TestCrossResumeAllowsEmptyBridge(t *testing.T) {
	plan, err := normalizeDevInteractionPlan(devInteractionPlan{
		ReplyCore:  "江浙沪皖隔天到，其他地方两三天。",
		ResumeMode: "CROSS_RESUME",
		ResumeUnit: "S014",
		SkipUnits:  []string{"S012", "S013"},
	})
	if err != nil {
		t.Fatalf("cross resume with direct semantic continuation should allow empty bridge: %v", err)
	}
	if plan.ResumeTail != "" {
		t.Fatalf("unexpected bridge: %q", plan.ResumeTail)
	}
}
