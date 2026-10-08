package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveSellingPointWheelNotFound = errors.New("live selling point wheel not found")

const liveSellingPointWheelSelect = `
	SELECT id, tenant_id, plan_id, extraction_prompt_md, prompt_source,
		fact_package_sha256, original_wheel_json, COALESCE(confirmed_wheel_json, ''),
		request_system, request_user, raw_response, provider, model,
		latency_ms, input_tokens, output_tokens, status, active,
		created_by_user_id, confirmed_by_user_id, created_at, confirmed_at
	FROM live_selling_point_wheels
`

func scanLiveSellingPointWheel(row interface{ Scan(...any) error }) (model.LiveSellingPointWheelAsset, error) {
	var item model.LiveSellingPointWheelAsset
	var active bool
	var createdBy, confirmedBy sql.NullInt64
	var confirmedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.ExtractionPromptMD, &item.PromptSource,
		&item.FactPackageSHA256, &item.OriginalWheelJSON, &item.ConfirmedWheelJSON,
		&item.RequestSystem, &item.RequestUser, &item.RawResponse, &item.Provider, &item.Model,
		&item.LatencyMS, &item.InputTokens, &item.OutputTokens, &item.Status, &active,
		&createdBy, &confirmedBy, &item.CreatedAt, &confirmedAt,
	); err != nil {
		return item, err
	}
	item.Active = active
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if confirmedBy.Valid {
		value := confirmedBy.Int64
		item.ConfirmedByUserID = &value
	}
	if confirmedAt.Valid {
		value := confirmedAt.Time
		item.ConfirmedAt = &value
	}
	return item, nil
}

func (s *Store) SaveLiveSellingPointWheel(ctx context.Context, tenantID, planID, actorID int64, input model.SaveLiveSellingPointWheelInput) (model.LiveSellingPointWheelAsset, error) {
	status := input.Status
	if status == "" {
		status = "draft"
	}
	if status != "draft" && status != "validation_failed" {
		return model.LiveSellingPointWheelAsset{}, errors.New("卖点轮盘状态无效")
	}
	active := status == "draft"
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveSellingPointWheelAsset{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if active {
		if _, err := tx.ExecContext(ctx, `UPDATE live_selling_point_wheels SET active=0 WHERE tenant_id=? AND plan_id=? AND active=1`, tenantID, planID); err != nil {
			return model.LiveSellingPointWheelAsset{}, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_selling_point_wheels (
			tenant_id, plan_id, extraction_prompt_md, prompt_source, fact_package_sha256,
			original_wheel_json, request_system, request_user, raw_response,
			provider, model, latency_ms, input_tokens, output_tokens,
			status, active, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, tenantID, planID, strings.TrimSpace(input.ExtractionPromptMD), strings.TrimSpace(input.PromptSource),
		strings.TrimSpace(input.FactPackageSHA256), strings.TrimSpace(input.OriginalWheelJSON),
		input.RequestSystem, input.RequestUser, input.RawResponse, strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Model), input.LatencyMS, input.InputTokens, input.OutputTokens, status, active, actorID)
	if err != nil {
		return model.LiveSellingPointWheelAsset{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveSellingPointWheelAsset{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveSellingPointWheelAsset{}, err
	}
	return s.GetLiveSellingPointWheel(ctx, tenantID, planID, id)
}

func (s *Store) GetLiveSellingPointWheel(ctx context.Context, tenantID, planID, assetID int64) (model.LiveSellingPointWheelAsset, error) {
	item, err := scanLiveSellingPointWheel(s.db.QueryRowContext(ctx, liveSellingPointWheelSelect+` WHERE tenant_id=? AND plan_id=? AND id=? LIMIT 1`, tenantID, planID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveSellingPointWheelNotFound
	}
	return item, err
}

func (s *Store) GetActiveLiveSellingPointWheel(ctx context.Context, tenantID, planID int64) (model.LiveSellingPointWheelAsset, error) {
	item, err := scanLiveSellingPointWheel(s.db.QueryRowContext(ctx, liveSellingPointWheelSelect+` WHERE tenant_id=? AND plan_id=? AND active=1 ORDER BY created_at DESC, id DESC LIMIT 1`, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveSellingPointWheelNotFound
	}
	return item, err
}

func (s *Store) ConfirmLiveSellingPointWheel(ctx context.Context, tenantID, planID, assetID, actorID int64, confirmed string) (model.LiveSellingPointWheelAsset, error) {
	confirmed = strings.TrimSpace(confirmed)
	if confirmed == "" {
		return model.LiveSellingPointWheelAsset{}, errors.New("卖点轮盘不能为空")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_selling_point_wheels
		SET confirmed_wheel_json=?, status='confirmed', active=1,
			confirmed_by_user_id=?, confirmed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND active=1
	`, confirmed, actorID, assetID, tenantID, planID)
	if err != nil {
		return model.LiveSellingPointWheelAsset{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveSellingPointWheelAsset{}, ErrLiveSellingPointWheelNotFound
	}
	return s.GetLiveSellingPointWheel(ctx, tenantID, planID, assetID)
}
