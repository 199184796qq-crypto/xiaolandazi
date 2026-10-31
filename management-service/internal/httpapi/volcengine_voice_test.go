package httpapi

import (
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"testing"
)

func TestVolcengineExistingVoiceIdentity(t *testing.T) {
	input := model.VoiceProfileInput{Name: "已有主播", Provider: ttsgateway.ProviderVolcengine, VoiceID: "S_existing", Config: map[string]any{"target_model": ttsgateway.VolcengineICL2, "api_key": "must-be-discarded"}}
	if err := validateVolcengineImportedVoice(&input); err != nil {
		t.Fatal(err)
	}
	if input.Config["api_key"] != nil || input.CloneStatus != "ready" || input.SampleAssetID != nil {
		t.Fatal("import retained credentials or invented sample")
	}
	p := model.VoiceProfile{ID: 12, TenantID: 14, Name: input.Name, Provider: input.Provider, VoiceID: input.VoiceID, CloneStatus: input.CloneStatus, Config: input.Config}
	v, m, err := resolveSelectedCloneVoice(p, fullShowVariantVoiceInput{Source: "clone", ProfileID: p.ID, VoiceID: p.VoiceID, Model: ttsgateway.VolcengineICL2}, nil)
	if err != nil || v != p.VoiceID || m != ttsgateway.VolcengineICL2 {
		t.Fatal("import not selectable without fake clone binding", err)
	}
	if err := validateVoiceLibraryBinding(p, ttsgateway.CosyVoiceFlash); err == nil {
		t.Fatal("imported voice silently reassigned")
	}
	if ttsgateway.VoiceLibraryMatches(ttsgateway.ProviderQwen, ttsgateway.VolcengineICL2) {
		t.Fatal("import mixed with Aliyun library")
	}
}

func TestVolcengineSupportEntryCannotBypassImport(t *testing.T) {
	for _, input := range []model.VoiceProfileInput{
		{Name: "伪装", Provider: "qwen_audio", VoiceID: "S_other", Config: map[string]any{"target_model": " seed-icl-2.0 "}},
		{Name: "伪装", Provider: "volcengine", VoiceID: "S_other"},
	} {
		if validateSupportVoiceInput(&input) {
			t.Fatal("old support entry bypassed admin import")
		}
	}
}
