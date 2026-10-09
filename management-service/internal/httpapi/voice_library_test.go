package httpapi

import (
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"net/http/httptest"
	"testing"
)

func TestVoiceLibrariesKeepLegacyQwenAndCosyModelsSeparate(t *testing.T) {
	profiles := []model.VoiceProfile{
		{ID: 1, Config: map[string]any{"target_model": qwenCloneModelPlus}},
		{ID: 2, Config: map[string]any{"target_model": ttsgateway.CosyVoiceFlash}},
		{ID: 3, Config: map[string]any{"target_model": ttsgateway.CosyVoicePlus}},
		{ID: 4},
	}
	for _, tc := range []struct {
		library string
		ids     []int64
	}{{"", []int64{1, 2, 3, 4}}, {ttsgateway.ProviderQwen, []int64{1, 4}}, {ttsgateway.CosyVoiceFlash, []int64{2}}, {ttsgateway.CosyVoicePlus, []int64{3}}} {
		got := filterVoiceLibrary(tc.library, profiles)
		if len(got) != len(tc.ids) {
			t.Fatalf("%s leaked profiles: %v", tc.library, got)
		}
		for i, p := range got {
			if p.ID != tc.ids[i] {
				t.Fatal("wrong profile library", got)
			}
		}
	}
	for _, pair := range [][2]string{{qwenCloneModelPlus, ttsgateway.CosyVoiceFlash}, {ttsgateway.CosyVoiceFlash, ttsgateway.CosyVoicePlus}, {ttsgateway.CosyVoicePlus, qwenCloneModelFlash}} {
		if validateVoiceLibraryBinding(model.VoiceProfile{Config: map[string]any{"target_model": pair[0]}}, pair[1]) == nil {
			t.Fatal("allowed cross-model voice ID", pair)
		}
	}
	if err := validateVoiceLibraryBinding(profiles[0], qwenCloneModelFlash); err != nil {
		t.Fatal("legacy Qwen per-model cloning regressed", err)
	}
	for _, target := range []string{ttsgateway.CosyVoiceFlash, ttsgateway.CosyVoicePlus, "qwen-audio-3.0-tts-flash"} {
		if actual, err := normalizeVoiceCloneModel(target); err != nil || actual != target {
			t.Fatal(actual, err)
		}
	}
}

func TestCosyVoiceLibraryCannotPublishBindingFromAnotherModel(t *testing.T) {
	profile := model.VoiceProfile{ID: 1, TenantID: 14, CloneStatus: "ready", VoiceID: "flash-voice", Config: map[string]any{"target_model": ttsgateway.CosyVoiceFlash}}
	wrong := model.VoiceModelBinding{ID: 2, TenantID: 14, ProfileID: 1, Status: "ready", VoiceID: "plus-voice", Model: ttsgateway.CosyVoicePlus}
	if _, _, err := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{BindingID: 2}, []model.VoiceModelBinding{wrong}); err == nil {
		t.Fatal("allowed another model's voice")
	}
	r := httptest.NewRequest("GET", "/api/v1/live/official-voices?model=unknown", nil)
	w := httptest.NewRecorder()
	if _, ok := voiceLibraryQuery(w, r); ok || w.Code != 400 {
		t.Fatal("invalid library accepted")
	}
}
