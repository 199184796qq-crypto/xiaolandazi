package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type bindingStore struct {
	mu   sync.RWMutex
	path string
	data map[string]int64
}

func newBindingStore(path string) (*bindingStore, error) {
	s := &bindingStore{path: strings.TrimSpace(path), data: make(map[string]int64)}
	if s.path == "" {
		s.path = defaultBindingsFile
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return s, nil
	}
	var decoded map[string]int64
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode bindings: %w", err)
	}
	for deviceID, roomID := range decoded {
		deviceID = normalizeDeviceID(deviceID)
		if deviceID != "" && roomID > 0 {
			s.data[deviceID] = roomID
		}
	}
	return s, nil
}

func (s *bindingStore) room(deviceID string) (int64, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roomID, ok := s.data[normalizeDeviceID(deviceID)]
	return roomID, ok
}

func (s *bindingStore) snapshot() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int64, len(s.data))
	for key, value := range s.data {
		out[key] = value
	}
	return out
}

func (s *bindingStore) bind(deviceID string, roomID int64) error {
	deviceID = normalizeDeviceID(deviceID)
	if deviceID == "" {
		return errors.New("device_id is required")
	}
	if roomID <= 0 {
		return errors.New("room_id must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[deviceID] = roomID
	return s.persistLocked()
}

func (s *bindingStore) unbind(deviceID string) error {
	deviceID = normalizeDeviceID(deviceID)
	if deviceID == "" {
		return errors.New("device_id is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, deviceID)
	return s.persistLocked()
}

func (s *bindingStore) persistLocked() error {
	parent := filepath.Dir(s.path)
	if parent != "." {
		if err := os.MkdirAll(parent, 0750); err != nil {
			return err
		}
	}
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0640); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func normalizeDeviceID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
