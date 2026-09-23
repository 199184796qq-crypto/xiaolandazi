package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

const defaultSalesTeamCode = "platform-sales"

func (s *Store) MigrateSales(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_sales_teams (
			code, name, status
		)
		VALUES (?, '平台销售部', 'active')
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			status='active'
	`, defaultSalesTeamCode); err != nil {
		return fmt.Errorf("ensure default sales team: %w", err)
	}
	return nil
}

func (s *Store) defaultSalesTeamID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM crm_sales_teams
		WHERE code=? AND status='active'
		LIMIT 1
	`, defaultSalesTeamCode).Scan(&id)
	return id, err
}

func (s *Store) ListSalesStaff(
	ctx context.Context,
) ([]model.SalesStaffSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			s.id,
			u.id,
			s.employee_code,
			u.username,
			u.display_name,
			u.phone,
			u.email,
			u.province,
			u.city,
			u.district,
			s.status,
			s.team_id,
			COALESCE(t.name, ''),
			(
				SELECT COUNT(*)
				FROM crm_customer_sales_assignments a
				WHERE a.sales_staff_id=s.id
				  AND a.status='active'
				  AND a.effective_to IS NULL
			) AS customer_count,
			u.created_at
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		LEFT JOIN crm_sales_teams t ON t.id=s.team_id
		ORDER BY s.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SalesStaffSummary, 0)
	for rows.Next() {
		var item model.SalesStaffSummary
		var teamID sql.NullInt64
		if err := rows.Scan(
			&item.StaffID,
			&item.UserID,
			&item.EmployeeCode,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.Province,
			&item.City,
			&item.District,
			&item.Status,
			&teamID,
			&item.TeamName,
			&item.CustomerCount,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if teamID.Valid {
			value := teamID.Int64
			item.TeamID = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateSalesStaff(
	ctx context.Context,
	employeeCode string,
	username string,
	displayName string,
	phone string,
	email string,
	province string,
	city string,
	district string,
	passwordHash string,
) (model.SalesStaffSummary, error) {
	employeeCode = strings.TrimSpace(employeeCode)
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	email = strings.TrimSpace(email)
	province = strings.TrimSpace(province)
	city = strings.TrimSpace(city)
	district = strings.TrimSpace(district)

	teamID, err := s.defaultSalesTeamID(ctx)
	if err != nil {
		return model.SalesStaffSummary{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.SalesStaffSummary{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id,
			username,
			password_hash,
			must_change_password,
			display_name,
			phone,
			email,
			province,
			city,
			district,
			role,
			status
		)
		VALUES (
			NULL, ?, ?, 1, ?, ?, ?, ?, ?, ?, 'sales_staff', 'active'
		)
	`,
		username,
		passwordHash,
		displayName,
		phone,
		email,
		province,
		city,
		district,
	)
	if err != nil {
		return model.SalesStaffSummary{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.SalesStaffSummary{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO crm_sales_staff (
			user_id,
			employee_code,
			team_id,
			status
		)
		VALUES (?, ?, ?, 'active')
	`, userID, employeeCode, teamID)
	if err != nil {
		return model.SalesStaffSummary{}, normalizeDuplicate(err)
	}
	staffID, err := result.LastInsertId()
	if err != nil {
		return model.SalesStaffSummary{}, err
	}

	var staffGroupID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM staff_groups
		WHERE code='sales' AND status='active'
		LIMIT 1
	`).Scan(&staffGroupID); err != nil {
		return model.SalesStaffSummary{}, err
	}

	var staffRoleID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM staff_roles
		WHERE code='sales_staff'
		  AND group_id=?
		  AND status='active'
		LIMIT 1
	`, staffGroupID).Scan(&staffRoleID); err != nil {
		return model.SalesStaffSummary{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO staff_employees (
			user_id,
			employee_no,
			primary_group_id,
			employment_status
		)
		VALUES (?, ?, ?, 'active')
	`, userID, employeeCode, staffGroupID)
	if err != nil {
		return model.SalesStaffSummary{}, normalizeDuplicate(err)
	}
	internalEmployeeID, err := result.LastInsertId()
	if err != nil {
		return model.SalesStaffSummary{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO staff_employee_roles (
			employee_id,
			role_id,
			scope_type
		)
		VALUES (?, ?, 'assigned')
	`, internalEmployeeID, staffRoleID); err != nil {
		return model.SalesStaffSummary{}, err
	}

	if err := ensureUserInviteCodeTx(
		ctx,
		tx,
		userID,
		nil,
		"sales_staff",
	); err != nil {
		return model.SalesStaffSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.SalesStaffSummary{}, err
	}

	return model.SalesStaffSummary{
		StaffID:       staffID,
		UserID:        userID,
		EmployeeCode:  employeeCode,
		Username:      username,
		DisplayName:   displayName,
		Phone:         phone,
		Email:         email,
		Province:      province,
		City:          city,
		District:      district,
		Status:        "active",
		TeamID:        &teamID,
		TeamName:      "平台销售部",
		CustomerCount: 0,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

func (s *Store) AssignCustomerToSales(
	ctx context.Context,
	customerTenantID int64,
	salesStaffID int64,
	assignedByUserID int64,
	reason string,
) error {
	reason = strings.TrimSpace(reason)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var parentType string
	if err := tx.QueryRowContext(ctx, `
		SELECT parent.org_type
		FROM mgmt_tenants customer
		INNER JOIN mgmt_tenants parent ON parent.id=customer.parent_id
		WHERE customer.id=?
		  AND customer.org_type='customer'
		  AND customer.status='active'
	`, customerTenantID).Scan(&parentType); err != nil {
		return err
	}
	if parentType != "platform" {
		return fmt.Errorf("sales can only be assigned to platform direct customers")
	}

	var salesUserID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT s.user_id
		FROM crm_sales_staff s
		INNER JOIN mgmt_users u ON u.id=s.user_id
		WHERE s.id=?
		  AND s.status='active'
		  AND u.status='active'
	`, salesStaffID).Scan(&salesUserID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE crm_customer_sales_assignments
		SET status='ended',
		    effective_to=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_to IS NULL
	`, customerTenantID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_sales_assignments (
			tenant_id,
			sales_staff_id,
			status,
			effective_from,
			assigned_by_user_id,
			reason
		)
		VALUES (
			?,
			?,
			'active',
			CURRENT_TIMESTAMP(3),
			?,
			?
		)
	`,
		customerTenantID,
		salesStaffID,
		assignedByUserID,
		reason,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) ClearCustomerSalesAssignment(
	ctx context.Context,
	customerTenantID int64,
	assignedByUserID int64,
	reason string,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE crm_customer_sales_assignments
		SET status='ended',
		    effective_to=CURRENT_TIMESTAMP(3),
		    reason=CASE
				WHEN ?<>'' THEN ?
				ELSE reason
			END
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_to IS NULL
	`,
		strings.TrimSpace(reason),
		strings.TrimSpace(reason),
		customerTenantID,
	)
	if err != nil {
		return err
	}
	_, err = result.RowsAffected()
	return err
}

func (s *Store) ListSalesCustomersByUser(
	ctx context.Context,
	salesUserID int64,
) ([]model.AdminCustomer, error) {
	var staffID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM crm_sales_staff
		WHERE user_id=?
		  AND status='active'
		LIMIT 1
	`, salesUserID).Scan(&staffID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.display_name,
			u.phone,
			u.status,
			COALESCE(p.source_type, 'unknown'),
			COALESCE(parent.id, 0),
			COALESCE(parent.name, ''),
			COALESCE(parent.org_type, ''),
			COALESCE(rr.inviter_user_id, 0),
			COALESCE(inviter.username, ''),
			COALESCE(inviter.display_name, ''),
			u.created_at
		FROM crm_customer_sales_assignments a
		INNER JOIN mgmt_tenants customer_org
			ON customer_org.id=a.tenant_id
		INNER JOIN mgmt_users u
			ON u.tenant_id=customer_org.id
		   AND u.role='customer'
		LEFT JOIN mgmt_tenants parent
			ON parent.id=customer_org.parent_id
		LEFT JOIN crm_customer_profiles p
			ON p.tenant_id=customer_org.id
		LEFT JOIN crm_registration_referrals rr
			ON rr.referred_user_id=u.id
		LEFT JOIN mgmt_users inviter
			ON inviter.id=rr.inviter_user_id
		WHERE a.sales_staff_id=?
		  AND a.status='active'
		  AND a.effective_to IS NULL
		ORDER BY u.id DESC
	`, staffID)
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
			&item.Status,
			&item.SourceType,
			&item.ParentOrgID,
			&item.ParentOrgName,
			&item.ParentOrgType,
			&item.InviterUserID,
			&item.InviterUsername,
			&item.InviterDisplayName,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) SalesStaffIDByUser(
	ctx context.Context,
	userID int64,
) (int64, error) {
	var staffID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id
		FROM crm_sales_staff
		WHERE user_id=?
		  AND status='active'
		LIMIT 1
	`, userID).Scan(&staffID)
	return staffID, err
}
