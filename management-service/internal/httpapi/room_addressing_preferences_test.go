package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestValidateRoomAddressingPreferencesNormalizesTerms(t *testing.T) {
	input := model.RoomAddressingPreferencesInput{
		NamingPreference: " MORE ",
		PreferredTerms:   []string{"朋友", " 朋友 ", "老哥"},
		BlockedTerms:     []string{"宝子", "宝子"},
	}
	if err := validateRoomAddressingPreferences(&input); err != nil {
		t.Fatal(err)
	}
	if input.NamingPreference != "more" {
		t.Fatalf("preference=%q", input.NamingPreference)
	}
	if len(input.PreferredTerms) != 2 || len(input.BlockedTerms) != 1 {
		t.Fatalf("terms not normalized: %#v %#v", input.PreferredTerms, input.BlockedTerms)
	}
}

func TestValidateRoomAddressingPreferencesRejectsInvalidPreference(t *testing.T) {
	input := model.RoomAddressingPreferencesInput{NamingPreference: "always"}
	if err := validateRoomAddressingPreferences(&input); err == nil {
		t.Fatal("expected invalid preference to fail")
	}
}
