package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/contentpools"
)

// User adoption alone commits the rule. It does not rewrite a product fact,
// subject root, secondary material, saved set or running/published snapshot.
func (s *Store) AdoptSpeechExpressionConstraint(ctx context.Context, tenant, plan, actor, fact, version, revision int64, c contentpools.Correction) error {
	if c.Kind != contentpools.ExpressionConstraintKind || c.ID == "" || len(c.Products) == 0 || c.Feedback == "" || c.Content.Forbidden == "" || c.Content.Question != "" || c.Content.Fact != "" || c.Content.Guidance != "" || c.Content.Cancellation != "" {
		return errors.New("仅可采用已复核的表达约束")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var current int64
	if e = tx.QueryRowContext(ctx, `SELECT version_no FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, fact).Scan(&current); e != nil {
		return e
	}
	if current != version {
		return ErrLiveAgentPlanFactVersionConflict
	}
	var rev int64
	if e = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&rev); e != nil {
		return e
	}
	var saved string
	e = tx.QueryRowContext(ctx, `SELECT CAST(data_json AS CHAR) FROM live_plan_content_corrections WHERE tenant_id=? AND plan_id=? AND id=?`, tenant, plan, c.ID).Scan(&saved)
	raw, _ := json.Marshal(c)
	if e == nil {
		var old contentpools.Correction
		if json.Unmarshal([]byte(saved), &old) != nil || contentpools.Digest(old) != contentpools.Digest(c) {
			return ErrPoolSourceChanged
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if rev != revision {
		return ErrPoolSourceChanged
	}
	var active int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active'`, tenant, plan).Scan(&active); e != nil {
		return e
	}
	if active != 1 {
		return ErrLiveAgentPlanNotFound
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO live_plan_content_corrections(id,tenant_id,plan_id,data_json,created_by) VALUES(?,?,?,?,?)`, c.ID, tenant, plan, raw, actor); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE live_plan_content_revisions SET revision=revision+1 WHERE tenant_id=? AND plan_id=?`, tenant, plan); e != nil {
		return e
	}
	return tx.Commit()
}

// Read only the adopted expression overlay, never mutable product facts.
func (s *Store) CurrentSpeechExpressionConstraints(ctx context.Context, tenant, plan int64) (contentpools.Sources, error) {
	corrections, e := s.ListContentCorrections(ctx, tenant, plan)
	if e != nil {
		return contentpools.Sources{}, e
	}
	source := contentpools.ExpressionConstraintSource(contentpools.Sources{Corrections: corrections})
	if len(source.Corrections) == 0 {
		return source, nil
	}
	links, e := s.ListLiveAgentPlanProductLinks(ctx, tenant, plan)
	if e != nil {
		return source, e
	}
	for _, p := range links {
		if p.Status == "active" {
			source.Products = append(source.Products, contentpools.Product{ID: p.ID, Link: p.LinkKey, Name: p.ProductName})
		}
	}
	return source, nil
}
