package db

import (
	"errors"
	"livecompanion/management/internal/ttsscheduler"
	"testing"
	"time"
)

func TestTTSPoolVolcengineValidation(t *testing.T) {
	base := ttsscheduler.AccountInput{Name: "火山", Provider: "volcengine", AccountUID: "10001", NewAPIKey: "fixture", Enabled: true, RPM: 30, RPS: 1, MaxConcurrent: 1, Models: []string{"seed-icl-2.0"}, Voices: map[string]string{"seed-icl-2.0/S_source": "S_target"}}
	if err := validateTTSAccount(&base); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ttsscheduler.AccountInput)
	}{
		{"excess concurrency", func(a *ttsscheduler.AccountInput) { a.MaxConcurrent = 11 }},
		{"wrong vendor model", func(a *ttsscheduler.AccountInput) { a.Models = []string{"qwen-audio-3.1-tts-flash"} }},
		{"wrong target voice", func(a *ttsscheduler.AccountInput) {
			a.Voices = map[string]string{"seed-icl-2.0/S_source": "qwen-voice"}
		}},
		{"invalid provider", func(a *ttsscheduler.AccountInput) { a.Provider = "unknown" }},
		{"environment account", func(a *ttsscheduler.AccountInput) { a.ID = ttsscheduler.CurrentAccountID; a.Revision = 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := base
			tc.change(&a)
			if validateTTSAccount(&a) == nil {
				t.Fatal("unsafe account accepted")
			}
		})
	}
}

func TestTTSPoolMySQLVolcengineProviderAndConcurrencyIsolation(t *testing.T) {
	s, _ := ttsPoolFixture(t)
	ctx := t.Context()
	a := ttsscheduler.AccountInput{Name: "火山已有音色", Provider: "volcengine", AccountUID: "10001", NewAPIKey: "fire-fixture-key", Enabled: true, RPM: 180, RPS: 3, MaxConcurrent: 1, Models: []string{"seed-icl-2.0"}, Voices: map[string]string{"seed-icl-2.0/S_source": "S_target"}}
	// Same numeric UID at a different provider is a different account; the
	// existing Aliyun quota, credentials and mappings must remain untouched.
	if err := s.SaveTTSAccount(ctx, a, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.TTSPoolSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var fire ttsscheduler.Account
	for _, account := range snapshot.Accounts {
		if account.Provider == "volcengine" {
			fire = account
		}
	}
	if fire.ID == "" || fire.MaxConcurrent != 1 || fire.RPM != 0 || fire.RPS != 0 {
		t.Fatal("saved provider/concurrency missing")
	}
	if err := s.SaveTTSAccount(ctx, a, 1); !errors.Is(err, ttsscheduler.ErrConfig) {
		t.Fatal("duplicate vendor UID accepted", err)
	}
	queue := func(id string) {
		t.Helper()
		if err := s.EnqueueTTS(ctx, ttsscheduler.Work{ID: id, Provider: "volcengine", Model: "seed-icl-2.0", VoiceID: "S_source", TTL: time.Minute}); err != nil {
			t.Fatal(err)
		}
	}
	queue("fire-first")
	queue("fire-next")
	first, err := s.ClaimTTS(ctx, "fire-first")
	if err != nil || !first.Granted || first.Credential.AccountID != fire.ID || first.Credential.APIKey != "fire-fixture-key" || first.Credential.VoiceID != "S_target" {
		t.Fatal("fire routed through wrong account", err)
	}
	// Remove the ordinary sliding-window wait, so this checks the independent
	// in-flight limit rather than merely observing a rate-limited request.
	if _, err := s.db.ExecContext(ctx, `UPDATE mgmt_tts_pool_jobs SET admitted_at=UTC_TIMESTAMP(6)-INTERVAL 2 SECOND WHERE id='fire-first'`); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimTTS(ctx, "fire-next")
	if err != nil || next.Granted {
		t.Fatal("concurrency cap bypassed", err)
	}
	ttsEnqueue(t, s, "ali-after-fire", "voice-a")
	ali, err := s.ClaimTTS(ctx, "ali-after-fire")
	if err != nil || !ali.Granted || ali.Credential.AccountID != ttsscheduler.CurrentAccountID {
		t.Fatal("fire FIFO blocked Aliyun", err)
	}
	if err := s.FinishTTS(ctx, "fire-first", "completed"); err != nil {
		t.Fatal(err)
	}
	next, err = s.ClaimTTS(ctx, "fire-next")
	if err != nil || !next.Granted || next.Credential.AccountID != fire.ID {
		t.Fatal("finished capacity not reused", err)
	}
	// A malformed old record with a wrong provider cannot steal the fire key.
	if err := s.EnqueueTTS(ctx, ttsscheduler.Work{ID: "wrong-vendor", Provider: "qwen_audio", Model: "seed-icl-2.0", VoiceID: "S_source", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	wrong, err := s.ClaimTTS(ctx, "wrong-vendor")
	if err != nil || wrong.Granted {
		t.Fatal("cross-vendor key exposed", err)
	}
	edit := ttsscheduler.AccountInput{ID: fire.ID, Revision: fire.Revision, Name: fire.Name, Provider: "aliyun", AccountUID: fire.AccountUID, RPM: 180, RPS: 3, Enabled: false}
	if err := s.SaveTTSAccount(ctx, edit, 1); !errors.Is(err, ttsscheduler.ErrConfig) {
		t.Fatal("provider identity replaced", err)
	}
}
