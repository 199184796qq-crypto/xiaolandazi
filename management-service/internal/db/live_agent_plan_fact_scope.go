package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
)

var ErrLiveFactScopeInvalid = errors.New("事实归属无效")
var ErrLiveFactScopeProductNotFound = errors.New("归属商品不存在、已停用或不属于当前方案")

func (s *Store) UpdateLiveAgentPlanFactScope(ctx context.Context, tenant, plan, fact, actor, expected int64, scope model.LiveAgentPlanFactScope) (model.LiveAgentPlanFact, error) {
	if expected <= 0 || (scope.Kind != "common" && scope.Kind != "product") || (scope.Kind == "common" && scope.ProductLinkID != 0) || (scope.Kind == "product" && scope.ProductLinkID <= 0) {
		return model.LiveAgentPlanFact{}, ErrLiveFactScopeInvalid
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
	if scope.Kind == "product" {
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan, scope.ProductLinkID).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveAgentPlanFact{}, ErrLiveFactScopeProductNotFound
		}
		if err != nil {
			return model.LiveAgentPlanFact{}, err
		}
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
	if current.Scope != nil && *current.Scope == scope {
		return current, nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET scope_json=?,version_no=version_no+1,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, encodeJSON(scope), actor, fact, tenant, plan)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_revisions(tenant_id,plan_id,fact_id,version_no,action,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id,scope_json)
 SELECT tenant_id,plan_id,id,version_no,'scope',category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,?,scope_json FROM live_agent_plan_facts WHERE id=? AND tenant_id=? AND plan_id=?`, actor, fact, tenant, plan)
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
