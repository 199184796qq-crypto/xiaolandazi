package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"livecompanion/management/internal/model"
)

const AnchorVoiceCacheRetentionSetting = "anchor_voice_cache_retention_hours"
const DefaultAnchorVoiceCacheHours = 5

func ParseAnchorVoiceCacheHours(raw string) (int, error) {
	hours, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || hours < 1 || hours > 168 {
		return 0, fmt.Errorf("OSS声音缓存保留时间须为1～168小时的整数")
	}
	return hours, nil
}

func ValidateGenerationCacheReuseSetting(key, raw string) error {
	if key != model.MainlineGenerationCacheReuseSetting && key != model.InteractionGenerationCacheReuseSetting {
		return nil
	}
	if raw != "true" && raw != "false" {
		return errors.New("内容缓存复用开关须为true或false")
	}
	return nil
}

func (s *Store) GenerationCacheReuseEnabled(ctx context.Context, key string) (bool, error) {
	if key != model.MainlineGenerationCacheReuseSetting && key != model.InteractionGenerationCacheReuseSetting {
		return false, errors.New("未知内容缓存复用开关")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if err = ValidateGenerationCacheReuseSetting(key, raw); err != nil {
		return false, err
	}
	return raw == "true", nil
}

func (s *Store) AnchorVoiceCacheRetentionHours(ctx context.Context) (int, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, AnchorVoiceCacheRetentionSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultAnchorVoiceCacheHours, nil
	}
	if err != nil {
		return 0, err
	}
	return ParseAnchorVoiceCacheHours(raw)
}
