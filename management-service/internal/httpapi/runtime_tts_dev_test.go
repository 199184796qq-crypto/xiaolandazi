package httpapi

import (
	"strings"
	"testing"
)

func TestDecodeDevInteractionPlan(t *testing.T) {
	plan, err := decodeDevInteractionPlan(`{"reply_core":"这个问题我给大家一起说一下。","resume_tail":"这个点说清楚了，咱们接着往下看肉质。","covered_topics":["price","price"],"covered_fact_ids":[],"skip_units":["feeding"],"resume_unit":"meat","resume_mode":"fusion_skip"}`)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ResumeMode != "FUSION_SKIP" || plan.ResumeUnit != "meat" {
		t.Fatalf("plan=%#v", plan)
	}
	if len(plan.CoveredTopics) != 1 {
		t.Fatalf("topics=%#v", plan.CoveredTopics)
	}
	if !strings.Contains(devInteractionFinalText(plan), "接着往下看肉质") {
		t.Fatalf("final=%q", devInteractionFinalText(plan))
	}
}

func TestDevInteractionPlanFallsBackWhenBridgeTextIsMissing(t *testing.T) {
	plan, err := normalizeDevInteractionPlan(devInteractionPlan{ReplyCore: "回答正文", ResumeMode: "BRIDGE"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ResumeMode != "DIRECT" || plan.ResumeTail != "" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestInvalidResumeModeFallsBack(t *testing.T) {
	plan, err := normalizeDevInteractionPlan(devInteractionPlan{ReplyCore: "回答正文", ResumeTail: "自然接回主线。", ResumeMode: "UNKNOWN"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.ResumeMode != "BRIDGE" {
		t.Fatalf("mode=%s", plan.ResumeMode)
	}
}

func TestPlannedSpeechUnitsCompensateForRate(t *testing.T) {
	if got := plannedSpeechUnits(10, 1.0); got != 56 {
		t.Fatalf("1.0x units=%d", got)
	}
	if got := plannedSpeechUnits(10, 1.2); got != 67 {
		t.Fatalf("1.2x units=%d", got)
	}
}

func TestFitInteractionPlanKeepsBridgeAndCapsUnits(t *testing.T) {
	plan := devInteractionPlan{
		ReplyCore:  "这个问题大家问得比较多，我先把最常见的做法说清楚。加热的时候温度别一下拉得太高，可以先让里面慢慢热透，再把表面做得更脆一点。这样吃起来里面不会太干，外面口感也更舒服。",
		ResumeTail: "这个点说清楚了，咱们接着往下看。",
		ResumeMode: "BRIDGE",
	}
	fitted := fitDevInteractionPlanMaxUnits(plan, 50)
	if fitted.ResumeTail == "" || !strings.Contains(fitted.ResumeTail, "接着") {
		t.Fatalf("bridge was lost: %#v", fitted)
	}
	if units := spokenUnitCount(devInteractionFinalText(fitted)); units > 50 {
		t.Fatalf("units=%d text=%q", units, devInteractionFinalText(fitted))
	}
	if fitted.ReplyCore == "" {
		t.Fatal("reply core was removed")
	}
}
