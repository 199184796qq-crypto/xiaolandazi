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
			u.status,
			u.created_at
		FROM mgmt_users u
		WHERE u.role = 'customer'
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
			&item.Status,
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
			u.status,
			u.created_at
		FROM mgmt_users u
		WHERE u.id = ?
		  AND u.role = 'customer'
		  AND u.tenant_id IS NOT NULL
		LIMIT 1
	`, userID).Scan(
		&item.UserID,
		&item.TenantID,
		&item.Username,
		&item.DisplayName,
		&item.Status,
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
		SET password_hash = ?
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
