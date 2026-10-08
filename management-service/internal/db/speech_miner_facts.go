package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

var ErrSpeechMinerFactConflict = errors.New("挖掘事实与已有内容冲突")

// Save the user's confirmed facts as plan-editor data only. No room binding,
// publication, Core call, or hot-reload belongs in this store operation.
// The batch is atomic; retrying identical confirmed facts does not duplicate rows.
func (s *Store) SaveSpeechMinerFacts(ctx context.Context, tenant, plan, actor int64, facts []model.LiveAgentPlanFactCandidate, sourceRef string) (added int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, plan, tenant).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return 0, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return 0, err
	}
	for _, fact := range facts {
		var id int64
		var value, status string
		err = tx.QueryRowContext(ctx, `SELECT id,fact_value,status FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? AND category=? AND fact_key=? FOR UPDATE`, tenant, plan, fact.Category, fact.Key).Scan(&id, &value, &status)
		if err == nil {
			if status == "active" && strings.TrimSpace(value) == fact.Value {
				continue
			}
			return 0, fmt.Errorf("%w：%s；不会覆盖已有事实", ErrSpeechMinerFactConflict, fact.Key)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_facts (tenant_id,plan_id,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,status,version_no,created_by_user_id,updated_by_user_id) VALUES (?,?,?,?,?,'','',?,'adoptable','user_confirmed_unpublished','speech_miner',?,'active',1,?,?)`, tenant, plan, fact.Category, fact.Key, fact.Value, fact.SourceQuote, sourceRef, actor, actor)
		if e != nil {
			return 0, e
		}
		id, e = result.LastInsertId()
		if e != nil {
			return 0, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_revisions (tenant_id,plan_id,fact_id,version_no,action,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id) VALUES (?,?,?,1,'adopt',?,?,?,'','',?,'adoptable','user_confirmed_unpublished','speech_miner',?,?)`, tenant, plan, id, fact.Category, fact.Key, fact.Value, fact.SourceQuote, sourceRef, actor)
		if e != nil {
			return 0, e
		}
		added++
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}
