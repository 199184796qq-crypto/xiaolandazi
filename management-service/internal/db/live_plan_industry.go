package db

import (
	"context"
	"database/sql"
	"errors"
)

var ErrPlanIndustryNotFound = errors.New("plan industry not found")

// PlanIndustry 是方案的业务域 + 行业选择。
type PlanIndustry struct {
	BizDomain    string `json:"biz_domain"`
	IndustryCode string `json:"industry_code"`
	Source       string `json:"source"`
}

// GetPlanIndustry 返回方案已选业务域与行业 code；未设置时返回零值 + ErrPlanIndustryNotFound。
func (s *Store) GetPlanIndustry(ctx context.Context, tenant, plan int64) (PlanIndustry, error) {
	var p PlanIndustry
	err := s.db.QueryRowContext(ctx, `SELECT biz_domain, industry_code, source FROM live_agent_plan_industry WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&p.BizDomain, &p.IndustryCode, &p.Source)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanIndustry{}, ErrPlanIndustryNotFound
	}
	return p, err
}

// SetPlanIndustry 保存方案业务域 + 行业（upsert）。source 只能是 "manual"。
func (s *Store) SetPlanIndustry(ctx context.Context, tenant, plan int64, bizDomain, industryCode, source string, actor int64) error {
	if !ValidIndustryBizDomain(bizDomain) {
		return errors.New("业务域必须是 ecommerce 或 local_service")
	}
	if source != "manual" {
		return errors.New("行业来源必须是 manual")
	}
	var actorVal any
	if actor > 0 {
		actorVal = actor
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_agent_plan_industry(tenant_id, plan_id, biz_domain, industry_code, source, updated_by_user_id)
		VALUES(?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE biz_domain=VALUES(biz_domain), industry_code=VALUES(industry_code), source=VALUES(source), updated_by_user_id=VALUES(updated_by_user_id)
	`, tenant, plan, bizDomain, industryCode, source, actorVal)
	return err
}
