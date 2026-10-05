package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"livecompanion/core/internal/collector"
	"livecompanion/core/internal/model"
)

func TestDeletedRoomMediaCacheIsReleasedAndLateStartRejected(t *testing.T) {
	root := t.TempDir()
	deleted := filepath.Join(root, "room-15")
	other := filepath.Join(root, "room-16")
	for _, dir := range []string{deleted, other} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "segment.ts"), []byte("cached media"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := &Manager{root: root, sessions: map[int64]*session{}}
	if err := m.ClearRoom(context.Background(), 15); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(deleted); !os.IsNotExist(err) {
		t.Fatal("deleted room media cache remains")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("cleanup touched another room cache")
	}
	if _, err := m.ensureSession(model.Room{ID: 15}, collector.StreamSource{}); err == nil {
		t.Fatal("late preview request resurrected deleted room")
	}
	if err := m.ClearRoom(context.Background(), 15); err != nil {
		t.Fatal("idempotent cleanup failed", err)
	}
}
