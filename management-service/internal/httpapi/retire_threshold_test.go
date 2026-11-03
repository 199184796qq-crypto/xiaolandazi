package httpapi

import (
	"testing"
	"time"

	"livecompanion/management/internal/scriptplay"
)

// 满池后：没播够 4 次的版本不能被淘汰；播够 4 次后淘汰最久没播的那条。
func TestRetireRequiresFourPlays(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	planID := time.Now().UnixNano()
	const voice = "voice-a"
	const piece = "piece"
	add := func(ref string, plays int, last string) {
		v := scriptplay.Version{VoiceID: voice, Model: "qwen", Kind: scriptplay.KindRewrite, AudioRef: ref, Fingerprint: "fp-" + ref, Text: ref}
		v.PlayCount = plays
		v.LastPlayedAt = last
		v.Seq = plays + 1
		if err := addScriptVoiceVersion(recordingCacheMediaRoot(), planID, piece, v); err != nil {
			t.Fatal(err)
		}
	}
	add("a", 1, "2026-11-03T00:00:01+08:00")
	add("b", 4, "2026-11-03T00:00:02+08:00")
	add("c", 9, "2026-11-03T00:00:03+08:00")

	pool, err := loadScriptVoicePool(recordingCacheMediaRoot(), planID, piece)
	if err != nil {
		t.Fatal(err)
	}
	if !hasRetirableVersion(pool, voice, 4) {
		t.Fatal("有播够 4 次的版本，应该可以被换掉")
	}
	dropOldestScriptVersion(planID, piece, voice, 4)

	after, err := loadScriptVoicePool(recordingCacheMediaRoot(), planID, piece)
	if err != nil {
		t.Fatal(err)
	}
	remaining := map[string]bool{}
	for _, v := range after.VoiceVersions(voice) {
		remaining[v.AudioRef] = true
	}
	if !remaining["a"] {
		t.Fatal("播不够次数的版本不该被淘汰")
	}
	if remaining["b"] {
		t.Fatal("播够 4 次且最久没播的版本应被淘汰")
	}
}
