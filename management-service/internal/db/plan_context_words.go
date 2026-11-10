package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/contextword"
)

var ErrContextWordsConflict = errors.New("语境词已更新，请刷新后重新保存")

func (s *Store) PlanContextWords(ctx context.Context, tenant, plan int64) (contextword.Catalog, error) {
	out := contextword.Catalog{Items: []contextword.Entry{}}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision,items_json FROM live_agent_plan_context_words WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&out.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out.Items)
	return out, err
}
func (s *Store) SavePlanContextWords(ctx context.Context, tenant, plan, actor int64, input contextword.Catalog) (contextword.Catalog, error) {
	items, err := contextword.Normalize(input.Items)
	if err != nil {
		return contextword.Catalog{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contextword.Catalog{}, err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, plan).Scan(&id); err != nil {
		return contextword.Catalog{}, err
	}
	var current int64
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,items_json FROM live_agent_plan_context_words WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&current, &raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return contextword.Catalog{}, err
	}
	if input.Revision != current {
		return contextword.Catalog{}, ErrContextWordsConflict
	}
	known := map[string]bool{}
	var previous []contextword.Entry
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, &previous); err != nil {
			return contextword.Catalog{}, err
		}
		for _, e := range previous {
			known[e.ID] = true
		}
	}
	for i := range items {
		if items[i].ID == "" {
			var b [16]byte
			if _, err = rand.Read(b[:]); err != nil {
				return contextword.Catalog{}, err
			}
			items[i].ID = hex.EncodeToString(b[:])
		} else if !known[items[i].ID] {
			return contextword.Catalog{}, ErrContextWordsConflict
		}
	}
	raw, err = json.Marshal(items)
	if err != nil {
		return contextword.Catalog{}, err
	}
	next := current + 1
	_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_context_words(tenant_id,plan_id,revision,items_json,updated_by_user_id) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE revision=VALUES(revision),items_json=VALUES(items_json),updated_by_user_id=VALUES(updated_by_user_id)`, tenant, plan, next, string(raw), actor)
	if err != nil {
		return contextword.Catalog{}, err
	}
	if err = tx.Commit(); err != nil {
		return contextword.Catalog{}, err
	}
	return contextword.Catalog{Revision: next, Items: items}, nil
}
