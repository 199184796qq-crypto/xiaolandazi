package room

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrRoomNameInvalid = errors.New("请输入1至128个字符的直播间名称，不能含换行或控制字符")
	ErrRoomNameChanged = errors.New("直播间名称已被修改，请刷新后重试")
)

type NameUpdate struct {
	Name         string  `json:"name"`
	ExpectedName *string `json:"expected_name"`
}

func ValidRoomName(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= 128 && strings.IndexFunc(value, unicode.IsControl) < 0
}

// UpdateName changes display metadata only, including while live. It does not
// replace collector generations, cancel timers, or touch audio/room identity.
func (s *Store) UpdateName(ctx context.Context, tenantID, roomID int64, input NameUpdate) (string, error) {
	input.Name = strings.TrimSpace(input.Name)
	if !ValidRoomName(input.Name) || input.ExpectedName == nil || !utf8.ValidString(*input.ExpectedName) || utf8.RuneCountInString(*input.ExpectedName) > 128 {
		return "", ErrRoomNameInvalid
	}
	item, err := s.Get(ctx, &tenantID, roomID)
	if err != nil {
		return "", err
	}
	current := s.lifecycle.entry(item)
	if current == nil {
		return "", ErrCollectorReplaced
	}
	current.transition.Lock()
	defer current.transition.Unlock()
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.retired {
		return "", ErrCollectorReplaced
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var original string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM core_rooms WHERE id = ? AND tenant_id = ? FOR UPDATE`, roomID, tenantID).Scan(&original); err != nil {
		return "", err
	}
	if original != *input.ExpectedName {
		return "", ErrRoomNameChanged
	}
	if original == input.Name {
		return original, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE core_rooms SET name = ?, updated_at = CURRENT_TIMESTAMP(3) WHERE id = ? AND tenant_id = ?`, input.Name, roomID, tenantID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	current.room.Name = input.Name
	return input.Name, nil
}
