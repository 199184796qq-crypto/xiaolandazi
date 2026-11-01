package strategycenter

import "testing"

func TestInteractionControlsDefaultsValidationAndMissingKeys(t *testing.T) {
	p := RoomInteractionPreferences{}
	for _, kind := range InteractionControlKinds {
		if !p.ControlEnabled(kind) {
			t.Fatal("missing key disabled", kind)
		}
	}
	p.Switches = map[string]bool{"welcome": false}
	if p.ControlEnabled("welcome_named") || !p.ControlEnabled("like") {
		t.Fatal("alias or missing-key semantics")
	}
	for _, v := range []int{-1, 3601} {
		p.Cooldowns = map[string]int{"welcome": v}
		if ValidateInteractionControls(p) == nil {
			t.Fatal("invalid seconds accepted", v)
		}
	}
	for _, v := range []int{0, 3600} {
		p.Cooldowns = map[string]int{"welcome": v}
		if err := ValidateInteractionControls(p); err != nil {
			t.Fatal(err)
		}
	}
	p.Switches = map[string]bool{}
	for _, k := range InteractionControlKinds {
		p.Switches[k] = false
	}
	if p.ControlsActive() || p.ControlEnabled("engagement") {
		t.Fatal("all nine off still active")
	}
}
func TestInteractionControlsMonotonicCopyAndLegacySave(t *testing.T) {
	s := New()
	on := true
	p := DefaultRoomInteractionPreferences(29)
	p.InteractionActive = &on
	p.Switches = map[string]bool{"welcome": false}
	p.Cooldowns = map[string]int{"like": 120}
	p.ControlsRevision = 2
	saved, ok := s.PutRoomInteractionPreferencesChecked(29, p)
	if !ok {
		t.Fatal("initial rejected")
	}
	saved.Switches["welcome"] = true
	p.Cooldowns["like"] = 5
	got := s.RoomInteractionPreferences(29)
	if got.Switches["welcome"] || got.Cooldowns["like"] != 120 {
		t.Fatal("caller map mutated stored config")
	}
	p.ControlsRevision = 1
	if _, ok = s.PutRoomInteractionPreferencesChecked(29, p); ok {
		t.Fatal("stale push accepted")
	}
	p.ControlsRevision = 0
	if _, ok = s.PutRoomInteractionPreferencesChecked(29, p); ok {
		t.Fatal("unversioned controls overwrote saved config")
	}
	oldClient := DefaultRoomInteractionPreferences(29)
	oldClient.QuestionPreference = 75
	if _, ok = s.PutRoomInteractionPreferencesChecked(29, oldClient); !ok {
		t.Fatal("legacy scalar save rejected")
	}
	got = s.RoomInteractionPreferences(29)
	if got.ControlsRevision != 2 || got.ControlEnabled("welcome") || got.Cooldowns["like"] != 120 {
		t.Fatal("legacy save reset controls", got)
	}
}
