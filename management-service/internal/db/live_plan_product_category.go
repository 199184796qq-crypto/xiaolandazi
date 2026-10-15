package db

import (
	"context"
	"database/sql"
	"errors"
)

var ErrPlanProductCategoryNotFound = errors.New("plan product category not found")

// GetPlanProductCategory 返回方案已判定的商品品类 code；从未判定返回空串 + ErrPlanProductCategoryNotFound。
// category 存英文 code（如 "food"），空串表示未判定，调用方回退到母版自判。
func (s *Store) GetPlanProductCategory(ctx context.Context, tenant, plan int64) (string, error) {
	var category string
	err := s.db.QueryRowContext(ctx, `SELECT category FROM live_agent_plan_product_category WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&category)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrPlanProductCategoryNotFound
	}
	return category, err
}

// SetPlanProductCategory 保存方案商品品类（upsert）。source 只能是 "auto" 或 "manual"。
func (s *Store) SetPlanProductCategory(ctx context.Context, tenant, plan int64, category, source string, actor int64) error {
	if source != "auto" && source != "manual" {
		return errors.New("品类来源必须是 auto 或 manual")
	}
	var actorVal any
	if actor > 0 {
		actorVal = actor
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_category(tenant_id, plan_id, category, source, updated_by_user_id)
		VALUES(?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE category=VALUES(category), source=VALUES(source), updated_by_user_id=VALUES(updated_by_user_id)
	`, tenant, plan, category, source, actorVal)
	return err
}
