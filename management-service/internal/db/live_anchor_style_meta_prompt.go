package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAnchorStyleMetaPromptNotFound = errors.New("live anchor style meta prompt not found")

const liveAnchorStyleMetaPromptSelect = `
	SELECT id, tenant_id, plan_id, scene, sample_sha256,
		original_meta_prompt_md, COALESCE(confirmed_meta_prompt_md, ''), monitor_json,
		analysis_prompt_source, request_system, request_user, raw_response,
		provider, model, latency_ms, input_tokens, output_tokens, status, active,
		created_by_user_id, confirmed_by_user_id, created_at, confirmed_at
	FROM live_anchor_style_meta_prompts
`

func scanLiveAnchorStyleMetaPrompt(row interface{ Scan(...any) error }) (model.LiveAnchorStyleMetaPromptAsset, error) {
	var item model.LiveAnchorStyleMetaPromptAsset
	var active bool
	var createdBy, confirmedBy sql.NullInt64
	var confirmedAt sql.NullTime
	if err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.Scene, &item.SampleSHA256,
		&item.OriginalMetaPromptMD, &item.ConfirmedMetaPromptMD, &item.MonitorJSON,
		&item.AnalysisPromptSource, &item.RequestSystem, &item.RequestUser, &item.RawResponse,
		&item.Provider, &item.Model, &item.LatencyMS, &item.InputTokens, &item.OutputTokens,
		&item.Status, &active, &createdBy, &confirmedBy, &item.CreatedAt, &confirmedAt,
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

// SaveLiveAnchorStyleMetaPrompt stores every compiler output as a new version.
// Older versions stay in the database; only the newest version is active.
func (s *Store) SaveLiveAnchorStyleMetaPrompt(ctx context.Context, tenantID, planID, actorID int64, input model.SaveLiveAnchorStyleMetaPromptInput) (model.LiveAnchorStyleMetaPromptAsset, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE live_anchor_style_meta_prompts SET active=0 WHERE tenant_id=? AND plan_id=? AND active=1`, tenantID, planID); err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_anchor_style_meta_prompts (
			tenant_id, plan_id, scene, sample_sha256, original_meta_prompt_md,
			monitor_json, analysis_prompt_source, request_system, request_user,
			raw_response, provider, model, latency_ms, input_tokens, output_tokens,
			status, active, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', 1, ?)
	`, tenantID, planID, strings.TrimSpace(input.Scene), strings.TrimSpace(input.SampleSHA256),
		strings.TrimSpace(input.OriginalMetaPromptMD), input.MonitorJSON, strings.TrimSpace(input.AnalysisPromptSource),
		input.RequestSystem, input.RequestUser, input.RawResponse, strings.TrimSpace(input.Provider),
		strings.TrimSpace(input.Model), input.LatencyMS, input.InputTokens, input.OutputTokens, actorID)
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	return s.GetLiveAnchorStyleMetaPrompt(ctx, tenantID, planID, id)
}

func (s *Store) GetLiveAnchorStyleMetaPrompt(ctx context.Context, tenantID, planID, assetID int64) (model.LiveAnchorStyleMetaPromptAsset, error) {
	item, err := scanLiveAnchorStyleMetaPrompt(s.db.QueryRowContext(ctx, liveAnchorStyleMetaPromptSelect+` WHERE tenant_id=? AND plan_id=? AND id=? LIMIT 1`, tenantID, planID, assetID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveAnchorStyleMetaPromptNotFound
	}
	return item, err
}

func (s *Store) GetActiveLiveAnchorStyleMetaPrompt(ctx context.Context, tenantID, planID int64) (model.LiveAnchorStyleMetaPromptAsset, error) {
	item, err := scanLiveAnchorStyleMetaPrompt(s.db.QueryRowContext(ctx, liveAnchorStyleMetaPromptSelect+` WHERE tenant_id=? AND plan_id=? AND active=1 ORDER BY created_at DESC, id DESC LIMIT 1`, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrLiveAnchorStyleMetaPromptNotFound
	}
	return item, err
}

func (s *Store) ConfirmLiveAnchorStyleMetaPrompt(ctx context.Context, tenantID, planID, assetID, actorID int64, confirmed string) (model.LiveAnchorStyleMetaPromptAsset, error) {
	confirmed = strings.TrimSpace(confirmed)
	if confirmed == "" {
		return model.LiveAnchorStyleMetaPromptAsset{}, errors.New("元提示词不能为空")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_anchor_style_meta_prompts
		SET confirmed_meta_prompt_md=?, status='confirmed', active=1,
			confirmed_by_user_id=?, confirmed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND active=1
	`, confirmed, actorID, assetID, tenantID, planID)
	if err != nil {
		return model.LiveAnchorStyleMetaPromptAsset{}, err
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return model.LiveAnchorStyleMetaPromptAsset{}, ErrLiveAnchorStyleMetaPromptNotFound
	}
	return s.GetLiveAnchorStyleMetaPrompt(ctx, tenantID, planID, assetID)
}
