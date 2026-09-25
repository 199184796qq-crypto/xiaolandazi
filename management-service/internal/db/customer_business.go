package db

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

//go:embed customer_business_schema.sql
var customerBusinessSchema string

func (s *Store) migrateCustomerBusiness(ctx context.Context) error {
	for _, raw := range strings.Split(strings.ReplaceAll(customerBusinessSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, raw); err != nil {
			return fmt.Errorf("customer business schema: %w", err)
		}
	}
	return nil
}

var ErrCustomerBusinessConflict = errors.New("业务状态已变化，请刷新后重试")

type customerBusinessDB interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Scope follows the current assignment; historical authors never grant future access.
func customerScopeSQL(sc model.CustomerBusinessScope, tenantExpr string) (string, []any) {
	if sc.Finance {
		return "1=1", nil
	}
	if sc.Role == "customer" && sc.TenantID > 0 {
		return tenantExpr + "=?", []any{sc.TenantID}
	}
	if sc.Role == "sales_staff" && sc.UserID > 0 {
		return `EXISTS(SELECT 1 FROM crm_customer_sales_assignments a JOIN crm_sales_staff ss ON ss.id=a.sales_staff_id JOIN mgmt_users seller ON seller.id=ss.user_id WHERE a.tenant_id=` + tenantExpr + ` AND ss.user_id=? AND ss.status='active' AND seller.status='active' AND seller.role='sales_staff' AND a.status='active' AND a.effective_to IS NULL)`, []any{sc.UserID}
	}
	return "1=0", nil
}

// Match the sales portfolio transfer's locking order before any customer write.
func lockCustomerBusinessWriter(ctx context.Context, tx *sql.Tx, sc model.CustomerBusinessScope) error {
	if sc.Role != "sales_staff" {
		return nil
	}
	var id int64
	return tx.QueryRowContext(ctx, `SELECT ss.id FROM crm_sales_staff ss JOIN mgmt_users u ON u.id=ss.user_id WHERE ss.user_id=? AND ss.status='active' AND u.status='active' AND u.role='sales_staff' FOR UPDATE`, sc.UserID).Scan(&id)
}
func checkCustomerBusinessAccess(ctx context.Context, q customerBusinessDB, sc model.CustomerBusinessScope, tenantID int64) error {
	where, args := customerScopeSQL(sc, "t.id")
	args = append([]any{tenantID}, args...)
	var id int64
	return q.QueryRowContext(ctx, "SELECT t.id FROM mgmt_tenants t WHERE t.id=? AND t.org_type='customer' AND ("+where+")", args...).Scan(&id)
}
func businessPage(page, size int) (int, int) {
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	if size < 1 || size > 100 {
		size = 12
	}
	return page, size
}
func businessLike(value string) string {
	return "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.TrimSpace(value)) + "%"
}
func (s *Store) CustomerRecognition(ctx context.Context, sc model.CustomerBusinessScope, tenantID int64) (model.CustomerRecognition, error) {
	out := model.CustomerRecognition{TenantID: tenantID, CollectionStatus: "awaiting_payment"}
	if err := checkCustomerBusinessAccess(ctx, s.db, sc, tenantID); err != nil {
		return out, err
	}
	var at sql.NullTime
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT receipt_id,confirmed_at,confirmed_amount_cents FROM fin_customer_confirmations WHERE tenant_id=?", tenantID).Scan(&id, &at, &out.ConfirmedAmountCents)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out.Qualified = err == nil
	if id.Valid {
		out.ConfirmationReceiptID = &id.Int64
	}
	if at.Valid {
		out.ConfirmedAt = &at.Time
	}
	err = s.db.QueryRowContext(ctx, "SELECT status FROM fin_customer_receipts WHERE tenant_id=? ORDER BY id DESC LIMIT 1", tenantID).Scan(&out.CollectionStatus)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fin_customer_receipts WHERE tenant_id=? AND status IN ('pending','posting_failed')", tenantID).Scan(&out.PendingCount); err != nil {
		return out, err
	}
	return out, nil
}
