package db

import (
	"context"
	"errors"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"livecompanion/management/internal/voicemodels"
)

func TestVoiceModelSettingsDecodePreservesDefaultsAndRejectsCorruption(t *testing.T) {
	c, err := decodeVoiceModelSettings(`{}`)
	if err != nil || c != voicemodels.Default() {
		t.Fatal(c, err)
	}
	for _, raw := range []string{`invalid`, `{"custom_model":"unknown"}`, `{"official_library":"unknown"}`} {
		if _, err := decodeVoiceModelSettings(raw); err == nil {
			t.Fatal("corrupt config accepted", raw)
		}
	}
}

func TestVoiceModelSettingsRevisionMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if err := s.MigrateSystemSettings(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := s.VoiceModelSettings(ctx)
	if err != nil || c != voicemodels.Default() {
		t.Fatal(c, err)
	}
	c.CustomModel = ttsgateway.CosyVoicePlus
	saved, err := s.SaveVoiceModelSettings(ctx, c, 17)
	if err != nil || saved.Revision != 1 {
		t.Fatal(saved, err)
	}
	if _, err := s.SaveVoiceModelSettings(ctx, c, 18); !errors.Is(err, ErrVoiceModelRevision) {
		t.Fatal("stale write allowed", err)
	}
	if err := s.MigrateSystemSettings(ctx); err != nil {
		t.Fatal(err)
	}
	reloaded, err := s.VoiceModelSettings(ctx)
	if err != nil || reloaded != saved {
		t.Fatal("migration overwrote admin choice", reloaded, err)
	}
	if err := s.UpdateSystemSettings(ctx, []model.SystemSettingUpdate{{Key: VoiceModelSetting, Value: `{}`}}, 17); err == nil {
		t.Fatal("generic editor bypassed revision guard")
	}
	var actor int64
	if err := s.db.QueryRowContext(ctx, `SELECT updated_by_user_id FROM mgmt_system_settings WHERE setting_key=?`, VoiceModelSetting).Scan(&actor); err != nil || actor != 17 {
		t.Fatal("save lost administrator", actor, err)
	}
}

func TestVoiceModelSettingsRejectsStaleEdit(t *testing.T) {
	previous := voicemodels.Default()
	previous.Revision = 9
	previous.CustomModel = ttsgateway.CosyVoicePlus
	edit := previous
	edit.Revision = 8
	edit.CustomModel = ttsgateway.CosyVoiceFlash
	if _, err := advanceVoiceModelSettings(previous, edit); !errors.Is(err, ErrVoiceModelRevision) {
		t.Fatal("stale edit would overwrite saved choice", err)
	}
	edit.Revision = 9
	next, err := advanceVoiceModelSettings(previous, edit)
	if err != nil || next.Revision != 10 || next.CustomModel != ttsgateway.CosyVoiceFlash || previous.CustomModel != ttsgateway.CosyVoicePlus {
		t.Fatal(next, previous, err)
	}
	edit.CustomModel = "unknown"
	if _, err := advanceVoiceModelSettings(previous, edit); err == nil {
		t.Fatal("invalid edit accepted")
	}
}
