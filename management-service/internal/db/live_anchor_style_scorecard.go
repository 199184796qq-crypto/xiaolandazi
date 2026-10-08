package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAnchorStyleScorecardNotFound = errors.New("live anchor style scorecard not found")

const liveAnchorStyleScorecardSelect = `
	SELECT id, tenant_id, plan_id, meta_prompt_asset_id, sample_sha256, COALESCE(sample_text, ''),
		extraction_prompt_md, prompt_source, original_scorecard_json,
		COALESCE(confirmed_scorecard_json, ''), request_system, request_user,
		raw_response, provider, model, latency_ms, input_tokens, output_tokens,
		status, COALESCE(calibration_error, ''), active, created_by_user_id, confirmed_by_user_id, created_at, confirmed_at
	FROM live_anchor_style_scorecards
`

func scanLiveAnchorStyleScorecard(row interface{ Scan(...any) error }) (model.LiveAnchorStyleScorecardAsset, error) {
	var item model.LiveAnchorStyleScorecardAsset
	var active bool
	var createdBy, confirmedBy sql.NullInt64
	var confirmedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.MetaPromptAssetID, &item.SampleSHA256, &item.SampleText,
		&item.ExtractionPromptMD, &item.PromptSource, &item.OriginalScorecardJSON,
		&item.ConfirmedScorecardJSON, &item.RequestSystem, &item.RequestUser,
		&item.RawResponse, &item.Provider, &item.Model, &item.LatencyMS,
		&item.InputTokens, &item.OutputTokens, &item.Status, &item.CalibrationError, &active,
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

func (s *Store) SaveLiveAnchorStyleScorecard(ctx context.Context, tenantID, planID, actorID int64, input model.SaveLiveAnchorStyleScorecardInput) (model.LiveAnchorStyleScorecardAsset, error) {
	status := "draft"
	if input.Status == "calibration_failed" {
		status = input.Status
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var currentMetaID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM live_anchor_style_meta_prompts WHERE tenant_id=? AND plan_id=? AND active=1 ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE`, tenantID, planID).Scan(&currentMetaID); err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	if currentMetaID != input.MetaPromptAssetID {
		return model.LiveAnchorStyleScorecardAsset{}, errors.New("风格资产已变化，评分基准未覆盖当前版本")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE live_anchor_style_scorecards SET active=0 WHERE tenant_id=? AND plan_id=? AND active=1`, tenantID, planID); err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_anchor_style_scorecards (
			tenant_id, plan_id, meta_prompt_asset_id, sample_sha256, sample_text,
			extraction_prompt_md, prompt_source, original_scorecard_json,
			request_system, request_user, raw_response, provider, model,
			latency_ms, input_tokens, output_tokens, status, calibration_error, active, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?)
	`, tenantID, planID, input.MetaPromptAssetID, strings.TrimSpace(input.SampleSHA256), input.SampleText,
		input.ExtractionPromptMD, strings.TrimSpace(input.PromptSource), input.OriginalScorecardJSON,
		input.RequestSystem, input.RequestUser, input.RawResponse, strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Model), input.LatencyMS, input.InputTokens, input.OutputTokens, status, input.CalibrationError, actorID)
	if err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	return s.GetLiveAnchorStyleScorecard(ctx, tenantID, planID, id)
}

func (s *Store) GetLiveAnchorStyleScorecard(ctx context.Context, tenantID, planID, assetID int64) (model.LiveAnchorStyleScorecardAsset, error) {
	item, err := scanLiveAnchorStyleScorecard(s.db.QueryRowContext(ctx, liveAnchorStyleScorecardSelect+` WHERE tenant_id=? AND plan_id=? AND id=? LIMIT 1`, tenantID, planID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveAnchorStyleScorecardNotFound
	}
	return item, err
}

func (s *Store) GetActiveLiveAnchorStyleScorecard(ctx context.Context, tenantID, planID int64) (model.LiveAnchorStyleScorecardAsset, error) {
	item, err := scanLiveAnchorStyleScorecard(s.db.QueryRowContext(ctx, liveAnchorStyleScorecardSelect+` WHERE tenant_id=? AND plan_id=? AND active=1 ORDER BY created_at DESC, id DESC LIMIT 1`, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveAnchorStyleScorecardNotFound
	}
	return item, err
}

func (s *Store) ConfirmLiveAnchorStyleScorecard(ctx context.Context, tenantID, planID, assetID, actorID int64, confirmed string) (model.LiveAnchorStyleScorecardAsset, error) {
	confirmed = strings.TrimSpace(confirmed)
	if confirmed == "" {
		return model.LiveAnchorStyleScorecardAsset{}, errors.New("评分基准不能为空")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_anchor_style_scorecards
		SET confirmed_scorecard_json=?, status='confirmed', calibration_error=NULL, active=1,
			confirmed_by_user_id=?, confirmed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND active=1
		  AND EXISTS (
		    SELECT 1 FROM live_anchor_style_meta_prompts m
		    WHERE m.id=live_anchor_style_scorecards.meta_prompt_asset_id
		      AND m.tenant_id=live_anchor_style_scorecards.tenant_id
		      AND m.plan_id=live_anchor_style_scorecards.plan_id AND m.active=1
		  )
	`, confirmed, actorID, assetID, tenantID, planID)
	if err != nil {
		return model.LiveAnchorStyleScorecardAsset{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyleScorecardAsset{}, ErrLiveAnchorStyleScorecardNotFound
	}
	return s.GetLiveAnchorStyleScorecard(ctx, tenantID, planID, assetID)
}
