package db

import (
	"context"
	"encoding/json"
	"errors"

	"livecompanion/management/internal/contentpools"
)

// Manual edits create a new complete snapshot. The same revision lock used by
// generation and agent saves serializes concurrent edits and source changes.
func (s *Store) EditContentPool(ctx context.Context, tenant, plan, actor int64, source contentpools.Sources, edit contentpools.PoolEdit) (contentpools.Set, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contentpools.Set{}, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&revision); err != nil {
		return contentpools.Set{}, err
	}
	if revision != source.Revision {
		return contentpools.Set{}, ErrPoolSourceChanged
	}
	var running int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_plan_content_jobs WHERE tenant_id=? AND plan_id=? AND status='running' AND lease_until>CURRENT_TIMESTAMP(3)`, tenant, plan).Scan(&running); err != nil {
		return contentpools.Set{}, err
	}
	if running > 0 {
		return contentpools.Set{}, ErrPoolJobRunning
	}
	var id int64
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT id,CAST(data_json AS CHAR) FROM live_plan_content_sets WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&id, &raw); err != nil {
		return contentpools.Set{}, err
	}
	if id != edit.ExpectedSetID {
		return contentpools.Set{}, errors.New("内容已被更新，请刷新后重新编辑；你的编辑草稿已保留")
	}
	var old contentpools.Set
	if err = json.Unmarshal(raw, &old); err != nil {
		return old, err
	}
	old.ID = id
	if old.SourceSHA != source.SHA() {
		return old, ErrPoolSourceChanged
	}
	next, err := contentpools.ApplyEdit(old, edit)
	if err != nil {
		return old, err
	}
	// Invalidates in-flight saves of the previous set without changing source
	// hashes or rewriting any saved/live version.
	next.Sources.Revision = revision + 1
	if _, err = tx.ExecContext(ctx, `UPDATE live_plan_content_revisions SET revision=revision+1 WHERE tenant_id=? AND plan_id=?`, tenant, plan); err != nil {
		return old, err
	}
	next, err = insertContentPoolSetTx(ctx, tx, tenant, plan, actor, next)
	if err != nil {
		return old, err
	}
	return next, tx.Commit()
}
