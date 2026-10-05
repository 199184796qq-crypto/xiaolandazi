package model

import (
	"strings"
	"testing"
	"time"
)

func TestComposeInventoryBatch(t *testing.T) {
	now := time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)
	for _, tt := range []struct{ prefix, suffix, legacy, want string }{
		{"", "", "", "20261002"},
		{"", " 02 ", "", "20261002-02"},
		{" 20260930 ", " A ", "ignored", "20260930-A"},
		{"", "", " 00001 ", "00001"},
		{"custom", "", "", "custom"},
	} {
		got, err := ComposeInventoryBatch(tt.prefix, tt.suffix, tt.legacy, now)
		if err != nil || got != tt.want {
			t.Fatalf("%+v: %q %v", tt, got, err)
		}
	}
	for _, bad := range []string{"a\nb", "a\x00b", strings.Repeat("批", 97)} {
		if _, err := ComposeInventoryBatch(bad, "", "", now); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
