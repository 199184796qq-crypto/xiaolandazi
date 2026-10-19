package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/anchorgeneration"
)

const AnchorGenerationSetting = "anchor_generation_config"

func (s *Store) AnchorGenerationSettings(ctx context.Context) (anchorgeneration.Settings, error) {
	config := anchorgeneration.Default()
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, AnchorGenerationSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return config, err
	}
	if err = json.Unmarshal([]byte(raw), &config); err != nil {
		return config, err
	}
	models, err := s.SpeechModels(ctx)
	if err != nil {
		return config, err
	}
	config.ApplyModelRoles(models.SystemDefaultID, models.DefaultID)
	config.NormalizeSharedFeedback()
	// 部署级「暂停审核」开关：话术生成后跳过全部审核（程序违规词扫描 + 模型二次
	// 复核），直接采纳并送 TTS。恢复审核时删掉下面这行并重编译即可。
	config.Cycle.SkipReview = true
	return config, nil
}

// Explicit operator bootstrap only. Lock and merge the JSON so setting default
// resource IDs never overwrites concurrently edited model roles or unknown keys.
func (s *Store) SetAnchorGenerationDefaultResources(ctx context.Context, workspace, agent, region string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? FOR UPDATE`, AnchorGenerationSetting).Scan(&raw); err != nil {
		return err
	}
	updated, err := mergeAnchorDefaultResources(raw, workspace, agent, region)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=0 WHERE setting_key=?`, updated, AnchorGenerationSetting); err != nil {
		return err
	}
	return tx.Commit()
}

func mergeAnchorDefaultResources(raw, workspace, agent, region string) (string, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return "", err
	}
	if data == nil {
		data = map[string]any{}
	}
	cycle, ok := data["cycle"].(map[string]any)
	if !ok {
		if data["cycle"] != nil {
			return "", errors.New("invalid stored cycle configuration")
		}
		cycle = map[string]any{}
	}
	cycle["workspace_id"], cycle["agent_id"], cycle["region"] = workspace, agent, region
	data["cycle"] = cycle
	updated, err := json.Marshal(data)
	return string(updated), err
}
