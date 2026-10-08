package db

import "context"

type RiskReviewPlan struct {
	TenantID, PlanID int64
	Name             string
}

func (s *Store) LiveRiskReviewPlans(ctx context.Context) ([]RiskReviewPlan, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT r.tenant_id,r.plan_id,p.name FROM live_fact_risk_reviews r INNER JOIN live_agent_plans p ON p.tenant_id=r.tenant_id AND p.id=r.plan_id ORDER BY r.tenant_id,r.plan_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RiskReviewPlan{}
	for rows.Next() {
		var p RiskReviewPlan
		if err = rows.Scan(&p.TenantID, &p.PlanID, &p.Name); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

func (s *Store) LiveRiskReviewCounts(ctx context.Context) (rows, pending, backups int64, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(status<>'confirmed'),0) FROM live_fact_risk_reviews WHERE active=1`).Scan(&rows, &pending)
	if err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_fact_risk_reviews_dedup_backup`).Scan(&backups)
	return
}
