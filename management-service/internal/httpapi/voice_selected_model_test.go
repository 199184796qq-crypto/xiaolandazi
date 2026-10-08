package httpapi

import (
	"livecompanion/management/internal/model"
	"testing"
)

func TestSelectedCloneVoiceUsesModelBindingNotCreationDefault(t *testing.T) {
	profile := model.VoiceProfile{ID: 2, TenantID: 14, CloneStatus: "ready", VoiceID: "plus-original", Config: map[string]any{"target_model": "qwen-audio-3.0-tts-plus"}}
	binding := model.VoiceModelBinding{ID: 8, ProfileID: 2, TenantID: 14, Status: "ready", VoiceID: "flash-selected", Model: "qwen-audio-3.1-tts-flash"}
	for _, input := range []fullShowVariantVoiceInput{
		{BindingID: 8, VoiceID: binding.VoiceID, Model: binding.Model},
		{VoiceID: binding.VoiceID, Model: binding.Model},
		{BindingID: 8},
	} {
		voice, target, err := resolveSelectedCloneVoice(profile, input, []model.VoiceModelBinding{binding})
		if err != nil || voice != binding.VoiceID || target != binding.Model {
			t.Fatal(voice, target, err)
		}
	}
	voice, target, err := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{}, nil)
	if err != nil || voice != profile.VoiceID || target != "qwen-audio-3.0-tts-plus" {
		t.Fatal("legacy original identity failed", voice, target, err)
	}
	for _, input := range []fullShowVariantVoiceInput{{BindingID: 9}, {BindingID: 8, Model: "qwen-audio-3.0-tts-plus"}, {VoiceID: "missing"}, {VoiceID: profile.VoiceID, Model: binding.Model}, {Model: binding.Model}} {
		if _, _, err := resolveSelectedCloneVoice(profile, input, nil); err == nil {
			t.Fatal("silently fell back", input)
		}
	}
	for _, bad := range []model.VoiceModelBinding{
		{ID: 8, ProfileID: 3, TenantID: 14, Status: "ready", VoiceID: binding.VoiceID, Model: binding.Model},
		{ID: 8, ProfileID: 2, TenantID: 15, Status: "ready", VoiceID: binding.VoiceID, Model: binding.Model},
		{ID: 8, ProfileID: 2, TenantID: 14, Status: "disabled", VoiceID: binding.VoiceID, Model: binding.Model},
	} {
		if _, _, err := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{BindingID: 8}, []model.VoiceModelBinding{bad}); err == nil {
			t.Fatal("invalid/cross-tenant binding accepted", bad)
		}
	}
	profile.CloneStatus = "disabled"
	if _, _, err := resolveSelectedCloneVoice(profile, fullShowVariantVoiceInput{BindingID: 8}, []model.VoiceModelBinding{binding}); err == nil {
		t.Fatal("disabled voice accepted")
	}
}
