package db

import (
	"context"
	"database/sql"
	"strings"

	"livecompanion/management/internal/model"
)

func salesCustomerPageOrder(sort string) string {
	switch sort {
	case "created-asc":
		return "u.created_at ASC, u.id ASC"
	case "name-asc":
		return "u.display_name ASC, u.id ASC"
	case "name-desc":
		return "u.display_name DESC, u.id DESC"
	default:
		return "u.created_at DESC, u.id DESC"
	}
}

// Keep the legacy all-items endpoint compatible with existing callers; new list UIs
// use this bounded query. All counts and filters are inside the current assignment scope.
func (s *Store) ListSalesCustomersPageByUser(ctx context.Context, userID int64,
	search, status, source, sort string, page, size int, financeFilters ...string,
) ([]model.SalesCustomerListItem, int64, model.SalesCustomerListSummary, error) {
	items := make([]model.SalesCustomerListItem, 0)
	var summary model.SalesCustomerListSummary
	var staffID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT ss.id FROM crm_sales_staff ss
		JOIN mgmt_users seller ON seller.id=ss.user_id
		WHERE ss.user_id=? AND ss.status='active' AND seller.status='active'
		AND seller.role='sales_staff' LIMIT 1`, userID).Scan(&staffID)
	if err != nil {
		return nil, 0, summary, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 12
	}
	// Bound offset arithmetic even for crafted requests.
	if page > 1000000 {
		page = 1000000
	}

	from := ` FROM mgmt_users u
		JOIN mgmt_tenants customer_org ON customer_org.id=u.tenant_id
		JOIN mgmt_tenants parent ON parent.id=customer_org.parent_id AND parent.org_type='platform'
		LEFT JOIN crm_customer_profiles profile ON profile.tenant_id=u.tenant_id
		LEFT JOIN fin_customer_confirmations confirmation ON confirmation.tenant_id=u.tenant_id
		LEFT JOIN fin_customer_receipts latest_receipt ON latest_receipt.id=(SELECT MAX(r.id) FROM fin_customer_receipts r WHERE r.tenant_id=u.tenant_id) `
	owned := `u.role='customer' AND customer_org.org_type='customer' AND EXISTS (
		SELECT 1 FROM crm_customer_sales_assignments assigned
		WHERE assigned.tenant_id=u.tenant_id AND assigned.sales_staff_id=?
		AND assigned.status='active' AND assigned.effective_to IS NULL
	)`
	// EXISTS prevents duplicate assignment rows from inflating counts or pages.
	where := " WHERE " + owned
	args := []any{staffID}
	if search = strings.TrimSpace(search); search != "" {
		where += " AND (u.display_name LIKE ? OR u.username LIKE ? OR u.phone LIKE ? OR u.email LIKE ? OR u.province LIKE ? OR u.city LIKE ? OR u.district LIKE ? OR u.address LIKE ?)"
		like := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(search) + "%"
		// Explicit LIKE escape keeps literal %, _ and ! searches predictable.
		where = strings.ReplaceAll(where, "LIKE ?", "LIKE ? ESCAPE '!'")
		for i := 0; i < 8; i++ {
			args = append(args, like)
		}
	}
	if status != "" && status != "all" {
		where += " AND u.status=?"
		args = append(args, status)
	}
	if source != "" && source != "all" {
		where += " AND COALESCE(NULLIF(profile.source_type,''),'unknown')=?"
		args = append(args, source)
	}

	if len(financeFilters) > 0 && financeFilters[0] == "qualified" {
		where += " AND confirmation.tenant_id IS NOT NULL"
	}
	if len(financeFilters) > 0 && financeFilters[0] == "unconfirmed" {
		where += " AND confirmation.tenant_id IS NULL"
	}
	if len(financeFilters) > 1 && financeFilters[1] != "" && financeFilters[1] != "all" {
		where += " AND COALESCE(latest_receipt.status,'awaiting_payment')=?"
		args = append(args, financeFilters[1])
	}

	// A read-only snapshot keeps the summary/count/page consistent during a handover.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, summary, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*), COALESCE(SUM(u.status='active'),0), COALESCE(SUM(u.status='disabled'),0),COALESCE(SUM(confirmation.tenant_id IS NOT NULL),0),COALESCE(SUM(confirmation.tenant_id IS NULL),0),COALESCE(SUM(EXISTS(SELECT 1 FROM fin_customer_receipts r WHERE r.tenant_id=u.tenant_id AND r.status IN ('pending','posting_failed'))),0)"+from+" WHERE "+owned, staffID).
		Scan(&summary.TotalCount, &summary.ActiveCount, &summary.DisabledCount, &summary.QualifiedCount, &summary.UnconfirmedCount, &summary.PendingReceiptCount); err != nil {
		return nil, 0, summary, err
	}
	var total int64
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*)"+from+where, args...).Scan(&total); err != nil {
		return nil, 0, summary, err
	}
	queryArgs := append(append([]any{}, args...), size, int64(page-1)*int64(size))
	rows, err := tx.QueryContext(ctx, `SELECT u.id,u.tenant_id,u.username,u.display_name,
		COALESCE(u.phone,''),COALESCE(u.email,''),COALESCE(u.province,''),COALESCE(u.city,''),
		COALESCE(u.district,''),COALESCE(u.address,''),u.status,
		COALESCE(NULLIF(profile.source_type,''),'unknown'),u.created_at,confirmation.tenant_id IS NOT NULL,COALESCE(latest_receipt.status,'awaiting_payment'),confirmation.confirmed_at`+from+where+
		" ORDER BY "+salesCustomerPageOrder(sort)+" LIMIT ? OFFSET ?", queryArgs...)
	if err != nil {
		return nil, 0, summary, err
	}
	for rows.Next() {
		var item model.SalesCustomerListItem
		if err = rows.Scan(&item.UserID, &item.TenantID, &item.Username, &item.DisplayName,
			&item.Phone, &item.Email, &item.Province, &item.City, &item.District, &item.Address,
			&item.Status, &item.SourceType, &item.CreatedAt, &item.Qualified, &item.CollectionStatus, &item.ConfirmedAt); err != nil {
			rows.Close()
			return nil, 0, summary, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, summary, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, summary, err
	}
	return items, total, summary, nil
}
