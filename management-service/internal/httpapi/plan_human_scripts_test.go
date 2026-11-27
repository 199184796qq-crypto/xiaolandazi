package httpapi

import (
	"testing"

	"livecompanion/management/internal/speechasr"
)

func TestPlanHumanScriptSRT(t *testing.T) {
	result := speechasr.Result{
		Text: "你好。欢迎光临。",
		Sentences: []speechasr.Sentence{
			{BeginTime: 150, EndTime: 750, Text: "你好。"},
			{BeginTime: 1045, EndTime: 2012, Text: "欢迎光临。"},
		},
	}
	want := "1\n00:00:00,150 --> 00:00:00,750\n你好。\n\n2\n00:00:01,045 --> 00:00:02,012\n欢迎光临。"
	got, err := planHumanScriptSRT(result, 2500)
	if err != nil || got != want {
		t.Fatalf("SRT = %q, err = %v", got, err)
	}
	for _, invalid := range []string{"", "旧版文本", "1\n00:00:01,000 --> 00:00:00,000\n结束时间提前", "1\n00:74:00,000 --> 00:00:01,000\n分钟溢出"} {
		if validPlanHumanScriptSRT(invalid) {
			t.Fatalf("accepted invalid SRT %q", invalid)
		}
	}
	if legacy := planHumanScriptTranscript(map[string]any{"script_transcript": "旧版手工文稿"}); legacy["format"] != "text" {
		t.Fatal(legacy)
	}
}
