package db

import (
	"context"
	"database/sql"
	"errors"
)

var ErrPlanAnalysisAssetNotFound = errors.New("plan analysis asset not found")

// Stable, validated plan analysis only. Never stores running cursors or leases.
func (s *Store) GetPlanAnalysisAsset(ctx context.Context, tenant, plan int64, kind, fingerprint string) (string, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT result_json FROM live_plan_analysis_assets WHERE tenant_id=? AND plan_id=? AND asset_kind=? AND input_sha256=?`, tenant, plan, kind, fingerprint).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPlanAnalysisAssetNotFound
	}
	return raw, err
}
func (s *Store) SavePlanAnalysisAsset(ctx context.Context, tenant, plan int64, kind, fingerprint, raw string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO live_plan_analysis_assets(tenant_id,plan_id,asset_kind,input_sha256,result_json) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE input_sha256=VALUES(input_sha256)`, tenant, plan, kind, fingerprint, raw)
	return err
}
