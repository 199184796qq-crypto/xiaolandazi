package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func defaultRoomAddressingPreferences(tenantID, roomID int64) model.RoomAddressingPreferences {
	return model.RoomAddressingPreferences{
		TenantID:         tenantID,
		RoomID:           roomID,
		NamingPreference: "natural",
		PreferredTerms:   []string{},
		BlockedTerms:     []string{},
	}
}

func normalizeAddressingTerms(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
		if len(result) >= 12 {
			break
		}
	}
	return result
}

func normalizeRoomAddressingPreferencesInput(input model.RoomAddressingPreferencesInput) model.RoomAddressingPreferencesInput {
	input.NamingPreference = strings.ToLower(strings.TrimSpace(input.NamingPreference))
	if input.NamingPreference == "" {
		input.NamingPreference = "natural"
	}
	input.PreferredTerms = normalizeAddressingTerms(input.PreferredTerms)
	input.BlockedTerms = normalizeAddressingTerms(input.BlockedTerms)
	return input
}

func decodeAddressingTerms(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{}
	}
	var result []string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return []string{}
	}
	return normalizeAddressingTerms(result)
}

func (s *Store) GetRoomAddressingPreferences(ctx context.Context, tenantID, roomID int64) (model.RoomAddressingPreferences, error) {
	item := defaultRoomAddressingPreferences(tenantID, roomID)
	var preferredJSON, blockedJSON string
	var updatedBy sql.NullInt64
	var updatedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT naming_preference, preferred_terms_json, blocked_terms_json, updated_by_user_id, updated_at
		FROM live_room_addressing_preferences
		WHERE tenant_id=? AND room_id=?
	`, tenantID, roomID).Scan(
		&item.NamingPreference, &preferredJSON, &blockedJSON, &updatedBy, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return model.RoomAddressingPreferences{}, err
	}
	item.PreferredTerms = decodeAddressingTerms(preferredJSON)
	item.BlockedTerms = decodeAddressingTerms(blockedJSON)
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	if updatedAt.Valid {
		item.UpdatedAt = updatedAt.Time
	}
	return item, nil
}

func (s *Store) UpsertRoomAddressingPreferences(ctx context.Context, tenantID, roomID, userID int64, input model.RoomAddressingPreferencesInput) (model.RoomAddressingPreferences, error) {
	input = normalizeRoomAddressingPreferencesInput(input)
	preferredJSON, _ := json.Marshal(input.PreferredTerms)
	blockedJSON, _ := json.Marshal(input.BlockedTerms)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_room_addressing_preferences (
			tenant_id, room_id, naming_preference, preferred_terms_json, blocked_terms_json, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			naming_preference=VALUES(naming_preference),
			preferred_terms_json=VALUES(preferred_terms_json),
			blocked_terms_json=VALUES(blocked_terms_json),
			updated_by_user_id=VALUES(updated_by_user_id),
			updated_at=CURRENT_TIMESTAMP(3)
	`, tenantID, roomID, input.NamingPreference, string(preferredJSON), string(blockedJSON), userID)
	if err != nil {
		return model.RoomAddressingPreferences{}, err
	}
	item, err := s.GetRoomAddressingPreferences(ctx, tenantID, roomID)
	if err != nil {
		return model.RoomAddressingPreferences{}, err
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	return item, nil
}
