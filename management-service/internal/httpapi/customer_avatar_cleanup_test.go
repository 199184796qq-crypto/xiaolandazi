package httpapi

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestPurgeCustomerAvatars(t *testing.T) {
	dir := t.TempDir()
	target := []string{"user-8-a123456789abcdef.png", "user-8-b123456789abcdef.webp"}
	preserved := []string{"user-81-a123456789abcdef.png", "user-8-shared.png", "system.jpg"}
	for _, name := range append(target, preserved...) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := purgeCustomerAvatars(t.Context(), dir, 8); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range target {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("avatar still present: %s %v", name, err)
		}
	}
	for _, name := range preserved {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("unrelated file missing: %s %v", name, err)
		}
	}
}
