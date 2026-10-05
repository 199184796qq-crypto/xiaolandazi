package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Store) CreateMediaAsset(ctx context.Context, input model.CreateMediaAssetInput) (model.MediaAsset, error) {
	metadataJSON, err := json.Marshal(defaultJSONMap(input.Metadata))
	if err != nil {
		return model.MediaAsset{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO media_assets (
			tenant_id, agent_id, asset_type, original_name,
			storage_driver, storage_bucket, object_key,
			mime_type, size_bytes, duration_ms, checksum_sha256,
			status, metadata_json, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', CAST(? AS JSON), ?)
	`,
		input.TenantID,
		input.AgentID,
		strings.TrimSpace(input.AssetType),
		strings.TrimSpace(input.OriginalName),
		strings.TrimSpace(input.StorageDriver),
		strings.TrimSpace(input.StorageBucket),
		strings.TrimSpace(input.ObjectKey),
		strings.TrimSpace(input.MIMEType),
		input.SizeBytes,
		input.DurationMS,
		strings.TrimSpace(input.ChecksumSHA256),
		string(metadataJSON),
		input.CreatedByUserID,
	)
	if err != nil {
		return model.MediaAsset{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.MediaAsset{}, err
	}
	return s.GetMediaAsset(ctx, input.TenantID, id)
}

func (s *Store) ListMediaAssets(ctx context.Context, tenantID int64, assetType string) ([]model.MediaAsset, error) {
	query := `
		SELECT id, tenant_id, agent_id, asset_type, original_name,
		       storage_driver, storage_bucket, object_key, mime_type,
		       size_bytes, duration_ms, checksum_sha256, status,
		       CAST(metadata_json AS CHAR), created_by_user_id, created_at, updated_at
		FROM media_assets
		WHERE tenant_id=? AND status<>'deleted'
	`
	args := []any{tenantID}
	if assetType = strings.TrimSpace(assetType); assetType != "" {
		query += " AND asset_type=?"
		args = append(args, assetType)
	}
	query += " ORDER BY id DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.MediaAsset, 0)
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetMediaAsset(ctx context.Context, tenantID, assetID int64) (model.MediaAsset, error) {
	return scanMediaAsset(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, asset_type, original_name,
		       storage_driver, storage_bucket, object_key, mime_type,
		       size_bytes, duration_ms, checksum_sha256, status,
		       CAST(metadata_json AS CHAR), created_by_user_id, created_at, updated_at
		FROM media_assets
		WHERE id=? AND tenant_id=? AND status<>'deleted'
		LIMIT 1
	`, assetID, tenantID))
}

func (s *Store) DeleteMediaAsset(ctx context.Context, tenantID, assetID int64) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE media_assets
		SET status='deleted', updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND status<>'deleted'
	`, assetID, tenantID)
	if err != nil {
		return err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type mediaAssetScanner interface {
	Scan(...any) error
}

func scanMediaAsset(scanner mediaAssetScanner) (model.MediaAsset, error) {
	var item model.MediaAsset
	var agentID sql.NullInt64
	var duration sql.NullInt64
	var createdBy sql.NullInt64
	var metadataRaw string
	if err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&agentID,
		&item.AssetType,
		&item.OriginalName,
		&item.StorageDriver,
		&item.StorageBucket,
		&item.ObjectKey,
		&item.MIMEType,
		&item.SizeBytes,
		&duration,
		&item.ChecksumSHA256,
		&item.Status,
		&metadataRaw,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.MediaAsset{}, err
	}
	if agentID.Valid {
		value := agentID.Int64
		item.AgentID = &value
	}
	if duration.Valid && duration.Int64 >= 0 {
		value := uint64(duration.Int64)
		item.DurationMS = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	_ = json.Unmarshal([]byte(metadataRaw), &item.Metadata)
	return item, nil
}

func (s *Store) SaveVoiceProfile(
	ctx context.Context,
	tenantID, userID, profileID int64,
	input model.VoiceProfileInput,
) (model.VoiceProfile, error) {
	configJSON, err := json.Marshal(defaultJSONMap(input.Config))
	if err != nil {
		return model.VoiceProfile{}, err
	}
	if input.CloneStatus = strings.TrimSpace(input.CloneStatus); input.CloneStatus == "" {
		input.CloneStatus = "pending"
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.TrimSpace(input.Provider)
	input.VoiceID = strings.TrimSpace(input.VoiceID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.VoiceProfile{}, err
	}
	defer tx.Rollback()

	if input.SampleAssetID != nil {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM media_assets
			WHERE id=? AND tenant_id=? AND status='active'
		`, *input.SampleAssetID, tenantID).Scan(&count); err != nil {
			return model.VoiceProfile{}, err
		}
		if count == 0 {
			return model.VoiceProfile{}, sql.ErrNoRows
		}
	}
	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `
			UPDATE voice_profiles SET is_default=0 WHERE tenant_id=?
		`, tenantID); err != nil {
			return model.VoiceProfile{}, err
		}
	}

	if profileID <= 0 {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO voice_profiles (
				tenant_id, agent_id, name, provider, voice_id,
				sample_asset_id, clone_status, config_json, is_default,
				created_by_user_id, updated_by_user_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?, ?, ?)
		`,
			tenantID, input.AgentID, input.Name, input.Provider, input.VoiceID,
			input.SampleAssetID, input.CloneStatus, string(configJSON), input.IsDefault,
			userID, userID,
		)
		if err != nil {
			return model.VoiceProfile{}, err
		}
		profileID, err = result.LastInsertId()
		if err != nil {
			return model.VoiceProfile{}, err
		}
	} else {
		result, err := tx.ExecContext(ctx, `
			UPDATE voice_profiles
			SET agent_id=?, name=?, provider=?, voice_id=?,
			    sample_asset_id=?, clone_status=?, config_json=CAST(? AS JSON),
			    is_default=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=?
		`,
			input.AgentID, input.Name, input.Provider, input.VoiceID,
			input.SampleAssetID, input.CloneStatus, string(configJSON),
			input.IsDefault, userID, profileID, tenantID,
		)
		if err != nil {
			return model.VoiceProfile{}, err
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			return model.VoiceProfile{}, sql.ErrNoRows
		}
	}
	if err := tx.Commit(); err != nil {
		return model.VoiceProfile{}, err
	}
	return s.GetVoiceProfile(ctx, tenantID, profileID)
}

func (s *Store) ListVoiceProfiles(ctx context.Context, tenantID int64) ([]model.VoiceProfile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, name, provider, voice_id,
		       sample_asset_id, clone_status, CAST(config_json AS CHAR), is_default,
		       created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM voice_profiles
		WHERE tenant_id=? AND clone_status<>'disabled'
		ORDER BY is_default DESC, id DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.VoiceProfile, 0)
	for rows.Next() {
		item, err := scanVoiceProfile(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CountActiveVoiceProfiles(ctx context.Context, tenantID int64) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM voice_profiles
		WHERE tenant_id=? AND clone_status<>'disabled'
	`, tenantID).Scan(&count)
	return count, err
}

func (s *Store) DisableVoiceProfile(ctx context.Context, tenantID, profileID, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE voice_profiles
		SET clone_status='disabled', is_default=0, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND clone_status<>'disabled'
	`, userID, profileID, tenantID)
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
	if _, err := tx.ExecContext(ctx, `
		UPDATE voice_model_bindings
		SET status='disabled', updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND profile_id=? AND status<>'disabled'
	`, tenantID, profileID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetVoiceProfile(ctx context.Context, tenantID, profileID int64) (model.VoiceProfile, error) {
	return scanVoiceProfile(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, name, provider, voice_id,
		       sample_asset_id, clone_status, CAST(config_json AS CHAR), is_default,
		       created_by_user_id, updated_by_user_id, created_at, updated_at
		FROM voice_profiles
		WHERE id=? AND tenant_id=?
		LIMIT 1
	`, profileID, tenantID))
}

func (s *Store) CreateVoiceModelBinding(
	ctx context.Context,
	tenantID, userID int64,
	input model.VoiceModelBindingInput,
) (model.VoiceModelBinding, error) {
	input.Provider = strings.TrimSpace(input.Provider)
	input.Model = strings.TrimSpace(input.Model)
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	input.Status = strings.TrimSpace(input.Status)
	if input.Status == "" {
		input.Status = "ready"
	}
	if input.Rate == 0 {
		input.Rate = 1
	}
	configJSON, err := json.Marshal(defaultJSONMap(input.Config))
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	var profileCount, sampleCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM voice_profiles WHERE id=? AND tenant_id=?
	`, input.ProfileID, tenantID).Scan(&profileCount); err != nil {
		return model.VoiceModelBinding{}, err
	}
	if profileCount == 0 {
		return model.VoiceModelBinding{}, sql.ErrNoRows
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM media_assets WHERE id=? AND tenant_id=? AND status='active'
	`, input.SampleAssetID, tenantID).Scan(&sampleCount); err != nil {
		return model.VoiceModelBinding{}, err
	}
	if sampleCount == 0 {
		return model.VoiceModelBinding{}, sql.ErrNoRows
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO voice_model_bindings (
			tenant_id, profile_id, sample_asset_id, provider, model, voice_id,
			rate, status, config_json, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CAST(? AS JSON), ?)
	`,
		tenantID, input.ProfileID, input.SampleAssetID, input.Provider, input.Model, input.VoiceID,
		input.Rate, input.Status, string(configJSON), userID,
	)
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.VoiceModelBinding{}, err
	}
	return s.GetVoiceModelBinding(ctx, tenantID, id)
}

func (s *Store) GetVoiceModelBinding(ctx context.Context, tenantID, bindingID int64) (model.VoiceModelBinding, error) {
	return scanVoiceModelBinding(s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, profile_id, sample_asset_id, provider, model, voice_id,
		       rate, status, CAST(config_json AS CHAR), created_by_user_id, created_at, updated_at
		FROM voice_model_bindings
		WHERE id=? AND tenant_id=?
		LIMIT 1
	`, bindingID, tenantID))
}

func (s *Store) ListVoiceModelBindings(ctx context.Context, tenantID, profileID int64) ([]model.VoiceModelBinding, error) {
	query := `
		SELECT id, tenant_id, profile_id, sample_asset_id, provider, model, voice_id,
		       rate, status, CAST(config_json AS CHAR), created_by_user_id, created_at, updated_at
		FROM voice_model_bindings
		WHERE tenant_id=? AND status<>'disabled'
	`
	args := []any{tenantID}
	if profileID > 0 {
		query += " AND profile_id=?"
		args = append(args, profileID)
	}
	query += " ORDER BY id DESC"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.VoiceModelBinding, 0)
	for rows.Next() {
		item, err := scanVoiceModelBinding(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type voiceModelBindingScanner interface {
	Scan(...any) error
}

func scanVoiceModelBinding(scanner voiceModelBindingScanner) (model.VoiceModelBinding, error) {
	var item model.VoiceModelBinding
	var configRaw string
	var createdBy sql.NullInt64
	if err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&item.ProfileID,
		&item.SampleAssetID,
		&item.Provider,
		&item.Model,
		&item.VoiceID,
		&item.Rate,
		&item.Status,
		&configRaw,
		&createdBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.VoiceModelBinding{}, err
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	_ = json.Unmarshal([]byte(configRaw), &item.Config)
	return item, nil
}

type voiceProfileScanner interface {
	Scan(...any) error
}

func scanVoiceProfile(scanner voiceProfileScanner) (model.VoiceProfile, error) {
	var item model.VoiceProfile
	var agentID, sampleAssetID, createdBy, updatedBy sql.NullInt64
	var configRaw string
	if err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&agentID,
		&item.Name,
		&item.Provider,
		&item.VoiceID,
		&sampleAssetID,
		&item.CloneStatus,
		&configRaw,
		&item.IsDefault,
		&createdBy,
		&updatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.VoiceProfile{}, err
	}
	if agentID.Valid {
		value := agentID.Int64
		item.AgentID = &value
	}
	if sampleAssetID.Valid {
		value := sampleAssetID.Int64
		item.SampleAssetID = &value
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	_ = json.Unmarshal([]byte(configRaw), &item.Config)
	return item, nil
}

func defaultJSONMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
