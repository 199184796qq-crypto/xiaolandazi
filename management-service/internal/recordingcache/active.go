package recordingcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const activeHistoryMarker = "recording-cache-v3\n"

// SourceTab is the source snapshot used to prove exact per-unit inheritance.
// Context keys let the caller invalidate polished variants when their paragraph
// changes, even when one of its source sentences remains byte-for-byte equal.
type SourceTab struct {
	ID          string   `json:"tab_id"`
	Sources     []string `json:"sources"`
	UnitKeys    []string `json:"unit_keys,omitempty"`
	ContextKeys []string `json:"context_keys,omitempty"`
}

func (s Scope) activePath() string {
	return filepath.Join(s.Root, "recording-cache", "active.json")
}

func (s Scope) activeHistoryPath() string {
	// Outside recording-cache: a targeted or complete cache clear must not
	// reopen the one-time legacy-import path. RemoveAll still removes it.
	return filepath.Join(s.Root, ".recording-cache-v3")
}

func (s Scope) legacyBootstrapAllowedUnlocked() (bool, error) {
	info, err := os.Lstat(s.activeHistoryPath())
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("当前缓存历史屏障不是普通文件，不能导入旧清单")
	}
	raw, err := os.ReadFile(s.activeHistoryPath())
	if err != nil {
		return false, err
	}
	if string(raw) != activeHistoryMarker {
		return false, errors.New("当前缓存历史屏障损坏，不能导入旧清单")
	}
	return false, nil
}

// LegacyBootstrapAllowed is true only before this scope has ever claimed a V3
// active lineage. A cleared/missing active.json is not permission to resurrect
// an invalidated V2 namespace. Marker errors fail closed.
func (s Scope) LegacyBootstrapAllowed() (bool, error) {
	unlock, err := s.lockActiveIndex()
	if err != nil {
		return false, err
	}
	defer unlock()
	return s.legacyBootstrapAllowedUnlocked()
}

func (s Scope) markActiveHistoryUnlocked() error {
	allowed, err := s.legacyBootstrapAllowedUnlocked()
	if err != nil || !allowed {
		return err
	}
	file, err := os.CreateTemp(s.Root, ".recording-cache-v3-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.WriteString(activeHistoryMarker); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, s.activeHistoryPath())
}

func (s Scope) lockActiveIndex() (func(), error) {
	if strings.TrimSpace(s.Root) == "" {
		return nil, errors.New("缓存索引目录为空")
	}
	if err := os.MkdirAll(s.Root, 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(s.Root, ".cache-index.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockActiveFile(file); err != nil {
		file.Close()
		return nil, err
	}
	return func() { _ = unlockActiveFile(file); _ = file.Close() }, nil
}

func validateActive(manifest *Manifest) error {
	if manifest == nil || manifest.Version != 3 || manifest.SessionID <= 0 || strings.TrimSpace(manifest.OwnerToken) == "" ||
		strings.TrimSpace(manifest.VoiceKey) == "" || strings.TrimSpace(manifest.ScriptHash) == "" || strings.TrimSpace(manifest.CompatibilityKey) == "" {
		return errors.New("当前缓存清单身份或版本无效")
	}
	return nil
}

func (s Scope) loadActiveUnlocked() (*Manifest, error) {
	raw, err := os.ReadFile(s.activePath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("当前缓存清单损坏: %w", err)
	}
	if err := validateActive(&manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

// LoadActive reads only the owned scope. Corruption is an error, never a claim
// that no history exists or permission to bootstrap an older cache namespace.
func (s Scope) LoadActive() (*Manifest, error) {
	unlock, err := s.lockActiveIndex()
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.loadActiveUnlocked()
}

// ClaimActive atomically changes the owner after a caller has reconciled the
// observed predecessor. An older session cannot replace a newer session even
// if it subsequently obtains the newer token through another read.
func (s Scope) ClaimActive(expectedOwner string, next *Manifest) (bool, error) {
	if err := validateActive(next); err != nil {
		return false, err
	}
	unlock, err := s.lockActiveIndex()
	if err != nil {
		return false, err
	}
	defer unlock()
	current, err := s.loadActiveUnlocked()
	if err != nil {
		return false, err
	}
	if current == nil {
		if expectedOwner != "" {
			return false, nil
		}
		// The caller may have read a V2 manifest before another producer
		// claimed V3 and an operator then cleared its active pointer. Recheck
		// the durable barrier under this same lock: after any V3 history, an
		// absent pointer permits only a fresh empty index, never old entries.
		allowed, err := s.legacyBootstrapAllowedUnlocked()
		if err != nil {
			return false, err
		}
		if !allowed && len(next.Entries) > 0 {
			return false, nil
		}
	} else if current.OwnerToken != expectedOwner || next.SessionID < current.SessionID {
		return false, nil
	}
	// Write the durable no-legacy barrier first. If the subsequent active
	// update fails, a cold rebuild is safe; importing old invalidated content
	// would not be. Media files remain untouched in either case.
	if err := s.markActiveHistoryUnlocked(); err != nil {
		return false, err
	}
	return s.writeActiveUnlocked(next)
}

// SaveActive cannot reclaim ownership. All five identities must still match
// the live index; an old producer or an explicitly cleared index cannot revive
// its previous manifest merely by completing a late synthesis/archive.
func (s Scope) SaveActive(next *Manifest) (bool, error) {
	if err := validateActive(next); err != nil {
		return false, err
	}
	unlock, err := s.lockActiveIndex()
	if err != nil {
		return false, err
	}
	defer unlock()
	current, err := s.loadActiveUnlocked()
	if err != nil {
		return false, err
	}
	if current == nil || current.SessionID != next.SessionID || current.OwnerToken != next.OwnerToken ||
		current.VoiceKey != next.VoiceKey || current.ScriptHash != next.ScriptHash || current.CompatibilityKey != next.CompatibilityKey {
		return false, nil
	}
	return s.writeActiveUnlocked(next)
}

func (s Scope) writeActiveUnlocked(next *Manifest) (bool, error) {
	raw, err := json.Marshal(next)
	if err != nil {
		return false, err
	}
	dir := filepath.Dir(s.activePath())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	file, err := os.CreateTemp(dir, ".active-*.tmp")
	if err != nil {
		return false, err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err := os.Rename(temporary, s.activePath()); err != nil {
		return false, err
	}
	return true, nil
}
