package db

import "testing"

func TestAnchorVoiceCachePolicyRange(t *testing.T) {
	if DefaultAnchorVoiceCacheHours != 5 {
		t.Fatal("default changed")
	}
	for _, raw := range []string{"1", "5", "168", " 12 "} {
		if _, err := ParseAnchorVoiceCacheHours(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"0", "-1", "169", "forever", "5.5", ""} {
		if _, err := ParseAnchorVoiceCacheHours(raw); err == nil {
			t.Fatal("invalid policy accepted", raw)
		}
	}
}
