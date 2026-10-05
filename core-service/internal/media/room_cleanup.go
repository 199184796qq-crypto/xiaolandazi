package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ClearRoom releases its ffmpeg slot immediately, rather than waiting for the
// idle reaper while a deleted room's old browser keeps polling its media URL.
func (m *Manager) ClearRoom(ctx context.Context, roomID int64) error {
	if roomID <= 0 || strings.TrimSpace(m.root) == "" {
		return fmt.Errorf("invalid room media cleanup path")
	}
	m.mu.Lock()
	if m.deletedRooms == nil {
		m.deletedRooms = make(map[int64]struct{})
	}
	m.deletedRooms[roomID] = struct{}{}
	current := m.sessions[roomID]
	delete(m.sessions, roomID)
	m.mu.Unlock()
	stopSession(current)
	if current != nil && current.done != nil {
		select {
		case <-current.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	root, err := filepath.Abs(m.root)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, fmt.Sprintf("room-%d", roomID))
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("room cache path escapes media root")
	}
	return os.RemoveAll(dir)
}
