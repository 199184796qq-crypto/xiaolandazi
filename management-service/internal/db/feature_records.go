package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Store) ListFeatureRecords(ctx context.Context, featureKey string) ([]model.FeatureRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, feature_key, record_key, title, status, sort_order,
		       COALESCE(CAST(payload_json AS CHAR), '{}'),
		       created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM sys_feature_records
		WHERE feature_key=?
		ORDER BY sort_order ASC, id ASC
	`, strings.TrimSpace(featureKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.FeatureRecord, 0)
	for rows.Next() {
		var item model.FeatureRecord
		var createdBy sql.NullInt64
		var updatedBy sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.FeatureKey,
			&item.RecordKey,
			&item.Title,
			&item.Status,
			&item.SortOrder,
			&item.PayloadJSON,
			&createdBy,
			&updatedBy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if createdBy.Valid {
			v := createdBy.Int64
			item.CreatedByUserID = &v
		}
		if updatedBy.Valid {
			v := updatedBy.Int64
			item.UpdatedByUserID = &v
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateFeatureRecord(
	ctx context.Context,
	featureKey string,
	userID int64,
	input model.FeatureRecordInput,
) (model.FeatureRecord, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO sys_feature_records (
			feature_key, record_key, title, status, sort_order, payload_json,
			created_by_user_id, updated_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?)
	`,
		strings.TrimSpace(featureKey),
		strings.TrimSpace(input.RecordKey),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Status),
		input.SortOrder,
		normalizeFeaturePayload(input.PayloadJSON),
		userID,
		userID,
	)
	if err != nil {
		return model.FeatureRecord{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.FeatureRecord{}, err
	}
	return s.GetFeatureRecord(ctx, featureKey, id)
}

func (s *Store) UpdateFeatureRecord(
	ctx context.Context,
	featureKey string,
	recordID int64,
	userID int64,
	input model.FeatureRecordInput,
) (model.FeatureRecord, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sys_feature_records
		SET record_key=?, title=?, status=?, sort_order=?,
		    payload_json=CAST(? AS JSON), updated_by_user_id=?
		WHERE id=? AND feature_key=?
	`,
		strings.TrimSpace(input.RecordKey),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.Status),
		input.SortOrder,
		normalizeFeaturePayload(input.PayloadJSON),
		userID,
		recordID,
		strings.TrimSpace(featureKey),
	)
	if err != nil {
		return model.FeatureRecord{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.FeatureRecord{}, err
	}
	if affected == 0 {
		return model.FeatureRecord{}, sql.ErrNoRows
	}
	return s.GetFeatureRecord(ctx, featureKey, recordID)
}

func (s *Store) DeleteFeatureRecord(
	ctx context.Context,
	featureKey string,
	recordID int64,
) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM sys_feature_records
		WHERE id=? AND feature_key=?
	`, recordID, strings.TrimSpace(featureKey))
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) GetFeatureRecord(
	ctx context.Context,
	featureKey string,
	recordID int64,
) (model.FeatureRecord, error) {
	var item model.FeatureRecord
	var createdBy sql.NullInt64
	var updatedBy sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, feature_key, record_key, title, status, sort_order,
		       COALESCE(CAST(payload_json AS CHAR), '{}'),
		       created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM sys_feature_records
		WHERE id=? AND feature_key=?
	`, recordID, strings.TrimSpace(featureKey)).Scan(
		&item.ID,
		&item.FeatureKey,
		&item.RecordKey,
		&item.Title,
		&item.Status,
		&item.SortOrder,
		&item.PayloadJSON,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.FeatureRecord{}, err
	}
	if createdBy.Valid {
		v := createdBy.Int64
		item.CreatedByUserID = &v
	}
	if updatedBy.Valid {
		v := updatedBy.Int64
		item.UpdatedByUserID = &v
	}
	return item, nil
}

func normalizeFeaturePayload(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	return value
}

func (s *Store) EnsureFeatureSeed(
	ctx context.Context,
	featureKey string,
	recordKey string,
	title string,
	status string,
	sortOrder int,
	payloadJSON string,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO sys_feature_records (
			feature_key, record_key, title, status, sort_order, payload_json
		)
		VALUES (?, ?, ?, ?, ?, CAST(? AS JSON))
	`,
		strings.TrimSpace(featureKey),
		strings.TrimSpace(recordKey),
		strings.TrimSpace(title),
		strings.TrimSpace(status),
		sortOrder,
		normalizeFeaturePayload(payloadJSON),
	)
	if err != nil {
		return fmt.Errorf("seed feature record: %w", err)
	}
	return nil
}
