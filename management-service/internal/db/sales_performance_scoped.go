package db

import (
	"context"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func salesPerformanceWhere(
	scope model.StaffBusinessScope,
	search string,
) (string, []any) {
	scopeSQL, scopeArgs := staffScopeSQL(scope, "s.user_id")
	where := []string{"s.status='active'", "u.status='active'", scopeSQL}
	args := append([]any{}, scopeArgs...)
	search = strings.TrimSpace(search)
	if search != "" {
		like := "%" + search + "%"
		where = append(where, "(u.display_name LIKE ? OR s.employee_code LIKE ? OR COALESCE(t.name,'') LIKE ?)")
		args = append(args, like, like, like)
	}
	return strings.Join(where, " AND "), args
}

func salesPerformanceOrder(sortMode string) string {
	switch sortMode {
	case "earning-desc":
		return "COALESCE(e.earning_amount_cents,0) DESC, s.id ASC"
	case "orders-desc":
		return "COALESCE(o.paid_order_count,0) DESC, s.id ASC"
	case "name-asc":
		return "u.display_name ASC, s.id ASC"
	default:
		return "(COALESCE(o.paid_amount_cents,0)-COALESCE(o.refunded_amount_cents,0)) DESC, s.id ASC"
	}
}

func (s *Store) ListSalesPerformanceScoped(
	ctx context.Context,
	scope model.StaffBusinessScope,
	periodStart time.Time,
	periodEnd time.Time,
	search string,
	sortMode string,
	page int,
	pageSize int,
) ([]model.SalesPerformanceSummary, int64, model.SalesPerformanceTotals, error) {
	page, pageSize = normalizeBusinessPage(page, pageSize, 100)
	whereSQL, scopeArgs := salesPerformanceWhere(scope, search)

	var total int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		LEFT JOIN crm_sales_teams t ON t.id=s.team_id
		WHERE `+whereSQL,
		scopeArgs...,
	).Scan(&total); err != nil {
		return nil, 0, model.SalesPerformanceTotals{}, err
	}

	queryArgs := []any{
		periodStart, periodEnd,
		periodStart, periodEnd,
	}
	queryArgs = append(queryArgs, scopeArgs...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)

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
		WHERE `+whereSQL+`
		ORDER BY `+salesPerformanceOrder(sortMode)+`
		LIMIT ? OFFSET ?
	`, queryArgs...)
	if err != nil {
		return nil, 0, model.SalesPerformanceTotals{}, err
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
			return nil, 0, model.SalesPerformanceTotals{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, model.SalesPerformanceTotals{}, err
	}

	totalsArgs := []any{
		periodStart, periodEnd,
		periodStart, periodEnd,
	}
	totalsArgs = append(totalsArgs, scopeArgs...)

	var totals model.SalesPerformanceTotals
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(COALESCE(o.paid_order_count,0)),0),
			COALESCE(SUM(COALESCE(o.customer_count,0)),0),
			COALESCE(SUM(COALESCE(o.paid_amount_cents,0)),0),
			COALESCE(SUM(COALESCE(o.refunded_amount_cents,0)),0),
			COALESCE(SUM(COALESCE(o.paid_amount_cents,0)-COALESCE(o.refunded_amount_cents,0)),0),
			COALESCE(SUM(COALESCE(e.earning_amount_cents,0)),0),
			COALESCE(SUM(COALESCE(e.pending_earning_cents,0)),0),
			COALESCE(SUM(COALESCE(e.settled_earning_cents,0)),0)
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
		WHERE `+whereSQL,
		totalsArgs...,
	).Scan(
		&totals.PaidOrderCount,
		&totals.CustomerCount,
		&totals.PaidAmountCents,
		&totals.RefundedAmountCents,
		&totals.NetRevenueCents,
		&totals.EarningAmountCents,
		&totals.PendingEarningCents,
		&totals.SettledEarningCents,
	); err != nil {
		return nil, 0, model.SalesPerformanceTotals{}, err
	}

	return items, total, totals, nil
}
