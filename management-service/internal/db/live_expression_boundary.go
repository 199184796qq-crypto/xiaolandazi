package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/model"
)

var ErrExpressionBoundaryConflict = errors.New("表达尺度已被其他人修改，请刷新后重试")

func (s *Store) GetExpressionBoundaries(ctx context.Context, tenantID, planID int64) (model.LiveExpressionBoundaryProfile, error) {
	profile := model.LiveExpressionBoundaryProfile{TenantID: tenantID, PlanID: planID, Items: []model.LiveExpressionBoundaryRule{}}
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT items_json,revision FROM live_agent_expression_boundaries WHERE tenant_id=? AND plan_id=?`, tenantID, planID).Scan(&data, &profile.Revision)
	if errors.Is(err, sql.ErrNoRows) { return profile, nil }
	if err != nil { return profile, err }
	err = json.Unmarshal(data, &profile.Items)
	return profile, err
}

func (s *Store) SaveExpressionBoundaries(ctx context.Context, tenantID, planID, userID, expected int64, items []model.LiveExpressionBoundaryRule) (model.LiveExpressionBoundaryProfile, error) {
	data, err := json.Marshal(items)
	if err != nil { return model.LiveExpressionBoundaryProfile{}, err }
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil { return model.LiveExpressionBoundaryProfile{}, err }
	defer tx.Rollback()
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM live_agent_expression_boundaries WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenantID, planID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) { return model.LiveExpressionBoundaryProfile{}, err }
	if revision != expected { return model.LiveExpressionBoundaryProfile{}, ErrExpressionBoundaryConflict }
	if revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_expression_boundaries(tenant_id,plan_id,items_json,revision,updated_by_user_id) VALUES(?,?,?,1,?)`, tenantID, planID, data, userID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE live_agent_expression_boundaries SET items_json=?,revision=revision+1,updated_by_user_id=? WHERE tenant_id=? AND plan_id=? AND revision=?`, data, userID, tenantID, planID, expected)
	}
	if err != nil { return model.LiveExpressionBoundaryProfile{}, err }
	if err = tx.Commit(); err != nil { return model.LiveExpressionBoundaryProfile{}, err }
	return s.GetExpressionBoundaries(ctx, tenantID, planID)
}
