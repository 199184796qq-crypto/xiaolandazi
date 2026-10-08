package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"livecompanion/management/internal/model"
)

func defaultRoomInteractionPreferences(tenantID, roomID int64) model.RoomInteractionPreferences {
	return model.RoomInteractionPreferences{
		TenantID:             tenantID,
		RoomID:               roomID,
		OverallInteraction:   50,
		QuestionPreference:   50,
		WelcomePreference:    50,
		EngagementPreference: 50,
		ChatPreference:       50,
		ConversionPreference: 50,
		AutoHeat:             true,
	}
}

func clampInteractionPreference(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func normalizeRoomInteractionPreferencesInput(input model.RoomInteractionPreferencesInput) model.RoomInteractionPreferencesInput {
	input.OverallInteraction = clampInteractionPreference(input.OverallInteraction)
	input.QuestionPreference = clampInteractionPreference(input.QuestionPreference)
	input.WelcomePreference = clampInteractionPreference(input.WelcomePreference)
	input.EngagementPreference = clampInteractionPreference(input.EngagementPreference)
	input.ChatPreference = clampInteractionPreference(input.ChatPreference)
	input.ConversionPreference = clampInteractionPreference(input.ConversionPreference)
	return input
}

func (s *Store) GetRoomInteractionPreferences(ctx context.Context, tenantID, roomID int64) (model.RoomInteractionPreferences, error) {
	item := defaultRoomInteractionPreferences(tenantID, roomID)
	var updatedBy sql.NullInt64
	var updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT overall_interaction, question_preference, welcome_preference,
		       engagement_preference, chat_preference, conversion_preference,
		       auto_heat, updated_by_user_id, updated_at
		FROM live_room_interaction_preferences
		WHERE tenant_id=? AND room_id=?
	`, tenantID, roomID).Scan(
		&item.OverallInteraction,
		&item.QuestionPreference,
		&item.WelcomePreference,
		&item.EngagementPreference,
		&item.ChatPreference,
		&item.ConversionPreference,
		&item.AutoHeat,
		&updatedBy,
		&updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		schedule, scheduleErr := s.GetRoomInteractionSchedule(ctx, tenantID, roomID)
		item.Schedule = &schedule
		return item, scheduleErr
	}
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time
	}
	schedule, scheduleErr := s.GetRoomInteractionSchedule(ctx, tenantID, roomID)
	item.Schedule = &schedule
	return item, scheduleErr
}

func (s *Store) UpsertRoomInteractionPreferences(
	ctx context.Context,
	tenantID, roomID, userID int64,
	input model.RoomInteractionPreferencesInput,
) (model.RoomInteractionPreferences, error) {
	input = normalizeRoomInteractionPreferencesInput(input)
	current, err := s.GetRoomInteractionSchedule(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	defer tx.Rollback()
	// Serialize all room edits, including initialization, before saving either
	// scalar preferences or the immutable room/plan configuration version.
	var lockID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM core_rooms WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, roomID).Scan(&lockID); err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_room_interaction_preferences (
			tenant_id, room_id, overall_interaction, question_preference,
			welcome_preference, engagement_preference, chat_preference,
			conversion_preference, auto_heat, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			overall_interaction=VALUES(overall_interaction),
			question_preference=VALUES(question_preference),
			welcome_preference=VALUES(welcome_preference),
			engagement_preference=VALUES(engagement_preference),
			chat_preference=VALUES(chat_preference),
			conversion_preference=VALUES(conversion_preference),
			auto_heat=VALUES(auto_heat),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`,
		tenantID,
		roomID,
		input.OverallInteraction,
		input.QuestionPreference,
		input.WelcomePreference,
		input.EngagementPreference,
		input.ChatPreference,
		input.ConversionPreference,
		input.AutoHeat,
		userID,
	)
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	if input.Schedule != nil {
		if _, err := s.saveRoomInteractionScheduleTx(ctx, tx, tenantID, roomID, userID, *input.Schedule, current); err != nil {
			return model.RoomInteractionPreferences{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	item, err := s.GetRoomInteractionPreferences(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	return item, nil
}
