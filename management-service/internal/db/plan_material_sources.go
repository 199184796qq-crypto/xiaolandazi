package db

import (
	"context"
	"database/sql"
)

const materialSourcesDDL = `CREATE TABLE IF NOT EXISTS live_agent_plan_material_sources (
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    source_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    original_text MEDIUMTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, plan_id, source_id)
) ENGINE=InnoDB`

func (s *Store) SaveMaterialSource(ctx context.Context, tenantID, planID int64, id, original string) error {
	return saveMaterialSource(ctx, s.db, tenantID, planID, id, original)
}

func saveMaterialSource(ctx context.Context, conn interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, tenantID, planID int64, id, original string) error {
	_, err := conn.ExecContext(ctx, `INSERT INTO live_agent_plan_material_sources (tenant_id,plan_id,source_id,original_text)
        VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE source_id=live_agent_plan_material_sources.source_id`, tenantID, planID, id, original)
	return err
}

func (s *Store) MaterialSource(ctx context.Context, tenantID, planID int64, id string) (string, error) {
	var original string
	err := s.db.QueryRowContext(ctx, `SELECT original_text FROM live_agent_plan_material_sources
        WHERE tenant_id=? AND plan_id=? AND source_id=?`, tenantID, planID, id).Scan(&original)
	if err == sql.ErrNoRows {
		return "", ErrMaterialClaimNotFound
	}
	return original, err
}
