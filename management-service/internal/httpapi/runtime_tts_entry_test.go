package httpapi

import (
	"strings"
	"testing"
)

func TestEntryModeSoftAppearsInFinalSpeech(t *testing.T) {
	plan, err := normalizeDevInteractionPlan(devInteractionPlan{
		EntryMode:  "soft",
		EntryLead:  "有朋友正好问到这个，我顺手说一下。",
		ReplyCore:  "江浙沪皖正常隔天到，其他地方两三天。",
		ResumeMode: "DIRECT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryMode != "SOFT" {
		t.Fatalf("entry mode=%s", plan.EntryMode)
	}
	if !strings.HasPrefix(devInteractionFinalText(plan), plan.EntryLead) {
		t.Fatalf("entry lead missing from final text: %q", devInteractionFinalText(plan))
	}
}

func TestEntryModeDirectClearsLead(t *testing.T) {
	plan, err := normalizeDevInteractionPlan(devInteractionPlan{
		EntryMode:  "DIRECT",
		EntryLead:  "这句不应该保留。",
		ReplyCore:  "直接回答。",
		ResumeMode: "DIRECT",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.EntryLead != "" {
		t.Fatalf("direct entry kept lead: %q", plan.EntryLead)
	}
	if devInteractionFinalText(plan) != "直接回答。" {
		t.Fatalf("final=%q", devInteractionFinalText(plan))
	}
}

func TestContinuityQualityPromptTreatsEntryLooserThanResume(t *testing.T) {
	plan := devInteractionPlan{
		EntryMode:  "HARD",
		EntryLead:  "先插一句回答下公屏这个问题。",
		ReplyCore:  "这里是回答正文。",
		ResumeMode: "DIRECT",
	}
	input := devAnswerTTSInput{
		Question:        "这个问题怎么处理？",
		CurrentMainline: "主线刚刚说完一句。",
		NextMainlineUnits: []devMainlineUnitInput{
			{ID: "S2", Text: "下一段主线。", Topics: []string{"general"}},
		},
	}
	prompt := devContinuityQualityPrompt(plan, input)
	if !strings.Contains(prompt, "切入侧比回归侧宽松") {
		t.Fatalf("quality prompt missing entry policy: %s", prompt)
	}
	if !strings.Contains(prompt, "HARD=硬接") {
		t.Fatalf("quality prompt missing hard-entry semantics: %s", prompt)
	}
}
