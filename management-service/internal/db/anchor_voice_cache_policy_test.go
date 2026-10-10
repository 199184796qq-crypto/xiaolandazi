package db

import (
	"livecompanion/management/internal/model"
	"testing"
)

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

func TestGenerationCacheReuseSettingRejectsAmbiguousValues(t *testing.T) {
	for _, key := range []string{model.MainlineGenerationCacheReuseSetting, model.InteractionGenerationCacheReuseSetting} {
		for _, raw := range []string{"true", "false"} {
			if err := ValidateGenerationCacheReuseSetting(key, raw); err != nil {
				t.Fatal(err)
			}
		}
		for _, raw := range []string{"0", "1", "yes", "False", "", " false "} {
			if ValidateGenerationCacheReuseSetting(key, raw) == nil {
				t.Fatal("accepted ambiguous switch", raw)
			}
		}
	}
}
