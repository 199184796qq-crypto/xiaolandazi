package db

import (
	"context"
	"database/sql"
	"fmt"

	"livecompanion/management/internal/contextword"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
)

type AnswerSnapshot struct {
	Records  []MaterialRecallRecord
	Objects  model.MaterialObjectCatalog
	Links    []QALink
	Words    contextword.Catalog
	QA       []qapair.Record
	Revision string
}

// All sources share one read-only MVCC snapshot. The transaction ends before
// any remote model/vector call. No repair or embedding purchase occurs here.
func (s *Store) PlanAnswerSnapshot(ctx context.Context, tenant, plan int64) (AnswerSnapshot, error) {
	var out AnswerSnapshot
	if tenant <= 0 || plan <= 0 {
		return out, fmt.Errorf("回答资料身份无效")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=?`, tenant, plan).Scan(&id); err != nil {
		return out, err
	}
	if out.Records, err = materialRecallSnapshot(ctx, tx, tenant, plan); err != nil {
		return out, err
	}
	if out.Objects, out.Links, _, err = qaObjects(ctx, tx, tenant, plan); err != nil {
		return out, err
	}
	if out.Words, err = planContextWords(ctx, tx, tenant, plan); err != nil {
		return out, err
	}
	if out.QA, err = listQA(ctx, tx, tenant, plan); err != nil {
		return out, err
	}
	out.Revision = qapair.Hash([]any{tenant, plan, out.Records, out.Objects, out.Links, out.Words, out.QA})
	return out, nil
}
