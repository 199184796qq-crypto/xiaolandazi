package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"time"
)

// Read-only hints; the order transaction always rechecks under locks.
func (s *Store) AnnotateCampaignEligibility(ctx context.Context, tenant int64, items []model.MarketingCampaign) error {
	var created time.Time
	var phone string
	if err := s.db.QueryRowContext(ctx, `SELECT t.created_at,COALESCE((SELECT phone FROM mgmt_users WHERE tenant_id=t.id AND role='customer' AND status='active' ORDER BY id LIMIT 1),'') FROM mgmt_tenants t WHERE id=?`, tenant).Scan(&created, &phone); err != nil {
		return err
	}
	if phone != "" {
		var earliest time.Time
		if err := s.db.QueryRowContext(ctx, `SELECT MIN(created_at) FROM mgmt_users WHERE phone=? AND role='customer'`, phone).Scan(&earliest); err != nil {
			return err
		}
		if earliest.Before(created) {
			created = earliest
		}
	}
	for i := range items {
		c := &items[i]
		eligible := campaignAudienceEligible(*c, created, time.Now().UTC())
		reason := "仅符合新开户条件的账户可以领取"
		if c.Controls.RequirePhone && strings.TrimSpace(phone) == "" {
			eligible = false
			reason = "请先绑定手机号"
		}
		key := c.Controls.BenefitKey
		if key == "" {
			key = fmt.Sprintf("campaign-%d", c.ID)
		}
		subjects := []string{fmt.Sprintf("tenant:%d", tenant)}
		if phone != "" {
			subjects = append(subjects, "phone:"+strings.TrimSpace(phone))
		}
		for _, v := range subjects {
			if c.Controls.MaxClaims == 0 && c.Controls.MaxUnits == 0 {
				continue
			}
			hash := fmt.Sprintf("%x", sha256.Sum256([]byte(v)))
			var count, units uint64
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(quantity),0) FROM mkt_claim_reservations WHERE benefit_key=? AND subject_hash=? AND status IN ('pending','consumed')`, key, hash).Scan(&count, &units); err != nil {
				return err
			}
			if (c.Controls.MaxClaims > 0 && count >= uint64(c.Controls.MaxClaims)) || (c.Controls.MaxUnits > 0 && units >= uint64(c.Controls.MaxUnits)) {
				eligible = false
				reason = "已领取或已有待支付活动订单；取消未支付订单后可重试"
			}
		}
		c.Eligible = &eligible
		if !eligible {
			c.IneligibleReason = reason
		}
	}
	return nil
}
