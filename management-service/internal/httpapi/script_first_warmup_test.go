package httpapi

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"livecompanion/management/internal/ttsgateway"
)

// 方案级必须落在 plans 段下，不与任何房间号撞名。
func TestPlanCacheDirUsesPlansSegment(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MEDIA_ROOT", root)
	dir, ok := planCacheDir(14, 4, "qwen-audio-3.1-tts-flash")
	if !ok {
		t.Fatal("方案级目录应可用")
	}
	want := filepath.Join(root, "14", "plans", "4", "recording-cache", "script-first-v1", planWarmupCacheName)
	if dir != want {
		t.Fatalf("方案级目录 %s，期望 %s", dir, want)
	}
	// 房间级是 <root>/<tenant>/<room>/<plan>/...，两者不能互相包含。
	roomScope, ok := recordingCacheScopeFor(14, 29, 4)
	if !ok {
		t.Fatal("房间级 scope 应可用")
	}
	roomDir := roomScope.CacheDir("script-first-v1", "paragraphs")
	if hasPrefix(dir, roomDir) || hasPrefix(roomDir, dir) {
		t.Fatalf("方案级 %s 与房间级 %s 不应互相包含", dir, roomDir)
	}
	if _, ok := planCacheDir(0, 4, "x"); ok {
		t.Fatal("非法租户应拒绝")
	}
	if _, ok := planCacheDir(14, 0, "x"); ok {
		t.Fatal("非法方案应拒绝")
	}
}

func TestScriptFirstNamespaceByProvider(t *testing.T) {
	if got := scriptFirstNamespace(ttsgateway.VolcengineICL2); got != "volcengine-longtext-v1" {
		t.Fatalf("火山命名空间错误: %s", got)
	}
	if got := scriptFirstNamespace("qwen-audio-3.1-tts-flash"); got != "script-first-v1" {
		t.Fatalf("阿里命名空间错误: %s", got)
	}
}

// 只复制 wav 与 json，其它杂物不进房间缓存。
func TestCopyDirKeepsOnlyAudioAndMetadata(t *testing.T) {
	source := t.TempDir()
	target := filepath.Join(t.TempDir(), "unit")
	for name, body := range map[string]string{
		"paragraph.wav":      "wav",
		"paragraph-job.json": "{}",
		"notes.tmp":          "junk",
		"cache.json":         "{}",
	} {
		if err := os.WriteFile(filepath.Join(source, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyDir(source, target); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"paragraph.wav", "paragraph-job.json", "cache.json"} {
		if _, err := os.Stat(filepath.Join(target, want)); err != nil {
			t.Fatalf("缺少 %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "notes.tmp")); err == nil {
		t.Fatal("杂物不该被复制")
	}
}

// 复制必须是原子的：先写临时目录再整体改名，不留半份。
func TestCopyDirAtomicLeavesNoTemporary(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "paragraph.wav"), []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unit")
	if err := copyDirAtomic(source, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "paragraph.wav")); err != nil {
		t.Fatalf("目标目录缺少音频: %v", err)
	}
	if _, err := os.Stat(target + ".warmup-tmp"); err == nil {
		t.Fatal("临时目录应已清理")
	}
}

// 已有目录属于原房间恢复路径，连未确认的付费状态也不能被覆盖。
func TestCopyDirAtomicPreservesExistingTarget(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "paragraph.wav"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "unit")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "stale.json"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copyDirAtomic(source, target); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected existing-target refusal, got %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(target, "stale.json")); err != nil || string(data) != "old" {
		t.Fatal("existing room state changed", err)
	}
	if _, err := os.Stat(filepath.Join(target, "paragraph.wav")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("source overwrote existing target")
	}
}

func hasPrefix(path, prefix string) bool {
	rel, err := filepath.Rel(prefix, path)
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && len(rel) > 0 && rel[0] != '.'
}
