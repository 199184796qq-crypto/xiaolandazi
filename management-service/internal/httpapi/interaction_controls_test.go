package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/model"
	"testing"
)

func TestInteractionControlValidation(t *testing.T) {
	for _, raw := range []string{`{"interaction_cooldowns":{"like":3601}}`, `{"interaction_cooldowns":{"like":-1}}`, `{"interaction_cooldowns":{"like":1.5}}`, `{"interaction_switches":{"typo":false}}`, `{"interaction_cooldowns":{"urgent":20}}`} {
		var in model.RoomInteractionPreferencesInput
		err := json.Unmarshal([]byte(raw), &in)
		if err == nil {
			err = validateRoomInteractionPreferences(&in)
		}
		if err == nil {
			t.Fatal("invalid input accepted", raw)
		}
	}
	for _, raw := range []string{`{}`, `{"interaction_cooldowns":{"welcome":0,"like":3600}}`, `{"interaction_active":false,"interaction_switches":{"welcome":false}}`} {
		var in model.RoomInteractionPreferencesInput
		if err := json.Unmarshal([]byte(raw), &in); err != nil {
			t.Fatal(err)
		}
		if err := validateRoomInteractionPreferences(&in); err != nil {
			t.Fatal(err)
		}
	}
}
