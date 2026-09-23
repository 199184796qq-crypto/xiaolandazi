package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) MigrateOrganizations(ctx context.Context) error {
	columns := []struct {
		name string
		sql  string
	}{
		{"parent_id", "ALTER TABLE mgmt_tenants ADD COLUMN parent_id BIGINT UNSIGNED NULL AFTER id"},
		{"org_type", "ALTER TABLE mgmt_tenants ADD COLUMN org_type VARCHAR(32) NOT NULL DEFAULT 'customer' AFTER parent_id"},
		{"level", "ALTER TABLE mgmt_tenants ADD COLUMN level INT NOT NULL DEFAULT 1 AFTER org_type"},
	}
	for _, column := range columns {
		ok, err := s.columnExists(ctx, "mgmt_tenants", column.name)
		if err != nil {
			return err
		}
		if !ok {
			if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add mgmt_tenants.%s: %w", column.name, err)
			}
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (NULL, 'platform', 0, 'platform', '伴播搭子平台', 'active')
		ON DUPLICATE KEY UPDATE
			org_type='platform',
			level=0,
			status='active'
	`); err != nil {
		return fmt.Errorf("ensure platform organization: %w", err)
	}

	var platformID int64
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT id FROM mgmt_tenants WHERE code='platform' LIMIT 1",
	).Scan(&platformID); err != nil {
		return fmt.Errorf("load platform organization: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_tenants
		SET org_type='customer',
		    level=1,
		    parent_id=?
		WHERE id<>?
		  AND (org_type IS NULL OR org_type='' OR org_type='customer')
		  AND parent_id IS NULL
	`, platformID, platformID); err != nil {
		return fmt.Errorf("backfill direct customer organizations: %w", err)
	}

	return nil
}

func (s *Store) PlatformOrganizationID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(
		ctx,
		"SELECT id FROM mgmt_tenants WHERE org_type='platform' ORDER BY id LIMIT 1",
	).Scan(&id)
	return id, err
}

func (s *Store) ListAgents(ctx context.Context) ([]model.AgentSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.id,
			t.code,
			t.name,
			t.status,
			t.created_at,
			COALESCE(u.id, 0),
			COALESCE(u.username, ''),
			COALESCE(u.display_name, ''),
			COALESCE(u.phone, ''),
			COALESCE(u.email, ''),
			COALESCE(u.status, ''),
			(
				SELECT COUNT(*)
				FROM mgmt_tenants c
				WHERE c.parent_id=t.id
				  AND c.org_type='customer'
			) AS customer_count
		FROM mgmt_tenants t
		LEFT JOIN mgmt_users u
		  ON u.tenant_id=t.id
		 AND u.role='agent_admin'
		WHERE t.org_type='agent'
		ORDER BY t.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentSummary, 0)
	for rows.Next() {
		var item model.AgentSummary
		if err := rows.Scan(
			&item.OrganizationID,
			&item.Code,
			&item.Name,
			&item.Status,
			&item.CreatedAt,
			&item.AdminUserID,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Email,
			&item.UserStatus,
			&item.CustomerCount,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateAgent(
	ctx context.Context,
	code string,
	name string,
	username string,
	displayName string,
	phone string,
	email string,
	province string,
	city string,
	district string,
	passwordHash string,
) (model.AgentSummary, error) {
	code = strings.TrimSpace(code)
	name = strings.TrimSpace(name)
	username = strings.TrimSpace(username)
	displayName = strings.TrimSpace(displayName)
	phone = strings.TrimSpace(phone)
	email = strings.TrimSpace(email)
	province = strings.TrimSpace(province)
	city = strings.TrimSpace(city)
	district = strings.TrimSpace(district)

	platformID, err := s.PlatformOrganizationID(ctx)
	if err != nil {
		return model.AgentSummary{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentSummary{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (?, 'agent', 1, ?, ?, 'active')
	`, platformID, code, name)
	if err != nil {
		return model.AgentSummary{}, normalizeDuplicate(err)
	}
	orgID, err := result.LastInsertId()
	if err != nil {
		return model.AgentSummary{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id, username, password_hash, must_change_password, display_name, phone, email, province, city, district, role, status
		)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 'agent_admin', 'active')
	`, orgID, username, passwordHash, displayName, phone, email, province, city, district)
	if err != nil {
		return model.AgentSummary{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.AgentSummary{}, err
	}

	if err := ensureOrganizationResourceAccountsTx(ctx, tx, orgID, "agent"); err != nil {
		return model.AgentSummary{}, fmt.Errorf("create agent resource accounts: %w", err)
	}

	agentTenantID := orgID
	if err := ensureUserInviteCodeTx(ctx, tx, userID, &agentTenantID, "agent_admin"); err != nil {
		return model.AgentSummary{}, err
	}

	result, err = tx.ExecContext(ctx, `
		INSERT INTO crm_agent_orgs (
			mgmt_tenant_id, code, name, status
		)
		VALUES (?, ?, ?, 'active')
	`, orgID, code, name)
	if err != nil {
		return model.AgentSummary{}, fmt.Errorf("create commercial agent organization: %w", err)
	}
	commercialAgentID, err := result.LastInsertId()
	if err != nil {
		return model.AgentSummary{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_agent_members (
			agent_org_id, user_id, member_role, status
		)
		VALUES (?, ?, 'agent_admin', 'active')
	`, commercialAgentID, userID); err != nil {
		return model.AgentSummary{}, fmt.Errorf("create commercial agent member: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.AgentSummary{}, err
	}

	return model.AgentSummary{
		OrganizationID: orgID,
		Code:           code,
		Name:           name,
		Status:         "active",
		CreatedAt:      time.Now().UTC(),
		AdminUserID:    userID,
		Username:       username,
		DisplayName:    displayName,
		Phone:          phone,
		Email:          email,
		UserStatus:     "active",
		CustomerCount:  0,
	}, nil
}

func (s *Store) ListAgentCustomers(
	ctx context.Context,
	agentTenantID int64,
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
			u.created_at
		FROM mgmt_users u
		INNER JOIN mgmt_tenants t ON t.id=u.tenant_id
		LEFT JOIN mgmt_tenants parent ON parent.id=t.parent_id
		LEFT JOIN crm_customer_profiles p ON p.tenant_id=t.id
		LEFT JOIN crm_registration_referrals rr ON rr.referred_user_id=u.id
		LEFT JOIN mgmt_users inviter ON inviter.id=rr.inviter_user_id
		WHERE u.role='customer'
		  AND t.org_type='customer'
		  AND t.parent_id=?
		ORDER BY u.id DESC
	`, agentTenantID)
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
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) CreateAgentCustomer(
	ctx context.Context,
	agentTenantID int64,
	operatorUserID int64,
	username string,
	displayName string,
	phone string,
	email string,
	province string,
	city string,
	district string,
	passwordHash string,
) (model.User, error) {
	var agentType string
	if err := s.db.QueryRowContext(ctx, `
		SELECT org_type
		FROM mgmt_tenants
		WHERE id=? AND status='active'
	`, agentTenantID).Scan(&agentType); err != nil {
		return model.User{}, err
	}
	if agentType != "agent" {
		return model.User{}, sql.ErrNoRows
	}

	tenantCode, err := randomTenantCode()
	if err != nil {
		return model.User{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (?, 'customer', 2, ?, ?, 'active')
	`, agentTenantID, tenantCode, strings.TrimSpace(displayName))
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	tenantID, err := result.LastInsertId()
	if err != nil {
		return model.User{}, err
	}
	result, err = tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id, username, password_hash, must_change_password, display_name, phone, email, province, city, district, role, status
		)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, 'customer', 'active')
	`, tenantID, strings.TrimSpace(username), passwordHash, strings.TrimSpace(displayName), strings.TrimSpace(phone), strings.TrimSpace(email), strings.TrimSpace(province), strings.TrimSpace(city), strings.TrimSpace(district))
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.User{}, err
	}

	if err := ensureOrganizationResourceAccountsTx(ctx, tx, tenantID, "customer"); err != nil {
		return model.User{}, fmt.Errorf("create customer resource accounts: %w", err)
	}

	customerTenantID := tenantID
	if err := ensureUserInviteCodeTx(ctx, tx, userID, &customerTenantID, "customer"); err != nil {
		return model.User{}, fmt.Errorf("create agent customer invite code: %w", err)
	}

	var commercialAgentID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM crm_agent_orgs
		WHERE mgmt_tenant_id=?
		LIMIT 1
	`, agentTenantID).Scan(&commercialAgentID); err != nil {
		return model.User{}, fmt.Errorf("load commercial agent organization: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_profiles (
			tenant_id, source_type, source_agent_org_id, source_note
		)
		VALUES (?, 'agent', ?, 'agent opened account')
	`, tenantID, commercialAgentID); err != nil {
		return model.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_agent_relations (
			tenant_id,
			agent_org_id,
			relation_type,
			status,
			effective_from,
			assigned_by_user_id,
			reason
		)
		VALUES (?, ?, 'primary', 'active', CURRENT_TIMESTAMP(3), NULL, 'agent opened account')
	`, tenantID, commercialAgentID); err != nil {
		return model.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_accounts (
			tenant_id, account_type, currency, balance_cents, status
		)
		VALUES
			(?, 'cash', 'CNY', 0, 'active'),
			(?, 'reward', 'CNY', 0, 'active')
	`, tenantID, tenantID); err != nil {
		return model.User{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.User{}, err
	}

	return model.User{
		ID:                 userID,
		TenantID:           &tenantID,
		Username:           strings.TrimSpace(username),
		PasswordHash:       passwordHash,
		DisplayName:        strings.TrimSpace(displayName),
		Phone:              strings.TrimSpace(phone),
		Province:           strings.TrimSpace(province),
		City:               strings.TrimSpace(city),
		District:           strings.TrimSpace(district),
		Role:               "customer",
		MustChangePassword: true,
		Status:             "active",
		CreatedAt:          time.Now().UTC(),
	}, nil
}
