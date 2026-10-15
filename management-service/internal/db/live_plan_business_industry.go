package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
)

var ErrInvalidPlanBusinessIndustry = errors.New("invalid plan business industry")

func (s *Store) migratePlanBusinessIndustries(ctx context.Context) error {
	for _, column := range []string{"ecommerce_industry", "local_service_industry"} {
		exists, err := s.columnExists(ctx, "live_agent_plans", column)
		if err != nil {
			return err
		}
		if !exists {
			if _, err = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE live_agent_plans ADD COLUMN %s VARCHAR(32) NOT NULL DEFAULT 'general'", column)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) GetPlanBusinessSettings(ctx context.Context, tenant, plan int64) (model.PlanBusinessSettings, error) {
	var domain, ecommerce, local string
	err := s.db.QueryRowContext(ctx, `SELECT biz_domain,ecommerce_industry,local_service_industry FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active'`, tenant, plan).Scan(&domain, &ecommerce, &local)
	if errors.Is(err, sql.ErrNoRows) {
		return model.PlanBusinessSettings{}, ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return model.PlanBusinessSettings{}, err
	}
	if !ValidPlanBizDomain(domain) {
		domain = "ecommerce"
	}
	if !model.ValidPlanBusinessIndustry("ecommerce", ecommerce) {
		ecommerce = "general"
	}
	if !model.ValidPlanBusinessIndustry("local_service", local) {
		local = "general"
	}
	return model.PlanBusinessSettings{BizDomain: domain, Industries: map[string]string{"ecommerce": ecommerce, "local_service": local}, IndustryOptions: map[string][]model.PlanBusinessIndustry{"ecommerce": model.PlanBusinessIndustryOptions("ecommerce"), "local_service": model.PlanBusinessIndustryOptions("local_service")}}, nil
}

func (s *Store) SetPlanBusinessIndustry(ctx context.Context, tenant, plan int64, domain, industry string) (model.PlanBusinessSettings, error) {
	if !model.ValidPlanBusinessIndustry(domain, industry) {
		return model.PlanBusinessSettings{}, ErrInvalidPlanBusinessIndustry
	}
	column := "ecommerce_industry"
	if domain == "local_service" {
		column = "local_service_industry"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_agent_plans SET `+column+`=? WHERE tenant_id=? AND id=? AND status='active'`, industry, tenant, plan)
	if err != nil {
		return model.PlanBusinessSettings{}, err
	}
	return s.GetPlanBusinessSettings(ctx, tenant, plan)
}
