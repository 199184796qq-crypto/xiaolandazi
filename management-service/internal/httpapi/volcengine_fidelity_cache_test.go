package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/ttsgateway"
)

func TestVolcengineFidelityUsesSeparatePaidAudioNamespace(t *testing.T) {
	voice, text, rate := "S_source", "原文保持不变。", 1.13
	old, _ := json.Marshal([]any{"volcengine-paragraph-v1", ttsgateway.VolcengineICL2, voice, rate, text})
	oldDigest := sha256.Sum256(old)
	oldKey := hex.EncodeToString(oldDigest[:])
	newKey := volcengineParagraphKey(voice, text, rate)
	if newKey == oldKey || newKey != volcengineParagraphKey(voice, text, rate) {
		t.Fatal("new profile reused old paid audio or has unstable identity")
	}
	root := t.TempDir()
	oldPath := filepath.Join(root, oldKey, "cache.json")
	oldCache := volcengineParagraphCache{Key: oldKey, Audio: map[int]volcengineCachedAudio{100: {AssetID: 42, VoiceID: voice, DurationMS: 1000}}}
	if err := writeVolcengineJSON(oldPath, oldCache); err != nil {
		t.Fatal(err)
	}
	newCache, err := loadVolcengineParagraphCache(filepath.Join(root, newKey, "cache.json"), newKey)
	if err != nil || len(newCache.Audio) != 0 {
		t.Fatal("legacy audio was returned for new profile", err)
	}
	retained, err := loadVolcengineParagraphCache(oldPath, oldKey)
	if err != nil || retained.Audio[100].AssetID != 42 {
		t.Fatal("legacy paid source was modified", err)
	}
}
