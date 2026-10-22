// Package recordingcache caches the audio units of the script-first mode.
//
// Ownership mirrors the data model: tenant -> room -> plan. Whatever can be
// deleted owns a directory, so deleting it removes exactly its own assets.
//
//	{mediaRoot}/{tenant}/{room}/{plan}/
//	    recordings/                    uploaded source audio
//	    generated_voice/               synthesized voice and archived tracks
//	    recording-cache/{voice}/{script}/
//	        manifest.json              sentence -> asset id, duration, timeline
//	        {sentence}.wav
package recordingcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Entry is one synthesized sentence unit. Only the asset id is stored, never a
// URL: playback URLs are re-signed right before they are handed to Core.
type Entry struct {
	Sentence  string     `json:"sentence"`
	Spoken    string     `json:"spoken"`
	AssetID   int64      `json:"asset_id"`
	Duration  int64      `json:"duration_ms"`
	Timeline  [][2]int64 `json:"timeline_ms"`
	SafeCuts  []int64    `json:"safe_cuts_ms"`
	UpdatedAt string     `json:"updated_at"`
}

// Manifest is the cache index for one plan + voice + script version.
type Manifest struct {
	Version    int     `json:"version"`
	VoiceKey   string  `json:"voice_key"`
	ScriptHash string  `json:"script_hash"`
	Entries    []Entry `json:"entries"`
}

// Scope locates one cache directory.
type Scope struct {
	Root     string
	TenantID int64
	RoomID   int64
	PlanID   int64
}

// ScopeFor derives the directory from validated numeric ids only.
func ScopeFor(mediaRoot string, tenantID, roomID, planID int64) (Scope, error) {
	if tenantID <= 0 || roomID < 0 || planID < 0 {
		return Scope{}, errors.New("缓存归属参数非法")
	}
	root := filepath.Join(mediaRoot, strconv.FormatInt(tenantID, 10))
	if roomID > 0 {
		root = filepath.Join(root, strconv.FormatInt(roomID, 10))
	}
	if planID > 0 {
		root = filepath.Join(root, strconv.FormatInt(planID, 10))
	}
	return Scope{Root: root, TenantID: tenantID, RoomID: roomID, PlanID: planID}, nil
}

// CacheDir returns the directory of one voice + script version.
func (s Scope) CacheDir(voiceKey, scriptHash string) string {
	return filepath.Join(s.Root, "recording-cache", safeSegment(voiceKey), safeSegment(scriptHash))
}

// safeSegment keeps the path segment to hex and letters so a value can never
// escape the directory or carry separators.
func safeSegment(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" {
		out = "default"
	}
	return out
}

// Load reads the manifest. A missing manifest returns (nil, nil) so callers
// treat it as "no cache yet".
func (s Scope) Load(voiceKey, scriptHash string) (*Manifest, error) {
	path := filepath.Join(s.CacheDir(voiceKey, scriptHash), "manifest.json")
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("缓存清单损坏: %w", err)
	}
	if manifest.VoiceKey != voiceKey || manifest.ScriptHash != scriptHash {
		return nil, nil
	}
	return &manifest, nil
}

// Save writes the manifest atomically so readers never see a partial file.
func (s Scope) Save(manifest *Manifest) error {
	dir := s.CacheDir(manifest.VoiceKey, manifest.ScriptHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("manifest.json.%d.tmp", os.Getpid()))
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "manifest.json"))
}

// WriteAudio writes one sentence audio atomically.
func (s Scope) WriteAudio(voiceKey, scriptHash, sentenceKey string, data []byte) (string, error) {
	dir := s.CacheDir(voiceKey, scriptHash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := safeSegment(sentenceKey) + ".wav"
	final := filepath.Join(dir, name)
	tmp := filepath.Join(dir, fmt.Sprintf("%s.%d.tmp", name, os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		return "", err
	}
	return final, nil
}

// Lock takes the room-level exclusive lock so a delete never races a writer.
// It uses an exclusive lock file, which works on every platform.
func (s Scope) Lock() (func(), error) {
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Root, ".lock")
	deadline := time.Now().Add(30 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d", os.Getpid())
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, errors.New("房间缓存被占用")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// RemoveCache drops only the cache layers, never the uploaded recordings.
func (s Scope) RemoveCache(voiceKey, scriptHash string) error {
	var dir string
	switch {
	case voiceKey != "" && scriptHash != "":
		dir = s.CacheDir(voiceKey, scriptHash)
	case voiceKey != "":
		dir = filepath.Join(s.Root, "recording-cache", safeSegment(voiceKey))
	default:
		dir = filepath.Join(s.Root, "recording-cache")
	}
	return removeDir(dir)
}

// RemoveAll drops the whole plan directory, recordings included.
func (s Scope) RemoveAll() error {
	return removeDir(s.Root)
}

// removeDir renames before deleting: the rename is atomic, so a process that is
// reading the directory never observes a half-deleted tree.
func removeDir(dir string) error {
	if dir == "" || dir == "/" || filepath.Dir(dir) == dir {
		return errors.New("拒绝删除空路径")
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	trash := dir + ".deleting"
	_ = os.RemoveAll(trash)
	if err := os.Rename(dir, trash); err != nil {
		return err
	}
	return os.RemoveAll(trash)
}
