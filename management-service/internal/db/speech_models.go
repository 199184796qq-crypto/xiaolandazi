package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"livecompanion/management/internal/agentgateway"
)

func (s *Store) MigrateSpeechModels(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mgmt_anchor_model_settings (
		id TINYINT UNSIGNED NOT NULL PRIMARY KEY,
		config_json JSON NOT NULL,
		keys_json JSON NOT NULL,
		updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
		updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT IGNORE INTO mgmt_anchor_model_settings(id,config_json,keys_json) VALUES (1,?,?)`, `{"profiles":[],"default_id":"","revision":0}`, `{}`)
	return err
}

func (s *Store) readSpeechModels(ctx context.Context) (agentgateway.SpeechModels, map[string][]byte, error) {
	var configRaw, keysRaw []byte
	err := s.db.QueryRowContext(ctx, `SELECT config_json,keys_json FROM mgmt_anchor_model_settings WHERE id=1`).Scan(&configRaw, &keysRaw)
	var config agentgateway.SpeechModels
	keys := map[string][]byte{}
	if err != nil {
		return config, keys, err
	}
	if err = json.Unmarshal(configRaw, &config); err != nil {
		return config, keys, err
	}
	if err = json.Unmarshal(keysRaw, &keys); err != nil {
		return config, keys, err
	}
	for i := range config.Profiles {
		config.Profiles[i].HasAPIKey = len(keys[config.Profiles[i].ID]) > 0
	}
	return config, keys, nil
}

func (s *Store) SpeechModels(ctx context.Context) (agentgateway.SpeechModels, error) {
	config, _, err := s.readSpeechModels(ctx)
	return config, err
}

// Empty ID means resolve the shared default for mainline and realtime speech.
// A named ID is a pinned repair/test target and must never fall back elsewhere.
func (s *Store) ResolveSpeechModel(ctx context.Context, id string) (*agentgateway.SpeechModel, error) {
	config, keys, err := s.readSpeechModels(ctx)
	if err != nil {
		return nil, err
	}
	if id == "" {
		id = config.DefaultID
		if id == "" {
			return nil, nil
		}
	}
	for _, p := range config.Profiles {
		if p.ID != id {
			continue
		}
		p.APIKey, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), id, keys[id])
		return &p, err
	}
	return nil, errors.New("主播模型配置不存在")
}

func (s *Store) SaveSpeechModels(ctx context.Context, input agentgateway.SpeechModelsInput, actorID int64) error {
	if err := agentgateway.ValidateSpeechModels(input); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldRaw, keysRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT config_json,keys_json FROM mgmt_anchor_model_settings WHERE id=1 FOR UPDATE`).Scan(&oldRaw, &keysRaw); err != nil {
		return err
	}
	var old agentgateway.SpeechModels
	oldKeys := map[string][]byte{}
	if err = json.Unmarshal(oldRaw, &old); err != nil {
		return err
	}
	if err = json.Unmarshal(keysRaw, &oldKeys); err != nil {
		return err
	}
	if input.Revision != old.Revision {
		return errors.New("配置已被其他管理员修改，请刷新后重试")
	}
	next := agentgateway.SpeechModels{Profiles: []agentgateway.SpeechModel{}, DefaultID: input.DefaultID, Revision: old.Revision + 1}
	nextKeys := map[string][]byte{}
	for _, item := range input.Profiles {
		p := item.SpeechModel
		key := strings.TrimSpace(item.NewAPIKey)
		if key != "" {
			nextKeys[p.ID], err = agentgateway.SealSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), p.ID, key)
			if err != nil {
				return err
			}
		} else {
			for _, existing := range old.Profiles {
				if existing.ID == p.ID && (existing.BaseURL != p.BaseURL || existing.Protocol != p.Protocol) {
					return errors.New("API 地址或协议改变后，请重新填写密钥")
				}
			}
			nextKeys[p.ID] = oldKeys[p.ID]
		}
		if len(nextKeys[p.ID]) == 0 {
			return errors.New("请填写模型 API 密钥")
		}
		// Verify retained credentials against the server key before activation.
		if _, err = agentgateway.OpenSpeechKey(os.Getenv("MODEL_CONFIG_ENCRYPTION_KEY"), p.ID, nextKeys[p.ID]); err != nil {
			return err
		}
		p.HasAPIKey = true
		p.APIKey = ""
		next.Profiles = append(next.Profiles, p)
	}
	configJSON, err := next.PublicJSON()
	if err != nil {
		return err
	}
	keysJSON, err := json.Marshal(nextKeys)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_anchor_model_settings SET config_json=?,keys_json=?,updated_by_user_id=? WHERE id=1`, configJSON, keysJSON, actorID); err != nil {
		return err
	}
	return tx.Commit()
}
