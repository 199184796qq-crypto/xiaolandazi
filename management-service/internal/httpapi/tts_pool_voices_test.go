package httpapi

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"livecompanion/management/internal/db"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/ttsscheduler"
)

func TestHighFidelityPoolCatalogueUsesEnabledConfiguredMappings(t *testing.T) {
	account := ttsscheduler.Account{ID: "private-account", Name: "supplier-name", AccountUID: "private-uid", Provider: ttsgateway.ProviderVolcengine, Configured: true, Enabled: true, Models: []string{ttsgateway.VolcengineICL2}, Voices: map[string]string{"seed-icl-2.0/S_main": "S_private_target", "seed-icl-2.0/S_alpha": "S_other_target"}}
	duplicate := account
	duplicate.ID = "backup"
	disabled := account
	disabled.Enabled = false
	disabled.Voices = map[string]string{"seed-icl-2.0/S_disabled": "S_disabled"}
	missingKey := disabled
	missingKey.Enabled, missingKey.Configured = true, false
	missingKey.Voices = map[string]string{"seed-icl-2.0/S_no_key": "S_no_key"}
	wrongProvider := account
	wrongProvider.Provider = "aliyun"
	wrongProvider.Voices = map[string]string{"seed-icl-2.0/S_ali": "S_ali"}
	wrongModel := account
	wrongModel.Models = []string{"another-model"}
	wrongModel.Voices = map[string]string{"seed-icl-2.0/S_wrong_model": "S_wrong_model"}
	invalid := account
	invalid.Voices = map[string]string{"seed-icl-2.0/S_good": "invalid-target", "seed-icl-2.0/bad-source": "S_valid", "other-model/S_bad_model": "S_valid", "seed-icl-2.0/S_x/extra": "S_valid"}
	voices := highFidelityPoolVoices([]ttsscheduler.Account{disabled, account, duplicate, missingKey, wrongProvider, wrongModel, invalid})
	if len(voices) != 2 || voices[0].ID != "S_alpha" || voices[1].ID != "S_main" {
		t.Fatalf("catalogue must deduplicate, sort and exclude unavailable voices: %+v", voices)
	}
	for _, voice := range voices {
		if voice.Provider != ttsgateway.ProviderVolcengine || voice.Model != ttsgateway.VolcengineICL2 || strings.Contains(voice.Name, "火山") {
			t.Fatal("wrong identity or supplier exposed in display", voice)
		}
		if voice.Name != "高仿真声音" || voice.Gender != "" || strings.Contains(voice.Name, strings.TrimPrefix(voice.ID, "S_")) {
			t.Fatal("missing display metadata must not invent name/gender from ID", voice)
		}
	}
	raw, _ := json.Marshal(voices)
	for _, secret := range []string{"private-account", "private-uid", "supplier-name", "S_private_target", "S_other_target"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("account detail or backup mapping leaked", secret)
		}
	}
	if empty := highFidelityPoolVoices(nil); empty == nil || !reflect.DeepEqual(empty, highFidelityPoolVoices([]ttsscheduler.Account{disabled, missingKey})) {
		t.Fatal("empty catalogue must be an empty JSON list")
	}
	longID := "S_" + strings.Repeat("a", 128)
	account.Voices = map[string]string{"seed-icl-2.0/" + longID: longID}
	long := highFidelityPoolVoices([]ttsscheduler.Account{account})
	if len(long) != 1 || long[0].ID != longID || utf8.RuneCountInString(long[0].Name) > 128 {
		t.Fatal("long valid IDs must remain selectable and saveable", long)
	}
}

func TestHighFidelityPoolCatalogueDisplayIsSeparateFromSynthesisIdentity(t *testing.T) {
	account := ttsscheduler.Account{Provider: ttsgateway.ProviderVolcengine, Configured: true, Enabled: true, Models: []string{ttsgateway.VolcengineICL2}, Voices: map[string]string{"seed-icl-2.0/S_main": "S_backup_target"}}
	before := highFidelityPoolVoices([]ttsscheduler.Account{account})
	after := applyHighFidelityVoiceDisplays(highFidelityPoolVoices([]ttsscheduler.Account{account}), map[string]db.TTSVoiceDisplay{
		"S_main": {Name: "晚晴", Gender: "女声"}, "S_not_available": {Name: "停用声音", Gender: "男声"},
	})
	if len(after) != 1 || after[0].Name != "晚晴" || after[0].Gender != "女声" {
		t.Fatal("display data must only overlay available voices", after)
	}
	before[0].Name, before[0].Gender = after[0].Name, after[0].Gender
	if !reflect.DeepEqual(before, after) || account.Voices["seed-icl-2.0/S_main"] != "S_backup_target" {
		t.Fatal("metadata changed synthesis identity/mapping")
	}
}
