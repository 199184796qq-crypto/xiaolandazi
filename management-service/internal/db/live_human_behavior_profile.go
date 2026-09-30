package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func defaultRoomHumanBehaviorProfile(tenantID, roomID int64) model.RoomHumanBehaviorProfile {
	return model.RoomHumanBehaviorProfile{TenantID: tenantID, RoomID: roomID}
}

func (s *Store) GetRoomHumanBehaviorProfile(ctx context.Context, tenantID, roomID int64) (model.RoomHumanBehaviorProfile, error) {
	item := defaultRoomHumanBehaviorProfile(tenantID, roomID)
	var stateExpiresAt sql.NullTime
	var updatedBy sql.NullInt64
	var updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT trait_text, state_text, state_expires_at, updated_by_user_id, updated_at
		FROM live_room_human_behavior_profiles
		WHERE tenant_id=? AND room_id=?
	`, tenantID, roomID).Scan(
		&item.TraitText, &item.StateText, &stateExpiresAt, &updatedBy, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return model.RoomHumanBehaviorProfile{}, err
	}
	if stateExpiresAt.Valid {
		value := stateExpiresAt.Time.UTC()
		if time.Now().UTC().Before(value) {
			item.StateExpiresAt = &value
		} else {
			item.StateText = ""
		}
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time
	}
	return item, nil
}

func (s *Store) UpsertRoomHumanBehaviorProfile(ctx context.Context, tenantID, roomID, userID int64, input model.RoomHumanBehaviorProfileInput) (model.RoomHumanBehaviorProfile, error) {
	input.TraitText = strings.TrimSpace(input.TraitText)
	input.StateText = strings.TrimSpace(input.StateText)
	if input.StateText == "" {
		input.StateExpiresAt = nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_room_human_behavior_profiles (
			tenant_id, room_id, trait_text, state_text, state_expires_at, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			trait_text=VALUES(trait_text),
			state_text=VALUES(state_text),
			state_expires_at=VALUES(state_expires_at),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, roomID, input.TraitText, input.StateText, input.StateExpiresAt, userID)
	if err != nil {
		return model.RoomHumanBehaviorProfile{}, err
	}
	return s.GetRoomHumanBehaviorProfile(ctx, tenantID, roomID)
}
