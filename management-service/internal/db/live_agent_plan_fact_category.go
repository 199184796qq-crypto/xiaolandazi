package db

import (
	"context"
	"database/sql"
	"errors"

	"livecompanion/management/internal/model"
)

// ErrLiveFactCategoryInvalid 表示事实分类无效（空或不在当前方案允许的分类枚举内）。
var ErrLiveFactCategoryInvalid = errors.New("事实分类无效")

// UpdateLiveAgentPlanFactCategory 轻量更新单条正式事实的分类，镜像 scope 更新：
// 只改 category 字段并记 revision（action='category'），不触发事实归一化/四层整理。
// 用 expected version 乐观锁防并发覆盖；分类校验由调用方（httpapi 层）完成。
func (s *Store) UpdateLiveAgentPlanFactCategory(ctx context.Context, tenant, plan, fact, actor, expected int64, category string) (model.LiveAgentPlanFact, error) {
	if category == "" {
		return model.LiveAgentPlanFact{}, ErrLiveFactCategoryInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	current, err := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, fact, tenant, plan))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanFactNotFound
	}
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	if current.VersionNo != expected {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanFactVersionConflict
	}
	if current.Category == category {
		return current, nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET category=?,version_no=version_no+1,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, category, actor, fact, tenant, plan)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_revisions(tenant_id,plan_id,fact_id,version_no,action,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id,scope_json)
 SELECT tenant_id,plan_id,id,version_no,'category',category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,?,scope_json FROM live_agent_plan_facts WHERE id=? AND tenant_id=? AND plan_id=?`, actor, fact, tenant, plan)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	updated, err := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, fact, tenant, plan))
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	return updated, nil
}
