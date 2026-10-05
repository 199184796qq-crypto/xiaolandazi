package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
	"time"
)

var ErrMarketingEligibility = errors.New("不符合活动领取资格，或已经达到领取上限")

func ValidateMarketingControls(c model.MarketingCampaignControls, starts string) error {
	_, err := normalizeCampaignControls(c, starts)
	return err
}

func normalizeCampaignControls(c model.MarketingCampaignControls, starts string) (model.MarketingCampaignControls, error) {
	if c.Audience == "" {
		c.Audience = "all"
	}
	c.BenefitKey = strings.TrimSpace(c.BenefitKey)
	switch c.Audience {
	case "all":
	case "new_since_start":
		if starts == "" {
			return c, errors.New("新开户活动必须设置开始时间")
		}
	case "new_within_days":
		if c.NewAccountDays < 1 || c.NewAccountDays > 365 {
			return c, errors.New("新开户天数须为1-365")
		}
	default:
		return c, errors.New("活动人群不支持")
	}
	if c.Audience != "all" {
		c.MaxClaims = 1
		c.RequirePhone = true
		if c.BenefitKey == "" {
			c.BenefitKey = "new-account-welcome"
		}
	}
	if c.BenefitKey != "" && !validMarketingPlacementCode(c.BenefitKey) {
		return c, errors.New("权益标识须为不超过64位的小写字母、数字、短横线或下划线")
	}
	if c.MaxClaims > 100000 || c.MaxUnits > 100000 {
		return c, errors.New("活动限额过大")
	}
	return c, nil
}

func saveCampaignControlsTx(ctx context.Context, tx *sql.Tx, id int64, c model.MarketingCampaignControls) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO mkt_campaign_controls(campaign_id,controls_json) VALUES (?,CAST(? AS JSON)) ON DUPLICATE KEY UPDATE controls_json=VALUES(controls_json)`, id, string(b))
	return e
}

func (s *Store) loadCampaignControls(ctx context.Context, items []model.MarketingCampaign) error {
	for i := range items {
		var raw string
		e := s.db.QueryRowContext(ctx, `SELECT CAST(controls_json AS CHAR) FROM mkt_campaign_controls WHERE campaign_id=?`, items[i].ID).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			if e = json.Unmarshal([]byte(raw), &items[i].Controls); e != nil {
				return e
			}
		}
		for j := range items[i].Items {
			var price sql.NullInt64
			e = s.db.QueryRowContext(ctx, `SELECT fixed_price_cents FROM mkt_campaign_item_options WHERE campaign_item_id=?`, items[i].Items[j].ID).Scan(&price)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			if price.Valid {
				p := uint64(price.Int64)
				items[i].Items[j].FixedPriceCents = &p
			}
		}
	}
	return nil
}

func marketingItemPayable(base uint64, item model.MarketingCampaignItem) uint64 {
	if item.PricingMode == "fixed" && item.FixedPriceCents != nil {
		return *item.FixedPriceCents
	} // total price of one configured bundle
	if item.PricingMode == "free" {
		return 0
	}
	return calculateMarketingPayableCents(base, item.DiscountBPS)
}

func campaignAudienceEligible(c model.MarketingCampaign, created, now time.Time) bool {
	if !marketingCampaignWindowActive(c, now) {
		return false
	}
	switch c.Controls.Audience {
	case "new_since_start":
		start, e := time.Parse(time.RFC3339, c.StartsAt)
		return e == nil && !created.Before(start) && !created.After(now)
	case "new_within_days":
		return c.Controls.NewAccountDays > 0 && !created.After(now) && now.Before(created.AddDate(0, 0, int(c.Controls.NewAccountDays)))
	default:
		return true
	}
}

func reserveCampaignClaimTx(ctx context.Context, tx *sql.Tx, tenantID, orderID int64, c model.MarketingCampaign, item model.MarketingCampaignItem) error {
	// Same lock as campaign edits: stale or disabled prices cannot be purchased.
	var status string
	var start, end sql.NullTime
	if e := tx.QueryRowContext(ctx, `SELECT status,starts_at,ends_at FROM mkt_campaigns WHERE id=? FOR UPDATE`, c.ID).Scan(&status, &start, &end); e != nil {
		return e
	}
	current := c
	current.Status = status
	current.StartsAt = campaignTimeString(start)
	current.EndsAt = campaignTimeString(end)
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT CAST(controls_json AS CHAR) FROM mkt_campaign_controls WHERE campaign_id=?`, c.ID).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var controls model.MarketingCampaignControls
	if e == nil {
		if e = json.Unmarshal([]byte(raw), &controls); e != nil {
			return e
		}
	}
	if controls != c.Controls {
		return ErrMarketingEligibility
	}
	current.Controls = controls
	var mode string
	var price sql.NullInt64
	var qty, months, bps uint32
	if e = tx.QueryRowContext(ctx, `SELECT r.pricing_mode,r.package_months,r.discount_bps,i.quantity,o.fixed_price_cents FROM mkt_campaign_items i JOIN mkt_campaign_price_rules r ON r.campaign_item_id=i.id LEFT JOIN mkt_campaign_item_options o ON o.campaign_item_id=i.id WHERE i.id=? AND i.campaign_id=?`, item.ID, c.ID).Scan(&mode, &months, &bps, &qty, &price); e != nil {
		return e
	}
	if mode != item.PricingMode || qty != item.Quantity || months != item.PackageMonths || bps != item.DiscountBPS || (item.FixedPriceCents == nil) != (!price.Valid) || (price.Valid && uint64(price.Int64) != *item.FixedPriceCents) {
		return ErrMarketingEligibility
	}
	var created time.Time
	var phone string
	if e = tx.QueryRowContext(ctx, `SELECT t.created_at,COALESCE((SELECT u.phone FROM mgmt_users u WHERE u.tenant_id=t.id AND u.role='customer' AND u.status='active' ORDER BY u.id LIMIT 1),'') FROM mgmt_tenants t WHERE t.id=?`, tenantID).Scan(&created, &phone); e != nil {
		return e
	}
	if controls.Audience != "all" && phone != "" {
		var earliest time.Time
		if e = tx.QueryRowContext(ctx, `SELECT MIN(created_at) FROM mgmt_users WHERE phone=? AND role='customer'`, phone).Scan(&earliest); e != nil {
			return e
		}
		if earliest.Before(created) {
			created = earliest
		}
	}
	if !campaignAudienceEligible(current, created, time.Now().UTC()) {
		return ErrMarketingEligibility
	}
	if controls.RequirePhone && strings.TrimSpace(phone) == "" {
		return ErrMarketingEligibility
	}
	if controls.MaxClaims == 0 && controls.MaxUnits == 0 {
		return nil
	}
	key := controls.BenefitKey
	if key == "" {
		key = fmt.Sprintf("campaign-%d", c.ID)
	}
	subjects := []string{fmt.Sprintf("tenant:%d", tenantID)}
	if phone != "" {
		subjects = append(subjects, "phone:"+strings.TrimSpace(phone))
	}
	hashes := make([]string, 0, len(subjects))
	for _, v := range subjects {
		hashes = append(hashes, fmt.Sprintf("%x", sha256.Sum256([]byte(v))))
	}
	sort.Strings(hashes)
	for _, subject := range hashes {
		if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO mkt_claim_locks(benefit_key,subject_hash) VALUES (?,?)`, key, subject); e != nil {
			return e
		}
		var locked string
		if e = tx.QueryRowContext(ctx, `SELECT subject_hash FROM mkt_claim_locks WHERE benefit_key=? AND subject_hash=? FOR UPDATE`, key, subject).Scan(&locked); e != nil {
			return e
		}
		var count, units uint64
		// Current read avoids stale snapshots under repeatable-read.
		rows, err := tx.QueryContext(ctx, `SELECT quantity FROM mkt_claim_reservations WHERE benefit_key=? AND subject_hash=? AND status IN ('pending','consumed') FOR UPDATE`, key, subject)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n uint64
			if err = rows.Scan(&n); err != nil {
				rows.Close()
				return err
			}
			count++
			units += n
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if (controls.MaxClaims > 0 && count >= uint64(controls.MaxClaims)) || (controls.MaxUnits > 0 && units+uint64(item.Quantity) > uint64(controls.MaxUnits)) {
			return ErrMarketingEligibility
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO mkt_claim_reservations(order_id,benefit_key,subject_hash,quantity) VALUES (?,?,?,?)`, orderID, key, subject, item.Quantity); e != nil {
			return e
		}
	}
	return nil
}
