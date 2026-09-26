package httpapi

import (
	"strings"
	"testing"
)

func TestFatalQualityCannotPassEvenWithHighModelScore(t *testing.T) {
	quality, err := decodeDevContinuityQuality(`{"score":92,"fatal":true,"fatal_reason":"误解了用户问题意图","summary":"前后接续自然，但回答答错了问题。","issues":["把娃娃误解成玩具"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if quality.Score != 59 {
		t.Fatalf("score=%d want=59", quality.Score)
	}
	if devContinuityQualityPass(quality) {
		t.Fatal("fatal semantic error unexpectedly passed quality gate")
	}
	if !strings.Contains(quality.FatalReason, "意图") {
		t.Fatalf("fatal reason=%q", quality.FatalReason)
	}
}

func TestQualityPromptMakesIntentCorrectnessFirstGate(t *testing.T) {
	prompt := devContinuityQualityPrompt(devInteractionPlan{
		ReplyCore:  "鸡是拿来吃的，不是拿来玩的娃娃。",
		ResumeMode: "DIRECT",
	}, devAnswerTTSInput{
		Question:        "娃娃能吃吗？",
		CurrentMainline: "这只童子鸡已经切块处理。",
	})
	for _, want := range []string{
		`"question":"娃娃能吃吗？"`,
		`"generated_reply_core":"鸡是拿来吃的，不是拿来玩的娃娃。"`,
		`"mainline_before_stop":"这只童子鸡已经切块处理。"`,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("quality prompt missing %q", want)
		}
	}
}
