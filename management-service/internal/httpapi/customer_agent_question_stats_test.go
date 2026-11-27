package httpapi

import (
	"strings"
	"testing"
)

func TestQuestionStatsInstructionShowsSkillAndRetainsNotes(t *testing.T) {
	for _, tc := range []struct {
		draft, instruction string
		ok                 bool
	}{
		{"", "", true},
		{"/system-user-question-stats", "", true},
		{"  /system-user-question-stats  重点统计配送问题", "重点统计配送问题", true},
		{"/system-user-question-stats\n重点统计规格", "重点统计规格", true},
		{"/system-user-question-stats-test", "", false},
		{"/system-live-room-intro", "", false},
		{"/system-user-question-stats " + strings.Repeat("配", 4000), "", false},
		{"/system-user-question-stats 配\x00送", "", false},
	} {
		got, ok := questionStatsInstruction(tc.draft)
		if ok != tc.ok || ok && got != tc.instruction {
			t.Fatalf("question stats instruction ok=%v, length=%d, want ok=%v", ok, len(got), tc.ok)
		}
	}
	report := agentQuestionStatsContext{RoomName: "示例直播间", StartedAt: "2026-11-24 16:52"}
	const note = "重点统计配送问题"
	want := "/system-user-question-stats 统计用户问题 · 示例直播间 · 2026-11-24 16:52\n\n" + note
	if got := questionStatsMessage(report, note); got != want {
		t.Fatalf("rendered question stats user message: %q", got)
	}
}
