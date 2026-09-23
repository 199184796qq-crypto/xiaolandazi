package db

import (
	"context"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) ListSalesPerformance(
	ctx context.Context,
	periodStart time.Time,
	periodEnd time.Time,
) ([]model.SalesPerformanceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			s.user_id,
			s.employee_code,
			COALESCE(u.display_name, u.username, ''),
			COALESCE(t.name, ''),
			COALESCE(o.paid_order_count, 0),
			COALESCE(o.customer_count, 0),
			COALESCE(o.paid_amount_cents, 0),
			COALESCE(o.refunded_amount_cents, 0),
			COALESCE(o.paid_amount_cents, 0) - COALESCE(o.refunded_amount_cents, 0),
			COALESCE(e.earning_amount_cents, 0),
			COALESCE(e.pending_earning_cents, 0),
			COALESCE(e.settled_earning_cents, 0)
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		LEFT JOIN crm_sales_teams t ON t.id=s.team_id
		LEFT JOIN (
			SELECT
				sales_staff_id_snapshot AS sales_staff_id,
				COUNT(*) AS paid_order_count,
				COUNT(DISTINCT tenant_id) AS customer_count,
				SUM(paid_amount_cents) AS paid_amount_cents,
				SUM(refunded_amount_cents) AS refunded_amount_cents
			FROM biz_orders
			WHERE sales_staff_id_snapshot IS NOT NULL
			  AND paid_at IS NOT NULL
			  AND paid_at >= ?
			  AND paid_at < ?
			GROUP BY sales_staff_id_snapshot
		) o ON o.sales_staff_id=s.id
		LEFT JOIN (
			SELECT
				beneficiary_id AS sales_staff_id,
				SUM(amount_cents) AS earning_amount_cents,
				SUM(CASE WHEN status IN ('pending','available') THEN amount_cents ELSE 0 END) AS pending_earning_cents,
				SUM(CASE WHEN status IN ('settled','paid') THEN amount_cents ELSE 0 END) AS settled_earning_cents
			FROM inc_earnings
			WHERE beneficiary_type='sales_staff'
			  AND created_at >= ?
			  AND created_at < ?
			GROUP BY beneficiary_id
		) e ON e.sales_staff_id=s.id
		WHERE s.status='active'
		ORDER BY (COALESCE(o.paid_amount_cents,0)-COALESCE(o.refunded_amount_cents,0)) DESC,
		         s.id ASC
	`, periodStart, periodEnd, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SalesPerformanceSummary, 0)
	for rows.Next() {
		var item model.SalesPerformanceSummary
		if err := rows.Scan(
			&item.SalesStaffID,
			&item.UserID,
			&item.EmployeeCode,
			&item.DisplayName,
			&item.TeamName,
			&item.PaidOrderCount,
			&item.CustomerCount,
			&item.PaidAmountCents,
			&item.RefundedAmountCents,
			&item.NetRevenueCents,
			&item.EarningAmountCents,
			&item.PendingEarningCents,
			&item.SettledEarningCents,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
