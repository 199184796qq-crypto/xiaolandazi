package db

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/contentpools"
)

func (s *Store) MigrateContentCorrections(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_plan_content_corrections (id VARCHAR(64) NOT NULL,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,data_json JSON NOT NULL,created_by BIGINT UNSIGNED NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),PRIMARY KEY(tenant_id,plan_id,id),KEY idx_content_correction(tenant_id,plan_id,created_at,id)) ENGINE=InnoDB`)
	return e
}
func (s *Store) ListContentCorrections(ctx context.Context, tenant, plan int64) ([]contentpools.Correction, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT CAST(data_json AS CHAR) FROM live_plan_content_corrections WHERE tenant_id=? AND plan_id=? ORDER BY created_at,id`, tenant, plan)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []contentpools.Correction{}
	for rows.Next() {
		var raw []byte
		var c contentpools.Correction
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		if e = json.Unmarshal(raw, &c); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Rule and repaired library commit together; immutable published/live versions stay frozen.
func (s *Store) SaveContentCorrection(ctx context.Context, tenant, plan, actor int64, original contentpools.Set, next contentpools.Set, c contentpools.Correction, job PoolJob) (contentpools.Set, error) {
	if c.Content.Question != "" {
		return next, errors.New("纠正尚未澄清，不允许采纳")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return next, e
	}
	defer tx.Rollback()
	var rev int64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&rev); e != nil {
		return next, e
	}
	if rev != original.Sources.Revision {
		return next, ErrPoolSourceChanged
	}
	var id int64
	var sha string
	if e = tx.QueryRowContext(ctx, `SELECT id,source_sha FROM live_plan_content_sets WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&id, &sha); e != nil {
		return next, e
	}
	if id != original.ID || sha != original.SourceSHA {
		return next, errors.New("内容已更新，纠正未覆盖新内容，请刷新后重试")
	}
	var running int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_plan_content_jobs WHERE tenant_id=? AND plan_id=? AND id<>? AND status='running' AND lease_until>CURRENT_TIMESTAMP(3)`, tenant, plan, job.ID).Scan(&running); e != nil {
		return next, e
	}
	if running > 0 {
		return next, ErrPoolJobRunning
	}
	var status string
	if e = tx.QueryRowContext(ctx, `SELECT status FROM live_plan_content_jobs WHERE tenant_id=? AND plan_id=? AND id=? AND lease_until>CURRENT_TIMESTAMP(3) FOR UPDATE`, tenant, plan, job.ID).Scan(&status); e != nil {
		return next, e
	}
	if status != "running" {
		return next, ErrPoolSourceChanged
	}
	b, e := json.Marshal(c)
	if e != nil {
		return next, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO live_plan_content_corrections(id,tenant_id,plan_id,data_json,created_by) VALUES(?,?,?,?,?)`, c.ID, tenant, plan, b, actor); e != nil {
		return next, e
	}
	next.Sources.Revision = rev + 1
	next.SourceSHA = next.Sources.SHA()
	if e = contentpools.ValidateSet(next); e != nil {
		return next, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE live_plan_content_revisions SET revision=revision+1 WHERE tenant_id=? AND plan_id=?`, tenant, plan); e != nil {
		return next, e
	}
	next, e = insertContentPoolSetTx(ctx, tx, tenant, plan, actor, next)
	if e != nil {
		return next, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE live_plan_content_jobs SET status='ready',stage='纠正与关联内容已保存，等待保存智能体',result_set_id=?,lease_until=NULL,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=?`, next.ID, tenant, plan, job.ID); e != nil {
		return next, e
	}
	return next, tx.Commit()
}
