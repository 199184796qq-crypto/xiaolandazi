package db

import (
	"context"
	"encoding/json"
	"errors"
)

// Operator action: merge only the two text role references. Preserve all other
// generation, scheduling and voice settings, including keys unknown to this build.
func (s *Store) SetTextGenerationRoles(ctx context.Context, mainline, interaction string) error {
	for _, id := range []string{mainline, interaction} {
		p, err := s.ResolveSpeechModel(ctx, id)
		if err != nil || p == nil || !p.Enabled || p.APIKey == "" {
			return errors.New("text role profile unavailable")
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT value_text FROM mgmt_system_settings WHERE setting_key=? FOR UPDATE`, AnchorGenerationSetting).Scan(&raw); err != nil {
		return err
	}
	var settings map[string]any
	if err = json.Unmarshal([]byte(raw), &settings); err != nil {
		return err
	}
	cycle, ok := settings["cycle"].(map[string]any)
	if !ok {
		return errors.New("generation role settings malformed")
	}
	cycle["text_profile_id"] = mainline
	settings["interaction_profile_id"] = interaction
	updated, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE mgmt_system_settings SET value_text=?,updated_by_user_id=0 WHERE setting_key=?`, string(updated), AnchorGenerationSetting); err != nil {
		return err
	}
	return tx.Commit()
}
