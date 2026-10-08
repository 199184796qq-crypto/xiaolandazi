package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Choose an immutable asset created before the run. A new analysis or disable
// version never changes an already running session's content strategy.
func (s *Store) GetLatestPlanContentStrategy(ctx context.Context, tenant, plan int64, factsSHA string, before time.Time) (string, error) {
	if before.IsZero() {
		before = time.Now().UTC()
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT result_json FROM live_plan_analysis_assets
		WHERE tenant_id=? AND plan_id=? AND asset_kind='fact_purpose_angles_v1'
		AND (?='' OR JSON_UNQUOTE(JSON_EXTRACT(result_json,'$.facts_sha256'))=?) AND created_at<=?
		ORDER BY created_at DESC,input_sha256 DESC LIMIT 1`, tenant, plan, factsSHA, factsSHA, before.UTC()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPlanAnalysisAssetNotFound
	}
	return raw, err
}
