package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalVoiceCleanupRejectsLinkedParent(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	store, e := NewLocal(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(outside, filepath.Join(root, "linked")); e != nil {
		t.Skip("symlink unavailable")
	}
	if e = store.Put(context.Background(), "linked/paid.wav", strings.NewReader("keep"), "audio/wav"); e == nil {
		t.Fatal("write escaped")
	}
	if e = store.Delete(context.Background(), "linked/paid.wav"); e == nil {
		t.Fatal("delete followed linked parent")
	}
}
