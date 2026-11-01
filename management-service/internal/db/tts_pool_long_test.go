package db

import (
	"testing"
	"time"

	"livecompanion/management/internal/ttsscheduler"
)

func TestTTSPoolMySQLRecoveryPinsOriginalAccountWithoutBlockingOtherAccount(t *testing.T) {
	s, second := ttsPoolFixture(t)
	ctx := t.Context()
	// An unavailable pinned recovery must not take a different account, nor
	// prevent an unrelated normal request from using that free account.
	w := ttsscheduler.Work{ID: "recover", Model: "qwen-audio-3.1-tts-flash", Provider: "qwen_audio", VoiceID: "voice-a", RequiredAccountID: second, TTL: time.Minute}
	if err := s.EnqueueTTS(ctx, w); err != nil {
		t.Fatal(err)
	}
	ttsFillMinute(t, s, second)
	claim, err := s.ClaimTTS(ctx, "recover")
	if err != nil || claim.Granted {
		t.Fatal("recovery changed cloud owner", err)
	}
	ttsEnqueue(t, s, "normal", "voice-a")
	claim, err = s.ClaimTTS(ctx, "normal")
	if err != nil || !claim.Granted || claim.Credential.AccountID != ttsscheduler.CurrentAccountID {
		t.Fatal("pinned recovery blocked unrelated free account", err)
	}
}
