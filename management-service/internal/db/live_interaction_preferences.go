package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func defaultRoomInteractionPreferences(tenantID, roomID int64) model.RoomInteractionPreferences {
	return model.RoomInteractionPreferences{
		TenantID:             tenantID,
		RoomID:               roomID,
		OverallInteraction:   "natural",
		QuestionPreference:   "natural",
		WelcomePreference:    "natural",
		EngagementPreference: "natural",
		ChatPreference:       "natural",
		ConversionPreference: "natural",
		AutoHeat:              true,
	}
}

func normalizeRoomInteractionPreferencesInput(input model.RoomInteractionPreferencesInput) model.RoomInteractionPreferencesInput {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return "natural"
		}
		return value
	}
	input.OverallInteraction = normalize(input.OverallInteraction)
	input.QuestionPreference = normalize(input.QuestionPreference)
	input.WelcomePreference = normalize(input.WelcomePreference)
	input.EngagementPreference = normalize(input.EngagementPreference)
	input.ChatPreference = normalize(input.ChatPreference)
	input.ConversionPreference = normalize(input.ConversionPreference)
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
		return item, nil
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
	return item, nil
}

func (s *Store) UpsertRoomInteractionPreferences(
	ctx context.Context,
	tenantID, roomID, userID int64,
	input model.RoomInteractionPreferencesInput,
) (model.RoomInteractionPreferences, error) {
	input = normalizeRoomInteractionPreferencesInput(input)
	_, err := s.db.ExecContext(ctx, `
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
	item, err := s.GetRoomInteractionPreferences(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomInteractionPreferences{}, err
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	return item, nil
}
