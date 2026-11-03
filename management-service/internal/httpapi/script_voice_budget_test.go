package httpapi

import (
	"testing"
	"time"
)

func TestScriptVoiceBudgetBlocksOverLimit(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	t.Setenv("XIAOLAN_SCRIPT_VOICE_HOURLY_CHARS", "1000")
	planID := time.Now().UnixNano()

	if !allowScriptVoiceSynthesis(planID, 800) {
		t.Fatal("预算内应允许")
	}
	recordScriptVoiceSynthesis(planID, 800)
	if allowScriptVoiceSynthesis(planID, 300) {
		t.Fatal("超出预算应拒绝")
	}
	if left := scriptVoiceBudgetLeft(planID); left != 200 {
		t.Fatalf("剩余应为 200，实际 %d", left)
	}
	if !allowScriptVoiceSynthesis(planID, 200) {
		t.Fatal("刚好用完应允许")
	}
}
