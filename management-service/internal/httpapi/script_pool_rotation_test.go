package httpapi

import (
	"math/rand"
	"testing"
	"time"

	"livecompanion/management/internal/scriptplay"
)

// 版本池满 6 个后：一轮内不重复，跨轮按 last_played_at 轮转。
func TestScriptPoolRotatesAllVersions(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	planID := time.Now().UnixNano()
	const voice = "voice-a"
	var refs []string
	for i := 1; i <= 6; i++ {
		ref := string(rune('0' + i))
		refs = append(refs, ref)
		version := scriptplay.Version{VoiceID: voice, Model: "qwen", Kind: scriptplay.KindRewrite, AudioRef: ref, Fingerprint: "fp-" + ref, Text: ref + "号文本"}
		version.Seq = i
		if err := addScriptVoiceVersion(recordingCacheMediaRoot(), planID, "piece", version); err != nil {
			t.Fatal(err)
		}
	}
	rng := rand.New(rand.NewSource(1))
	played := map[string]bool{}
	for i := 0; i < 6; i++ {
		pick, ok := scriptPoolPickVersion(recordingCacheMediaRoot(), planID, "piece", voice, "", rng)
		if !ok {
			t.Fatalf("第 %d 次没取到版本", i+1)
		}
		if played[pick.AudioRef] {
			t.Fatalf("一轮内重复播放 %s", pick.AudioRef)
		}
		played[pick.AudioRef] = true
		markScriptVersionPlayed(recordingCacheMediaRoot(), planID, "piece", pick.AudioRef)
	}
	if len(played) != len(refs) {
		t.Fatalf("一轮应播完 %d 个版本，实际 %d", len(refs), len(played))
	}
}
