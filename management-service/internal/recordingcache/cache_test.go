package recordingcache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopeForRejectsInvalidIDs(t *testing.T) {
	if _, err := ScopeFor(t.TempDir(), 0, 1, 1); err == nil {
		t.Fatal("tenant 0 must be rejected")
	}
	if _, err := ScopeFor(t.TempDir(), 14, -1, 1); err == nil {
		t.Fatal("negative room must be rejected")
	}
}

func TestCacheDirSanitizesSegments(t *testing.T) {
	root := t.TempDir()
	scope, err := ScopeFor(root, 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	dir := scope.CacheDir("../../etc", "aa/bb")
	if filepath.Dir(dir) == dir {
		t.Fatal("cache dir must not collapse to root")
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel[0] == '.' {
		t.Fatalf("cache dir escaped root: %s", dir)
	}
	if got := filepath.Base(dir); got == ".." || got == "." {
		t.Fatalf("bad segment survived: %s", got)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	scope, err := ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	manifest := &Manifest{Version: 1, VoiceKey: "v1", ScriptHash: "s1", Entries: []Entry{
		{Sentence: "第一句。", Spoken: "第一句。", AssetID: 42, Duration: 1200},
	}}
	if err := scope.Save(manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := scope.Load("v1", "s1")
	if err != nil || loaded == nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].AssetID != 42 {
		t.Fatalf("unexpected manifest: %+v", loaded)
	}
	// 另一个话术版本读不到别人的清单。
	if other, err := scope.Load("v1", "s2"); err != nil || other != nil {
		t.Fatalf("other script must miss: %v %+v", err, other)
	}
}

func TestWriteAudioIsAtomic(t *testing.T) {
	scope, err := ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	path, err := scope.WriteAudio("v1", "s1", "sent1", []byte("RIFFtest"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "RIFFtest" {
		t.Fatalf("audio not written: %v", err)
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp")); len(matches) > 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestRemoveCacheKeepsRecordings(t *testing.T) {
	root := t.TempDir()
	scope, err := ScopeFor(root, 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	recordings := filepath.Join(scope.Root, "recordings")
	if err := os.MkdirAll(recordings, 0o755); err != nil {
		t.Fatal(err)
	}
	recording := filepath.Join(recordings, "source.wav")
	if err := os.WriteFile(recording, []byte("source"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.WriteAudio("v1", "s1", "sent1", []byte("RIFF")); err != nil {
		t.Fatal(err)
	}

	if err := scope.RemoveCache("v1", "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(recording); err != nil {
		t.Fatal("uploaded recording must survive a cache purge")
	}
	if _, err := os.Stat(scope.CacheDir("v1", "s1")); !os.IsNotExist(err) {
		t.Fatal("cache dir must be gone")
	}
}

func TestRemoveAllDropsWholeScope(t *testing.T) {
	scope, err := ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scope.WriteAudio("v1", "s1", "sent1", []byte("RIFF")); err != nil {
		t.Fatal(err)
	}
	if err := scope.RemoveAll(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scope.Root); !os.IsNotExist(err) {
		t.Fatal("plan dir must be gone")
	}
}

func TestPurgeFilesRefusesEscapingKeys(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(outside)

	result, err := PurgeFiles(root, []MediaEntry{{ObjectKey: "../secret.txt"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 0 {
		t.Fatalf("must not delete outside the root: %+v", result)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("outside file was deleted")
	}
}

func TestPurgeFilesCountsMissing(t *testing.T) {
	root := t.TempDir()
	result, err := PurgeFiles(root, []MediaEntry{{ObjectKey: "14/29/5/missing.wav"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Missing != 1 || result.Removed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestLockIsExclusive(t *testing.T) {
	scope, err := ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	release, err := scope.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(scope.Root, ".lock")); err != nil {
		t.Fatal(err)
	}
	second, err := scope.Lock()
	if err != nil {
		t.Fatalf("lock should be re-acquirable after removal: %v", err)
	}
	second()
	release()
}
