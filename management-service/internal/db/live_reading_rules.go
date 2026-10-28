package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

// GetRoomReadingRules returns the room's reading rules text (one rule per line).
func (s *Store) GetRoomReadingRules(ctx context.Context, tenantID, roomID int64) (model.RoomReadingRules, error) {
	item := model.RoomReadingRules{TenantID: tenantID, RoomID: roomID}
	err := s.db.QueryRowContext(ctx, `
SELECT rules_text, updated_by_user_id, updated_at
FROM live_room_reading_rules
WHERE tenant_id = ? AND room_id = ?`, tenantID, roomID).Scan(&item.RulesText, &item.UpdatedByUserID, &item.UpdatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	return item, nil
}

// UpsertRoomReadingRules saves the room's reading rules, capped to a sane size.
func (s *Store) UpsertRoomReadingRules(ctx context.Context, tenantID, roomID, userID int64, input model.RoomReadingRulesInput) (model.RoomReadingRules, error) {
	text := strings.TrimSpace(input.RulesText)
	if len([]rune(text)) > 4000 {
		text = string([]rune(text)[:4000])
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO live_room_reading_rules (tenant_id, room_id, rules_text, updated_by_user_id)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE rules_text = VALUES(rules_text), updated_by_user_id = VALUES(updated_by_user_id)`,
		tenantID, roomID, text, userID)
	if err != nil {
		return model.RoomReadingRules{}, err
	}
	return s.GetRoomReadingRules(ctx, tenantID, roomID)
}
