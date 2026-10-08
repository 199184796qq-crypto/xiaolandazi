package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Real MySQL plus the production voice-profile DDL: no fictional rooms table.
// Explicit test opt-in uses random isolated schema/tables and cleans them up.
func TestVoiceModelPreferenceMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE core_rooms(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	found := false
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS voice_profiles (") {
			exec(strings.TrimSpace(statement))
			found = true
		}
	}
	if !found {
		t.Fatal("production voice_profiles DDL missing")
	}
	exec(`INSERT INTO core_rooms(id,tenant_id) VALUES(26,14),(27,14),(28,15)`)
	original := map[string]any{
		"target_model": "qwen-audio-3.0-tts-plus", "voice_id": "original-plus",
		"sample_asset_id": float64(9), "avatar_key": "female_young",
		"ui_model_preferences": map[string]any{"27": "qwen-audio-3.0-tts-plus"},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO voice_profiles(id,tenant_id,name,provider,voice_id,sample_asset_id,clone_status,config_json,is_default,created_by_user_id) VALUES(2,14,'test voice','supplier','original-plus',9,'ready',?,1,5),(3,15,'other voice','supplier','other-plus',10,'ready',?,0,6),(4,14,'disabled voice','supplier','disabled-plus',11,'disabled',?,0,5)`, string(raw), string(raw), string(raw))
	saved, err := s.SaveVoiceModelPreference(ctx, 14, 26, 2, 5, "qwen-audio-3.1-tts-flash")
	if err != nil {
		t.Fatal(err)
	}
	if saved.VoiceID != "original-plus" || saved.Provider != "supplier" || saved.CloneStatus != "ready" || !saved.IsDefault || saved.SampleAssetID == nil || *saved.SampleAssetID != 9 {
		t.Fatal("voice identity changed", saved)
	}
	expected := original
	expected["ui_model_preferences"].(map[string]any)["26"] = "qwen-audio-3.1-tts-flash"
	if !reflect.DeepEqual(saved.Config, expected) {
		t.Fatal("config overwritten", saved.Config)
	}
	before, _ := json.Marshal(saved.Config)
	for _, scope := range [][3]int64{{15, 26, 2}, {14, 28, 2}, {14, 26, 3}, {14, 26, 4}, {14, 999, 2}} {
		if _, err := s.SaveVoiceModelPreference(ctx, scope[0], scope[1], scope[2], 5, "qwen-audio-3.0-tts-plus"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("cross-scope/disabled/missing write accepted", scope, err)
		}
	}
	if _, err := s.SaveVoiceModelPreference(ctx, 14, 26, 2, 5, "unknown-model"); err == nil {
		t.Fatal("invalid model saved")
	}
	fresh, err := s.GetVoiceProfile(ctx, 14, 2)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(fresh.Config)
	if string(before) != string(after) {
		t.Fatal("rejected transaction modified preferences")
	}
	other, err := s.GetVoiceProfile(ctx, 15, 3)
	if err != nil || other.Config["ui_model_preferences"].(map[string]any)["26"] != nil {
		t.Fatal("other tenant changed", err)
	}
}
