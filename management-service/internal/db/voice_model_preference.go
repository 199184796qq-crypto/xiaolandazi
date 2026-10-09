package db

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
	"strconv"
)

func setVoiceModelPreference(config map[string]any, room int64, modelName string) error {
	if config == nil || room <= 0 || !ttsgateway.SupportsCloneModel(modelName) {
		return errors.New("声音模型选择无效")
	}
	prefs, ok := config["ui_model_preferences"].(map[string]any)
	if config["ui_model_preferences"] != nil && !ok {
		return errors.New("声音模型偏好格式无效")
	}
	if prefs == nil {
		prefs = map[string]any{}
	}
	prefs[strconv.FormatInt(room, 10)] = modelName
	config["ui_model_preferences"] = prefs
	return nil
}

// A room-specific UI preference is not a live voice identity. Keep voice_id,
// provider, target_model, samples, bindings and other rooms completely intact.
func (s *Store) SaveVoiceModelPreference(ctx context.Context, tenant, room, profile, actor int64, modelName string) (model.VoiceProfile, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.VoiceProfile{}, err
	}
	defer tx.Rollback()
	var found int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM core_rooms WHERE id=? AND tenant_id=?`, room, tenant).Scan(&found); err != nil {
		return model.VoiceProfile{}, err
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT CAST(config_json AS CHAR) FROM voice_profiles WHERE id=? AND tenant_id=? AND clone_status<>'disabled' FOR UPDATE`, profile, tenant).Scan(&raw); err != nil {
		return model.VoiceProfile{}, err
	}
	config := map[string]any{}
	if err = json.Unmarshal([]byte(raw), &config); err != nil || config == nil {
		return model.VoiceProfile{}, errors.New("声音配置格式无效，未覆盖")
	}
	if err = setVoiceModelPreference(config, room, modelName); err != nil {
		return model.VoiceProfile{}, err
	}
	updated, err := json.Marshal(config)
	if err != nil {
		return model.VoiceProfile{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE voice_profiles SET config_json=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND clone_status<>'disabled'`, string(updated), actor, profile, tenant); err != nil {
		return model.VoiceProfile{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.VoiceProfile{}, err
	}
	return s.GetVoiceProfile(ctx, tenant, profile)
}
