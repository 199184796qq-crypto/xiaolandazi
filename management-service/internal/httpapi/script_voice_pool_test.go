package httpapi

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"

	"livecompanion/management/internal/scriptplay"
)

func TestScriptVoicePoolRoundTrip(t *testing.T) {
	root := t.TempDir()
	pool, err := loadScriptVoicePool(root, 7, "piece-1")
	if err != nil || len(pool.Versions) != 0 {
		t.Fatalf("空目录应返回空池: %v %v", pool, err)
	}
	// 第一个版本：原始。
	if err := addScriptVoiceVersion(root, 7, "piece-1", scriptplay.Version{
		ID: "v1", Fingerprint: "f1", VoiceID: "A", Model: "qwen", Volume: 70, Kind: scriptplay.KindOriginal, AudioRef: "a1",
	}); err != nil {
		t.Fatal(err)
	}
	// 第二个版本：已有原始，应该是改写。
	pool, err = loadScriptVoicePool(root, 7, "piece-1")
	if err != nil {
		t.Fatal(err)
	}
	if pool.NextKind("A") != scriptplay.KindRewrite {
		t.Fatal("已有原始后下一次应是改写")
	}
	if err := addScriptVoiceVersion(root, 7, "piece-1", scriptplay.Version{
		ID: "v2", Fingerprint: "f1", VoiceID: "A", Model: "qwen", Volume: 70, Kind: scriptplay.KindRewrite, AudioRef: "a2",
	}); err != nil {
		t.Fatal(err)
	}
	pool, _ = loadScriptVoicePool(root, 7, "piece-1")
	if got := pool.VoiceVersions("A"); len(got) != 2 || got[1].Seq != 2 {
		t.Fatalf("版本序号不对: %+v", got)
	}
}

func TestScriptVoicePoolInvalidateOnFingerprintChange(t *testing.T) {
	root := t.TempDir()
	for _, v := range []scriptplay.Version{
		{ID: "v1", Fingerprint: "old", VoiceID: "A", Model: "qwen", Kind: scriptplay.KindOriginal, AudioRef: "a1"},
		{ID: "v2", Fingerprint: "old", VoiceID: "A", Model: "qwen", Kind: scriptplay.KindRewrite, AudioRef: "a2"},
	} {
		if err := addScriptVoiceVersion(root, 9, "p", v); err != nil {
			t.Fatal(err)
		}
	}
	stale, err := invalidateScriptVoicePool(root, 9, "p", "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 2 {
		t.Fatalf("指纹变了应失效两个版本，实际 %d", len(stale))
	}
	pool, _ := loadScriptVoicePool(root, 9, "p")
	if len(pool.Versions) != 0 {
		t.Fatalf("失效后池子应为空，实际 %d", len(pool.Versions))
	}
	// 指纹没变：不动。
	if stale, err := invalidateScriptVoicePool(root, 9, "p", "new"); err != nil || len(stale) != 0 {
		t.Fatalf("空池不该产生失效: %v %v", stale, err)
	}
}

func TestScriptVoicePoolSeparatesVoices(t *testing.T) {
	root := t.TempDir()
	for _, v := range []scriptplay.Version{
		{ID: "a1", Fingerprint: "f", VoiceID: "A", Model: "qwen", Kind: scriptplay.KindOriginal, AudioRef: "x"},
		{ID: "b1", Fingerprint: "f", VoiceID: "B", Model: "volc", Kind: scriptplay.KindOriginal, AudioRef: "y"},
	} {
		if err := addScriptVoiceVersion(root, 3, "p2", v); err != nil {
			t.Fatal(err)
		}
	}
	pool, _ := loadScriptVoicePool(root, 3, "p2")
	if len(pool.VoiceVersions("A")) != 1 || len(pool.VoiceVersions("B")) != 1 {
		t.Fatalf("两个音色应各有一个版本: %+v", pool.Versions)
	}
	if pool.NextKind("B") != scriptplay.KindRewrite {
		t.Fatal("B 已有原始，下一次应是改写")
	}
	if pool.NextKind("C") != scriptplay.KindOriginal {
		t.Fatal("新音色第一次应是原始")
	}
}

func TestScriptVoicePoolDedupesSameAudio(t *testing.T) {
	root := t.TempDir()
	planID := int64(77)
	version := model.LiveAgentPlanVersion{}
	version.VoiceIdentity.VoiceID = "voice-a"
	version.VoiceIdentity.Model = "qwen"
	unit := volcengineScriptUnit{Key: "piece-key", Text: longText("同一段话")}
	audio := volcengineCachedAudio{AssetID: 12345, DurationMS: 90000}

	// 同一段音频登记两次：播放复用时也会走到这里，只能留一条，且不能变成改写。
	recordScriptVoiceVersion(root, planID, unit, version, audio)
	recordScriptVoiceVersion(root, planID, unit, version, audio)

	pool, err := loadScriptVoicePool(root, planID, unit.Key)
	if err != nil {
		t.Fatal(err)
	}
	versions := pool.VoiceVersions("voice-a")
	if len(versions) != 1 {
		t.Fatalf("同一段音频应只登记一次，实际 %d 条: %+v", len(versions), versions)
	}
	if versions[0].Kind != scriptplay.KindOriginal || versions[0].Seq != 1 {
		t.Fatalf("应保留一条原始版本，实际 kind=%s seq=%d", versions[0].Kind, versions[0].Seq)
	}
	// 文本一样、只是又合成了音频：仍然算同一段内容，不再登记。
	recordScriptVoiceVersion(root, planID, unit, version, volcengineCachedAudio{AssetID: 67890, DurationMS: 91000})
	pool, _ = loadScriptVoicePool(root, planID, unit.Key)
	if got := pool.VoiceVersions("voice-a"); len(got) != 1 {
		t.Fatalf("文本指纹相同不应新增版本: %+v", got)
	}
	// 文本不一样（改写后的正文）才是新版本。
	rewritten := volcengineScriptUnit{Key: "piece-key", Text: longText("这是改写后完全不同的说法")}
	recordScriptVoiceVersion(root, planID, rewritten, version, volcengineCachedAudio{AssetID: 99999, DurationMS: 92000})
	pool, _ = loadScriptVoicePool(root, planID, unit.Key)
	if got := pool.VoiceVersions("voice-a"); len(got) != 2 || got[1].Kind != scriptplay.KindOriginal {
		t.Fatalf("保存新原文不能误标成模型改写: %+v", got)
	}
}
func TestPruneScriptVoicePoolsRetainsPaidPieces(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	root := recordingCacheMediaRoot()
	planID := time.Now().UnixNano()

	keepUnit := volcengineScriptUnit{Key: "keep-key", Text: "当前话术"}
	staleUnit := volcengineScriptUnit{Key: "stale-key", Text: "已经删掉的话术"}
	for _, unit := range []volcengineScriptUnit{keepUnit, staleUnit} {
		if err := addScriptVoiceVersion(root, planID, unit.Key, scriptplay.Version{
			ID: unit.Key, Fingerprint: "f", VoiceID: "voice-a", Model: "qwen", Kind: scriptplay.KindOriginal, AudioRef: "1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	pruneScriptVoicePools(planID, []volcengineScriptUnit{keepUnit})

	if _, err := loadScriptVoicePool(root, planID, "keep-key"); err != nil {
		t.Fatalf("当前片段不应被删: %v", err)
	}
	stale, err := loadScriptVoicePool(root, planID, "stale-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(stale.Versions) != 1 {
		t.Fatalf("启动不得删除旧快照可能引用的片段，实际还有 %d 个版本", len(stale.Versions))
	}
}
