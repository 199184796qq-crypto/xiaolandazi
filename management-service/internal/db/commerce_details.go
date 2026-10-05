package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
	"time"
)

func (s *Store) CommerceSalesStaffID(ctx context.Context, user int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM crm_sales_staff WHERE user_id=? AND status='active'`, user).Scan(&id)
	return id, err
}
func (s *Store) ListCommerceEarningDetails(ctx context.Context, kind string, id int64, period string) ([]model.CommerceEarningDetail, error) {
	query := `SELECT e.id,COALESCE(o.order_no,''),COALESCE((SELECT product_name_snapshot FROM biz_order_items WHERE order_id=e.source_order_id ORDER BY id LIMIT 1),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.calculation_snapshot_json,'$.commerce_rule.name')),'退款冲回'),COALESCE(JSON_EXTRACT(e.calculation_snapshot_json,'$.commerce_rule.id'),0),e.amount_cents,e.status,e.available_at,e.created_at FROM inc_earnings e LEFT JOIN biz_orders o ON o.id=e.source_order_id WHERE e.beneficiary_type=? AND e.beneficiary_id=? AND LEFT(e.earning_type,9)='commerce_'`
	args := []any{kind, id}
	if period != "" {
		start, err := time.ParseInLocation("2006-01", period, commerceLocation)
		if err != nil {
			return nil, errors.New("账期格式应为YYYY-MM")
		}
		query += " AND e.created_at>=? AND e.created_at<?"
		args = append(args, start.UTC(), start.AddDate(0, 1, 0).UTC())
	}
	query += " ORDER BY e.id DESC LIMIT 500"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.CommerceEarningDetail{}
	for rows.Next() {
		var x model.CommerceEarningDetail
		if err = rows.Scan(&x.ID, &x.OrderNo, &x.ProductName, &x.RuleName, &x.RuleVersionID, &x.AmountCents, &x.Status, &x.AvailableAt, &x.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, x)
	}
	return items, rows.Err()
}
func validateCommerceTargetTx(ctx context.Context, tx *sql.Tx, r model.CommerceRewardRule) error {
	table := ""
	if r.ScopeType == "campaign" {
		table = "mkt_campaigns"
	} else if r.ScopeType == "product" {
		table = map[string]string{"membership": "catalog_membership_plans", "time_card": "catalog_time_card_products", "device": "catalog_device_products"}[r.ProductType]
	}
	if table == "" {
		return nil
	}
	var id int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE id=?", r.TargetID).Scan(&id); err != nil {
		return errors.New("所选商品或活动不存在")
	}
	return nil
}
