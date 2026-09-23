package db

import (
	"context"
	"database/sql"
	"fmt"

	"livecompanion/management/internal/model"
)

func (s *Store) ListAdminCustomers(
	ctx context.Context,
) ([]model.AdminCustomer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.status,
			COALESCE(p.source_type, 'unknown'),
			COALESCE(parent.id, 0),
			COALESCE(parent.name, ''),
			COALESCE(parent.org_type, ''),
			COALESCE(rr.inviter_user_id, 0),
			COALESCE(inviter.username, ''),
			COALESCE(inviter.display_name, ''),
			COALESCE(sa.sales_staff_id, 0),
			COALESCE(sales_user.id, 0),
			COALESCE(ss.employee_code, ''),
			COALESCE(sales_user.username, ''),
			COALESCE(sales_user.display_name, ''),
			u.created_at
		FROM mgmt_users u
		INNER JOIN mgmt_tenants customer_org ON customer_org.id=u.tenant_id
		LEFT JOIN mgmt_tenants parent ON parent.id=customer_org.parent_id
		LEFT JOIN crm_customer_profiles p ON p.tenant_id=u.tenant_id
		LEFT JOIN crm_registration_referrals rr ON rr.referred_user_id=u.id
		LEFT JOIN mgmt_users inviter ON inviter.id=rr.inviter_user_id
		LEFT JOIN crm_customer_sales_assignments sa
		  ON sa.tenant_id=u.tenant_id
		 AND sa.status='active'
		 AND sa.effective_to IS NULL
		LEFT JOIN crm_sales_staff ss ON ss.id=sa.sales_staff_id
		LEFT JOIN mgmt_users sales_user ON sales_user.id=ss.user_id
		WHERE u.role='customer'
		  AND u.tenant_id IS NOT NULL
		ORDER BY u.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminCustomer, 0)
	for rows.Next() {
		var item model.AdminCustomer
		if err := rows.Scan(
			&item.UserID,
			&item.TenantID,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.Status,
			&item.SourceType,
			&item.ParentOrgID,
			&item.ParentOrgName,
			&item.ParentOrgType,
			&item.InviterUserID,
			&item.InviterUsername,
			&item.InviterDisplayName,
			&item.SalesStaffID,
			&item.SalesUserID,
			&item.SalesEmployeeCode,
			&item.SalesUsername,
			&item.SalesDisplayName,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetAdminCustomer(
	ctx context.Context,
	userID int64,
) (model.AdminCustomer, error) {
	var item model.AdminCustomer
	err := s.db.QueryRowContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.status,
			COALESCE(p.source_type, 'unknown'),
			COALESCE(parent.id, 0),
			COALESCE(parent.name, ''),
			COALESCE(parent.org_type, ''),
			COALESCE(rr.inviter_user_id, 0),
			COALESCE(inviter.username, ''),
			COALESCE(inviter.display_name, ''),
			COALESCE(sa.sales_staff_id, 0),
			COALESCE(sales_user.id, 0),
			COALESCE(ss.employee_code, ''),
			COALESCE(sales_user.username, ''),
			COALESCE(sales_user.display_name, ''),
			u.created_at
		FROM mgmt_users u
		INNER JOIN mgmt_tenants customer_org ON customer_org.id=u.tenant_id
		LEFT JOIN mgmt_tenants parent ON parent.id=customer_org.parent_id
		LEFT JOIN crm_customer_profiles p ON p.tenant_id=u.tenant_id
		LEFT JOIN crm_registration_referrals rr ON rr.referred_user_id=u.id
		LEFT JOIN mgmt_users inviter ON inviter.id=rr.inviter_user_id
		LEFT JOIN crm_customer_sales_assignments sa
		  ON sa.tenant_id=u.tenant_id
		 AND sa.status='active'
		 AND sa.effective_to IS NULL
		LEFT JOIN crm_sales_staff ss ON ss.id=sa.sales_staff_id
		LEFT JOIN mgmt_users sales_user ON sales_user.id=ss.user_id
		WHERE u.id=?
		  AND u.role='customer'
		  AND u.tenant_id IS NOT NULL
		LIMIT 1
	`, userID).Scan(
		&item.UserID,
		&item.TenantID,
		&item.Username,
		&item.DisplayName,
		&item.Phone,
		&item.Email,
		&item.Status,
		&item.SourceType,
		&item.ParentOrgID,
		&item.ParentOrgName,
		&item.ParentOrgType,
		&item.InviterUserID,
		&item.InviterUsername,
		&item.InviterDisplayName,
		&item.SalesStaffID,
		&item.SalesUserID,
		&item.SalesEmployeeCode,
		&item.SalesUsername,
		&item.SalesDisplayName,
		&item.CreatedAt,
	)
	return item, err
}
func (s *Store) AdminResetCustomerPassword(
	ctx context.Context,
	userID int64,
	passwordHash string,
) (model.AdminCustomer, error) {
	customer, err := s.GetAdminCustomer(ctx, userID)
	if err != nil {
		return model.AdminCustomer{}, err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_users
		SET password_hash = ?,
		    must_change_password = 1
		WHERE id = ?
		  AND role = 'customer'
		  AND status = 'active'
	`, passwordHash, userID)
	if err != nil {
		return model.AdminCustomer{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.AdminCustomer{}, err
	}
	if affected == 0 {
		return model.AdminCustomer{}, sql.ErrNoRows
	}

	if err := s.DeleteUserSessions(ctx, userID); err != nil {
		return model.AdminCustomer{}, err
	}
	return customer, nil
}

func (s *Store) DeleteAdminCustomer(
	ctx context.Context,
	userID int64,
) (model.AdminCustomer, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdminCustomer{}, err
	}
	defer tx.Rollback()

	var customer model.AdminCustomer
	err = tx.QueryRowContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.display_name,
			u.phone,
			u.status,
			u.created_at
		FROM mgmt_users u
		WHERE u.id = ?
		  AND u.role = 'customer'
		  AND u.tenant_id IS NOT NULL
		FOR UPDATE
	`, userID).Scan(
		&customer.UserID,
		&customer.TenantID,
		&customer.Username,
		&customer.DisplayName,
		&customer.Status,
		&customer.CreatedAt,
	)
	if err != nil {
		return model.AdminCustomer{}, err
	}

	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM mgmt_sessions WHERE user_id = ?",
		userID,
	); err != nil {
		return model.AdminCustomer{}, fmt.Errorf("delete customer sessions: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM mgmt_users WHERE id = ? AND role = 'customer'",
		userID,
	); err != nil {
		return model.AdminCustomer{}, fmt.Errorf("delete customer user: %w", err)
	}

	var otherUsers int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mgmt_users
		WHERE tenant_id = ?
	`, customer.TenantID).Scan(&otherUsers); err != nil {
		return model.AdminCustomer{}, err
	}
	if otherUsers == 0 {
		if _, err := tx.ExecContext(
			ctx,
			"DELETE FROM mgmt_tenants WHERE id = ?",
			customer.TenantID,
		); err != nil {
			return model.AdminCustomer{}, fmt.Errorf("delete customer tenant: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return model.AdminCustomer{}, err
	}
	return customer, nil
}
