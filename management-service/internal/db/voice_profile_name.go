package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"unicode"
	"unicode/utf8"
)

var ErrVoiceProfileNameConflict = errors.New("voice profile name changed")

func ValidateVoiceProfileName(name string) error {
	if strings.TrimSpace(name) == "" || utf8.RuneCountInString(strings.TrimSpace(name)) > 80 {
		return errors.New("声音名称须为1～80字")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return errors.New("声音名称不能包含控制字符")
		}
	}
	return nil
}

// Naming is metadata only: never rewrite voice/sample/model/config/default or
// publish another room configuration to change a label.
func (s *Store) RenameVoiceProfile(ctx context.Context, tenant, profile, actor int64, name, expected string) (model.VoiceProfile, error) {
	if err := ValidateVoiceProfileName(name); err != nil {
		return model.VoiceProfile{}, err
	}
	name = strings.TrimSpace(name)
	result, err := s.db.ExecContext(ctx, `UPDATE voice_profiles SET name=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND id=? AND BINARY name=? AND clone_status<>'disabled'`, name, actor, tenant, profile, expected)
	if err != nil {
		return model.VoiceProfile{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return model.VoiceProfile{}, err
	}
	current, err := s.GetVoiceProfile(ctx, tenant, profile)
	if err != nil {
		return current, err
	}
	if current.CloneStatus == "disabled" {
		return model.VoiceProfile{}, sql.ErrNoRows
	}
	if changed == 0 && current.Name != expected {
		return model.VoiceProfile{}, ErrVoiceProfileNameConflict
	}
	return current, nil
}
