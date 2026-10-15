package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrInvalidPlanBizDomain = errors.New("invalid plan business mode")

func (s *Store) migratePlanBizDomain(ctx context.Context) error {
	exists, err := s.columnExists(ctx, "live_agent_plans", "biz_domain")
	if err != nil {
		return fmt.Errorf("check plan business mode: %w", err)
	}
	if exists {
		return nil
	}
	_, err = s.db.ExecContext(ctx, "ALTER TABLE live_agent_plans ADD COLUMN biz_domain VARCHAR(32) NOT NULL DEFAULT 'ecommerce'")
	return err
}

func ValidPlanBizDomain(domain string) bool {
	return domain == "ecommerce" || domain == "local_service"
}

func (s *Store) GetPlanBizDomain(ctx context.Context, tenant, plan int64) (string, error) {
	var domain string
	err := s.db.QueryRowContext(ctx, `SELECT biz_domain FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active'`, tenant, plan).Scan(&domain)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLiveAgentPlanNotFound
	}
	if err != nil {
		return "", err
	}
	if !ValidPlanBizDomain(domain) {
		domain = "ecommerce"
	}
	return domain, nil
}

func (s *Store) SetPlanBizDomain(ctx context.Context, tenant, plan int64, domain string) (string, error) {
	if !ValidPlanBizDomain(domain) {
		return "", ErrInvalidPlanBizDomain
	}
	_, err := s.db.ExecContext(ctx, `UPDATE live_agent_plans SET biz_domain=? WHERE tenant_id=? AND id=? AND status='active'`, domain, tenant, plan)
	if err != nil {
		return "", err
	}
	return s.GetPlanBizDomain(ctx, tenant, plan)
}
