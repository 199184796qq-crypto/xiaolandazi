package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/voicemodels"
)

const VoiceModelSetting = "voice_model_config"

var ErrVoiceModelRevision = errors.New("声音配置已被其他管理员更新，请重新读取后保存")

func advanceVoiceModelSettings(previous, updated voicemodels.Settings) (voicemodels.Settings, error) {
	if err := updated.Validate(); err != nil {
		return updated, err
	}
	if previous.Revision != updated.Revision {
		return updated, ErrVoiceModelRevision
	}
	updated.Revision++
	return updated, nil
}

func decodeVoiceModelSettings(raw string) (voicemodels.Settings, error) {
	c := voicemodels.Default()
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func (s *Store) VoiceModelSettings(ctx context.Context) (voicemodels.Settings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, VoiceModelSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return voicemodels.Default(), nil
	}
	if err != nil {
		return voicemodels.Settings{}, err
	}
	return decodeVoiceModelSettings(raw)
}

func (s *Store) SaveVoiceModelSettings(ctx context.Context, c voicemodels.Settings, actorID int64) (voicemodels.Settings, error) {
	if err := c.Validate(); err != nil {
		return c, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return c, err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? FOR UPDATE`, VoiceModelSetting).Scan(&raw); err != nil {
		return c, err
	}
	previous, err := decodeVoiceModelSettings(raw)
	if err != nil {
		return c, err
	}
	c, err = advanceVoiceModelSettings(previous, c)
	if err != nil {
		return c, err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return c, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=?`, string(encoded), actorID, VoiceModelSetting); err != nil {
		return c, err
	}
	return c, tx.Commit()
}
