package httpapi

import (
	"testing"
	"time"
)

func TestHanCountIgnoresPunctuationAndDigits(t *testing.T) {
	text := "一整只900克，八十七块六！"
	if got := hanCount(text); got != 9 {
		t.Fatalf("汉字数应为 9，实际 %d", got)
	}
	if runeCount(text) <= hanCount(text) {
		t.Fatal("总字符数应大于汉字数")
	}
}

func TestBudgetUsesHanCount(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "100")
	planID := time.Now().UnixNano()
	recordScriptVoiceSynthesis(planID, 90)
	if !allowScriptVoiceSynthesis(planID, 10) {
		t.Fatal("刚好用完应允许")
	}
	if allowScriptVoiceSynthesis(planID, 11) {
		t.Fatal("超出应拒绝")
	}
}
