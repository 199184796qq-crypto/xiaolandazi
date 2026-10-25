package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/livecost"
)

const LiveCostSetting = livecost.SettingsKey

var ErrLiveCostRevision = errors.New("费用配置已变化，请重新读取后再保存")

func defaultLiveCostSettingsJSON() string {
	raw, _ := json.Marshal(livecost.DefaultSettings())
	return string(raw)
}

func advanceLiveCostSettings(previous, updated livecost.Settings) (livecost.Settings, error) {
	if err := updated.Validate(); err != nil {
		return updated, err
	}
	if previous.Revision != updated.Revision {
		return updated, ErrLiveCostRevision
	}
	updated.Revision++
	if err := updated.Validate(); err != nil {
		return updated, err
	}
	return updated, nil
}

func (s *Store) LiveCostSettings(ctx context.Context) (livecost.Settings, error) {
	if s == nil || s.db == nil {
		return livecost.Settings{}, errors.New("读取费用配置暂不可用")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, LiveCostSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return livecost.DefaultSettings(), nil
	}
	if err != nil {
		return livecost.Settings{}, errors.New("读取费用配置失败，请稍后重试")
	}
	return livecost.DecodeSettings(raw)
}

func (s *Store) SaveLiveCostSettings(ctx context.Context, settings livecost.Settings, actorID int64) (livecost.Settings, error) {
	if err := settings.Validate(); err != nil {
		return settings, err
	}
	settings = settings.Clone()
	if s == nil || s.db == nil || actorID <= 0 {
		return settings, errors.New("保存费用配置的管理员身份或存储不可用")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return settings, errors.New("保存费用配置暂不可用，请稍后重试")
	}
	defer tx.Rollback()
	// The migration normally creates this row. This insert safely initializes an
	// absent row while preserving any existing administrator value and revision.
	if _, err = tx.ExecContext(ctx, `INSERT INTO mgmt_system_settings (setting_key,setting_group,label,value_text,input_type,sort_order) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE setting_key=VALUES(setting_key)`,
		LiveCostSetting, "intelligence", "直播计费与小时预算", defaultLiveCostSettingsJSON(), "json", 7); err != nil {
		return settings, errors.New("初始化费用配置失败，请稍后重试")
	}
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? FOR UPDATE`, LiveCostSetting).Scan(&raw); err != nil {
		return settings, errors.New("读取当前费用配置失败，请稍后重试")
	}
	previous, err := livecost.DecodeSettings(raw)
	if err != nil {
		return settings, errors.New("当前费用配置已损坏，请联系管理员核对")
	}
	settings, err = advanceLiveCostSettings(previous, settings)
	if err != nil {
		return settings, err
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		return settings, errors.New("费用配置编码失败")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=? WHERE setting_key=?`, string(encoded), actorID, LiveCostSetting); err != nil {
		return settings, errors.New("保存费用配置失败，请稍后重试")
	}
	if err = tx.Commit(); err != nil {
		return settings, errors.New("费用配置保存结果待核对，请重新读取")
	}
	return settings, nil
}
