package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type AISingleUseEventInput struct {
	ExternalID  string
	ActorUserID int64
	TenantID    *int64
	RoomID      *int64
	Source      string
	PayerType   string
	QuotedBeans uint64
	Metadata    map[string]any
	StartedAt   time.Time
}

func (s *Store) StartAISingleUseEvent(ctx context.Context, input AISingleUseEventInput) (string, error) {
	if input.ActorUserID <= 0 {
		return "", errors.New("invalid actor user")
	}
	input.Source = strings.TrimSpace(input.Source)
	input.PayerType = strings.TrimSpace(input.PayerType)
	if input.Source == "" || input.PayerType == "" {
		return "", errors.New("source and payer_type are required")
	}
	if input.QuotedBeans == 0 {
		input.QuotedBeans = 1
	}
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now().UTC()
	}
	if strings.TrimSpace(input.ExternalID) == "" {
		value, err := newLiveReference("AIONE")
		if err != nil {
			return "", err
		}
		input.ExternalID = value
	}
	metadata, _ := json.Marshal(input.Metadata)
	_, err := s.db.ExecContext(ctx, `
        INSERT INTO ai_single_use_events (
            external_id, actor_user_id, tenant_id, room_id, source, payer_type,
            status, quoted_beans, charged_beans, metadata_json, started_at
        ) VALUES (?, ?, ?, ?, ?, ?, 'started', ?, 0, ?, ?)
    `, input.ExternalID, input.ActorUserID, nullableSingleUseInt64(input.TenantID), nullableSingleUseInt64(input.RoomID),
		input.Source, input.PayerType, input.QuotedBeans, nullableSingleUseJSON(metadata), input.StartedAt)
	if err != nil {
		return "", err
	}
	return input.ExternalID, nil
}

func (s *Store) FinishAISingleUseEvent(
	ctx context.Context,
	externalID string,
	status string,
	provider string,
	modelName string,
	latencyMS int64,
	metadata map[string]any,
	completedAt time.Time,
) error {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil
	}
	status = strings.TrimSpace(status)
	if status != "succeeded" && status != "failed" {
		return errors.New("invalid single use status")
	}
	if completedAt.IsZero() {
		completedAt = time.Now().UTC()
	}
	raw, _ := json.Marshal(metadata)
	result, err := s.db.ExecContext(ctx, `
        UPDATE ai_single_use_events
        SET status=?, charged_beans=0, provider=?, model=?, latency_ms=?,
            metadata_json=CASE WHEN ? IS NULL THEN metadata_json ELSE ? END,
            completed_at=?, updated_at=CURRENT_TIMESTAMP(3)
        WHERE external_id=? AND status='started'
    `, status, strings.TrimSpace(provider), strings.TrimSpace(modelName), latencyMS,
		nullableSingleUseJSON(raw), nullableSingleUseJSON(raw), completedAt, externalID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		var existing string
		err := s.db.QueryRowContext(ctx, `SELECT status FROM ai_single_use_events WHERE external_id=?`, externalID).Scan(&existing)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	return nil
}

func nullableSingleUseInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableSingleUseJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	return string(raw)
}
