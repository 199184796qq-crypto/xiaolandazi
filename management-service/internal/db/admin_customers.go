package db

import (
	"context"
	"database/sql"

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
			COALESCE(policy_industry.industry_code, 'general'),
			COALESCE(industry.name, '通用'),
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
		LEFT JOIN live_policy_tenant_industries policy_industry
		  ON policy_industry.tenant_id=u.tenant_id
		LEFT JOIN live_policy_industries industry
		  ON industry.code=COALESCE(policy_industry.industry_code, 'general')
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
			&item.IndustryCode,
			&item.IndustryName,
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
			COALESCE(policy_industry.industry_code, 'general'),
			COALESCE(industry.name, '通用'),
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
		LEFT JOIN live_policy_tenant_industries policy_industry
		  ON policy_industry.tenant_id=u.tenant_id
		LEFT JOIN live_policy_industries industry
		  ON industry.code=COALESCE(policy_industry.industry_code, 'general')
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
		&item.IndustryCode,
		&item.IndustryName,
		&item.CreatedAt,
	)
	return item, err
}

// CanResetCustomerPassword extends the caller's existing customer scope with
// explicit grants tied to one employee, customer account, and tenant.
func (s *Store) CanResetCustomerPassword(
	ctx context.Context,
	scope model.StaffBusinessScope,
	employeeID, userID int64,
) (bool, error) {
	visible, err := s.AdminCustomerVisibleToScope(ctx, scope, userID)
	if visible || err != nil || employeeID <= 0 {
		return visible, err
	}

	err = s.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM mgmt_users u
			JOIN staff_employees e ON e.id=? AND e.user_id=? AND e.employment_status='active'
			JOIN sys_feature_records grant_record
			  ON grant_record.feature_key='staff-customer-password-reset-grants'
			 AND grant_record.record_key=CONCAT(e.id, ':', u.id, ':', u.tenant_id)
			 AND grant_record.status='active'
			WHERE u.id=? AND u.role='customer' AND u.tenant_id IS NOT NULL AND u.status='active'
		)
	`, employeeID, scope.ActorUserID, userID).Scan(&visible)
	return visible, err
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
