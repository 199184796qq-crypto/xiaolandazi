package db

import "context"

// PlanBusinessProfile 是生成提示词时需要的行业信息。它留在 live_agent_plans 上，
// 不在 LiveAgentPlan 结构里，因此单独读取。
type PlanBusinessProfile struct {
	BizDomain            string
	EcommerceIndustry    string
	LocalServiceIndustry string
}

func (s *Store) GetPlanBusinessProfile(ctx context.Context, tenantID, planID int64) (PlanBusinessProfile, error) {
	var p PlanBusinessProfile
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(biz_domain,''), COALESCE(ecommerce_industry,''), COALESCE(local_service_industry,'')
		 FROM live_agent_plans WHERE id=? AND tenant_id=?`, planID, tenantID).
		Scan(&p.BizDomain, &p.EcommerceIndustry, &p.LocalServiceIndustry)
	return p, err
}
// GetSystemSetting 读取单个平台设置；不存在时返回空串。
func (s *Store) GetSystemSetting(ctx context.Context, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value_text FROM mgmt_system_settings WHERE setting_key=?`, key).Scan(&v)
	if err != nil {
		return "", nil
	}
	return v, nil
}
