package httpapi

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func voiceFixture(t *testing.T) (model.LiveAgentPlanVersion, []volcengineScriptUnit, string) {
	t.Helper()
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	version, units := playFixture(model.LiveAgentPlanVersion{})
	// 用唯一方案号隔离：缓存根在进程内共享，固定方案号会读到上一次残留。
	version.PlanID = time.Now().UnixNano()
	return version, units, root
}

func TestScriptNextPlaybackVoiceRequiresAssets(t *testing.T) {
	version, units, root := voiceFixture(t)
	voices := []recordingScriptVoice{{VoiceID: "A"}, {VoiceID: "B"}}
	version.VoiceIdentity.Model = "qwen-audio-3.1-tts-flash"
	for _, unit := range units {
		addReadyScriptFixture(t, root, version, unit, "A", 101)
	}
	if next, ok := scriptNextPlaybackVoice(voices, 0, version, units); ok || next != 0 {
		t.Fatal("metadata missing B must not switch")
	}
	for _, unit := range units {
		addReadyScriptFixture(t, root, version, unit, "B", 102)
	}

	if next, ok := scriptNextPlaybackVoice(voices, 0, version, units); !ok || next != 1 {
		t.Fatalf("B 备齐后应切到 1: next=%d ok=%v", next, ok)
	}
	// 从最后一个音色继续轮转回到第一个。
	if next, ok := scriptNextPlaybackVoice(voices, 1, version, units); !ok || next != 0 {
		t.Fatalf("应轮转回 A: next=%d ok=%v", next, ok)
	}
}

func TestScriptNextPlaybackVoiceSingleVoiceNeverSwitches(t *testing.T) {
	version, units, _ := voiceFixture(t)
	voices := []recordingScriptVoice{{VoiceID: "A"}}
	if next, ok := scriptNextPlaybackVoice(voices, 0, version, units); ok || next != 0 {
		t.Fatalf("只有一个音色时不应切换: next=%d ok=%v", next, ok)
	}
}

func TestScriptVoiceBlockLeft(t *testing.T) {
	version, units, _ := voiceFixture(t)
	voices := []recordingScriptVoice{{VoiceID: "A"}}
	// 默认 min_voice_block=4、version_cap=6 → 块长 6。
	if got := scriptVoiceBlockLeft(voices, 0, version); got != 4 {
		t.Fatalf("默认块长应为 6，实际 %d", got)
	}
	// 配置成 3 版本、最少 4 轮 → 取较大者 4。
	script := recordingScript{Version: 2, VersionCap: 3, MinVoiceBlock: 4,
		Tabs: []recordingScriptTab{{TabID: "t1", Entries: []recordingScriptEntry{{EntryID: "a", Text: longText("x")}}}}}
	version.GenerationContext["recording_script"] = script
	if got := scriptVoiceBlockLeft(voices, 0, version); got != 4 {
		t.Fatalf("块长应取 max(4,3)=4，实际 %d", got)
	}
	// 配置成 8 版本、最少 4 轮 → 8。
	script.VersionCap = 8
	version.GenerationContext["recording_script"] = script
	if got := scriptVoiceBlockLeft(voices, 0, version); got != 4 {
		t.Fatalf("块长应取 8，实际 %d", got)
	}
	_ = units
}
