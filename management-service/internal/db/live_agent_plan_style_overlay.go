package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanStyleOverlayConflict = errors.New("叠加风格已被其他人员修改，请刷新后重试")

func (s *Store) GetLiveAgentPlanStyleOverlay(ctx context.Context, tenantID, planID int64) (model.LiveAgentPlanStyleOverlayProfile, error) {
	result := model.LiveAgentPlanStyleOverlayProfile{TenantID: tenantID, PlanID: planID, Items: []model.LiveAnchorStyleOverlayItem{}}
	var raw []byte
	var updatedBy int64
	var updatedAt time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT items_json, revision, updated_by_user_id, updated_at
		FROM live_agent_plan_style_overlays
		WHERE tenant_id=? AND plan_id=?
	`, tenantID, planID).Scan(&raw, &result.Revision, &updatedBy, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	if err := json.Unmarshal(raw, &result.Items); err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	result.UpdatedByUserID = &updatedBy
	result.UpdatedAt = &updatedAt
	return result, nil
}

func (s *Store) SaveLiveAgentPlanStyleOverlay(ctx context.Context, tenantID, planID, actorID, expectedRevision int64, items []model.LiveAnchorStyleOverlayItem) (model.LiveAgentPlanStyleOverlayProfile, error) {
	raw, err := json.Marshal(items)
	if err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM live_agent_plan_style_overlays WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenantID, planID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	if revision != expectedRevision {
		return model.LiveAgentPlanStyleOverlayProfile{}, ErrLiveAgentPlanStyleOverlayConflict
	}
	if revision == 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_style_overlays (tenant_id, plan_id, items_json, revision, updated_by_user_id)
			VALUES (?, ?, ?, 1, ?)
		`, tenantID, planID, raw, actorID)
	} else {
		_, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_style_overlays
			SET items_json=?, revision=revision+1, updated_by_user_id=?
			WHERE tenant_id=? AND plan_id=?
		`, raw, actorID, tenantID, planID)
	}
	if err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanStyleOverlayProfile{}, err
	}
	return s.GetLiveAgentPlanStyleOverlay(ctx, tenantID, planID)
}
