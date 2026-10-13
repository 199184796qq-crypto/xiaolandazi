package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strconv"
)

const userVoiceCacheSettingPrefix = "tenant_generation_cache_policy."
const userVoiceCacheSettingGroup = "user_voice_cache"

func userVoiceCacheKey(tenant int64) (string, error) {
	if tenant <= 0 {
		return "", errors.New("用户账号无效")
	}
	return userVoiceCacheSettingPrefix + strconv.FormatInt(tenant, 10), nil
}

func decodeUserVoiceCacheSettings(raw string) (*model.UserVoiceCacheSettings, error) {
	var wire struct {
		Mainline    *bool `json:"mainline_cache_reuse_enabled"`
		Interaction *bool `json:"interaction_cache_reuse_enabled"`
		Hours       *int  `json:"retention_hours"`
	}
	if json.Unmarshal([]byte(raw), &wire) != nil || wire.Mainline == nil || wire.Interaction == nil || wire.Hours == nil {
		return nil, errors.New("用户声音缓存配置不完整")
	}
	if _, err := ParseAnchorVoiceCacheHours(strconv.Itoa(*wire.Hours)); err != nil {
		return nil, err
	}
	return &model.UserVoiceCacheSettings{MainlineReuse: *wire.Mainline, InteractionReuse: *wire.Interaction, RetentionHours: *wire.Hours}, nil
}

func (s *Store) userVoiceCacheOverride(ctx context.Context, tenant int64) (*model.UserVoiceCacheSettings, error) {
	key, err := userVoiceCacheKey(tenant)
	if err != nil {
		return nil, err
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return decodeUserVoiceCacheSettings(raw)
}

func (s *Store) GenerationCacheReuseEnabledForTenant(ctx context.Context, tenant int64, key string) (bool, error) {
	if key != model.MainlineGenerationCacheReuseSetting && key != model.InteractionGenerationCacheReuseSetting {
		return false, errors.New("未知声音缓存开关")
	}
	policy, err := s.userVoiceCacheOverride(ctx, tenant)
	if err != nil {
		return false, err
	}
	if policy == nil {
		return s.GenerationCacheReuseEnabled(ctx, key)
	}
	if key == model.MainlineGenerationCacheReuseSetting {
		return policy.MainlineReuse, nil
	}
	return policy.InteractionReuse, nil
}

func (s *Store) AnchorVoiceCacheRetentionHoursForTenant(ctx context.Context, tenant int64) (int, error) {
	policy, err := s.userVoiceCacheOverride(ctx, tenant)
	if err != nil {
		return 0, err
	}
	if policy == nil {
		return s.AnchorVoiceCacheRetentionHours(ctx)
	}
	return policy.RetentionHours, nil
}

func (s *Store) DefaultUserVoiceCacheSettings(ctx context.Context) (model.UserVoiceCacheSettings, error) {
	var out model.UserVoiceCacheSettings
	var err error
	out.MainlineReuse, err = s.GenerationCacheReuseEnabled(ctx, model.MainlineGenerationCacheReuseSetting)
	if err != nil {
		return out, err
	}
	out.InteractionReuse, err = s.GenerationCacheReuseEnabled(ctx, model.InteractionGenerationCacheReuseSetting)
	if err != nil {
		return out, err
	}
	out.RetentionHours, err = s.AnchorVoiceCacheRetentionHours(ctx)
	return out, err
}

func (s *Store) ListUserVoiceCachePolicies(ctx context.Context) ([]model.UserVoiceCachePolicy, error) {
	defaults, err := s.DefaultUserVoiceCacheSettings(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT u.tenant_id,u.id,u.username,u.display_name,u.status,COALESCE(p.value_text,''),p.updated_at
 FROM mgmt_users u LEFT JOIN mgmt_system_settings p ON p.setting_key=CONCAT(?,u.tenant_id)
 WHERE u.role='customer' AND u.tenant_id IS NOT NULL ORDER BY u.id DESC`, userVoiceCacheSettingPrefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.UserVoiceCachePolicy{}
	for rows.Next() {
		item := model.UserVoiceCachePolicy{UserVoiceCacheSettings: defaults, UsingDefault: true}
		var raw string
		var updated sql.NullTime
		if err = rows.Scan(&item.TenantID, &item.UserID, &item.Username, &item.DisplayName, &item.Status, &raw, &updated); err != nil {
			return nil, err
		}
		if raw != "" {
			policy, e := decodeUserVoiceCacheSettings(raw)
			if e != nil {
				return nil, e
			}
			item.UserVoiceCacheSettings = *policy
			item.UsingDefault = false
			if updated.Valid {
				at := updated.Time
				item.UpdatedAt = &at
			}
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateUserVoiceCachePolicy(ctx context.Context, tenant, actor int64, policy *model.UserVoiceCacheSettings) error {
	key, err := userVoiceCacheKey(tenant)
	if err != nil {
		return err
	}
	var raw []byte
	if policy != nil {
		raw, err = json.Marshal(policy)
		if err != nil {
			return err
		}
		if _, err = decodeUserVoiceCacheSettings(string(raw)); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM mgmt_users WHERE tenant_id=? AND role='customer' ORDER BY id LIMIT 1 FOR UPDATE`, tenant).Scan(&id); err != nil {
		return err
	}
	if policy == nil {
		_, err = tx.ExecContext(ctx, `DELETE FROM mgmt_system_settings WHERE setting_key=?`, key)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO mgmt_system_settings(setting_key,setting_group,label,value_text,input_type,sort_order,updated_by_user_id)
  VALUES(?,?,'用户话术与声音缓存',?,'json',0,?) ON DUPLICATE KEY UPDATE value_text=VALUES(value_text),updated_by_user_id=VALUES(updated_by_user_id)`, key, userVoiceCacheSettingGroup, string(raw), actor)
	}
	if err != nil {
		return fmt.Errorf("保存用户声音缓存: %w", err)
	}
	return tx.Commit()
}
