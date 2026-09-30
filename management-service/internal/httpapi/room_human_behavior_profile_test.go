package httpapi

import (
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestValidateRoomHumanBehaviorProfileDefaultsStateExpiry(t *testing.T) {
	input := model.RoomHumanBehaviorProfileInput{
		TraitText: " 喜欢短句 ",
		StateText: " 今天嗓子不舒服 ",
	}
	before := time.Now().UTC()
	if err := validateRoomHumanBehaviorProfile(&input); err != nil {
		t.Fatal(err)
	}
	if input.TraitText != "喜欢短句" || input.StateText != "今天嗓子不舒服" {
		t.Fatalf("text not normalized: %#v", input)
	}
	if input.StateExpiresAt == nil || !input.StateExpiresAt.After(before.Add(119*time.Minute)) {
		t.Fatalf("state expiry not defaulted to current-session window: %#v", input.StateExpiresAt)
	}
}

func TestValidateRoomHumanBehaviorProfileRejectsExpiredState(t *testing.T) {
	expired := time.Now().UTC().Add(-time.Minute)
	input := model.RoomHumanBehaviorProfileInput{
		StateText:      "今天声音轻一点",
		StateExpiresAt: &expired,
	}
	if err := validateRoomHumanBehaviorProfile(&input); err == nil {
		t.Fatal("expected expired state to be rejected")
	}
}

func TestValidateRoomHumanBehaviorProfileClearsExpiryWithoutState(t *testing.T) {
	future := time.Now().UTC().Add(time.Hour)
	input := model.RoomHumanBehaviorProfileInput{
		TraitText:      "转场前喜欢总结",
		StateExpiresAt: &future,
	}
	if err := validateRoomHumanBehaviorProfile(&input); err != nil {
		t.Fatal(err)
	}
	if input.StateExpiresAt != nil {
		t.Fatalf("expiry must be cleared when state is empty: %#v", input.StateExpiresAt)
	}
}
