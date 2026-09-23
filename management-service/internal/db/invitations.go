package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrInviteCodeInvalid = errors.New("invite code invalid")

const inviteAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func (s *Store) MigrateInvitations(ctx context.Context) error {
	statements := []string{
		`
		CREATE TABLE IF NOT EXISTS iam_invite_codes (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			code VARCHAR(32) NOT NULL,
			owner_user_id BIGINT UNSIGNED NOT NULL,
			owner_tenant_id BIGINT UNSIGNED NULL,
			owner_role VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			max_uses BIGINT UNSIGNED NOT NULL DEFAULT 0,
			used_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
			expires_at DATETIME(3) NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_iam_invite_codes_code (code),
			UNIQUE KEY uk_iam_invite_codes_owner (owner_user_id),
			KEY idx_iam_invite_codes_status (status, expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
		`,
		`
		CREATE TABLE IF NOT EXISTS crm_registration_referrals (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			invite_code_id BIGINT UNSIGNED NOT NULL,
			inviter_user_id BIGINT UNSIGNED NOT NULL,
			inviter_tenant_id BIGINT UNSIGNED NULL,
			referred_user_id BIGINT UNSIGNED NOT NULL,
			referred_tenant_id BIGINT UNSIGNED NOT NULL,
			source_type VARCHAR(32) NOT NULL,
			parent_org_id BIGINT UNSIGNED NOT NULL,
			bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_crm_registration_referrals_user (referred_user_id),
			UNIQUE KEY uk_crm_registration_referrals_tenant (referred_tenant_id),
			KEY idx_crm_registration_referrals_inviter (inviter_user_id, bound_at),
			KEY idx_crm_registration_referrals_parent (parent_org_id, bound_at),
			KEY idx_crm_registration_referrals_code (invite_code_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
		`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate invitations: %w", err)
		}
	}

	hasTenantLink, err := s.columnExists(ctx, "crm_agent_orgs", "mgmt_tenant_id")
	if err != nil {
		return err
	}
	if !hasTenantLink {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE crm_agent_orgs
			ADD COLUMN mgmt_tenant_id BIGINT UNSIGNED NULL AFTER id
		`); err != nil {
			return fmt.Errorf("add crm_agent_orgs.mgmt_tenant_id: %w", err)
		}
	}

	hasAgentTenantIndex, err := s.indexExists(ctx, "crm_agent_orgs", "uk_crm_agent_orgs_mgmt_tenant")
	if err != nil {
		return err
	}
	if !hasAgentTenantIndex {
		if _, err := s.db.ExecContext(ctx, `
			CREATE UNIQUE INDEX uk_crm_agent_orgs_mgmt_tenant
			ON crm_agent_orgs (mgmt_tenant_id)
		`); err != nil {
			return fmt.Errorf("index crm_agent_orgs.mgmt_tenant_id: %w", err)
		}
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_agent_orgs (
			mgmt_tenant_id, code, name, status
		)
		SELECT id, code, name, status
		FROM mgmt_tenants
		WHERE org_type='agent'
		ON DUPLICATE KEY UPDATE
			name=VALUES(name),
			status=VALUES(status),
			mgmt_tenant_id=COALESCE(crm_agent_orgs.mgmt_tenant_id, VALUES(mgmt_tenant_id))
	`); err != nil {
		return fmt.Errorf("backfill commercial agent orgs: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO crm_agent_members (
			agent_org_id, user_id, member_role, status
		)
		SELECT
			a.id,
			u.id,
			'agent_admin',
			u.status
		FROM mgmt_users u
		INNER JOIN crm_agent_orgs a ON a.mgmt_tenant_id=u.tenant_id
		WHERE u.role='agent_admin'
	`); err != nil {
		return fmt.Errorf("backfill commercial agent members: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_customer_agent_relations (
			tenant_id,
			agent_org_id,
			relation_type,
			status,
			effective_from,
			reason
		)
		SELECT
			c.id,
			a.id,
			'primary',
			'active',
			CURRENT_TIMESTAMP(3),
			'organization tree backfill'
		FROM mgmt_tenants c
		INNER JOIN mgmt_tenants parent ON parent.id=c.parent_id AND parent.org_type='agent'
		INNER JOIN crm_agent_orgs a ON a.mgmt_tenant_id=parent.id
		WHERE c.org_type='customer'
		  AND NOT EXISTS (
			SELECT 1
			FROM crm_customer_agent_relations r
			WHERE r.tenant_id=c.id
			  AND r.status='active'
		  )
	`); err != nil {
		return fmt.Errorf("backfill customer agent relations: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT u.id, u.tenant_id, u.role
		FROM mgmt_users u
		LEFT JOIN iam_invite_codes c ON c.owner_user_id=u.id
		WHERE u.status='active'
		  AND c.id IS NULL
		ORDER BY u.id
	`)
	if err != nil {
		return fmt.Errorf("list users missing invite codes: %w", err)
	}
	defer rows.Close()

	type missingUser struct {
		id       int64
		tenantID sql.NullInt64
		role     string
	}
	missing := make([]missingUser, 0)
	for rows.Next() {
		var item missingUser
		if err := rows.Scan(&item.id, &item.tenantID, &item.role); err != nil {
			return err
		}
		missing = append(missing, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, item := range missing {
		var tenantID *int64
		if item.tenantID.Valid {
			value := item.tenantID.Int64
			tenantID = &value
		}
		if _, err := s.ensureUserInviteCode(ctx, item.id, tenantID, item.role); err != nil {
			return fmt.Errorf("ensure invite code for user %d: %w", item.id, err)
		}
	}

	return nil
}

func (s *Store) indexExists(
	ctx context.Context,
	tableName string,
	indexName string,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA=DATABASE()
		  AND TABLE_NAME=?
		  AND INDEX_NAME=?
	`, tableName, indexName).Scan(&count)
	return count > 0, err
}

func generateInviteCode() (string, error) {
	const size = 10
	buf := make([]byte, size)
	random := make([]byte, size)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = inviteAlphabet[int(random[i])%len(inviteAlphabet)]
	}
	return string(buf), nil
}

func (s *Store) ensureUserInviteCode(
	ctx context.Context,
	userID int64,
	tenantID *int64,
	role string,
) (model.InviteCodeSummary, error) {
	if item, err := s.GetInviteCodeForUser(ctx, userID); err == nil {
		return item, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return model.InviteCodeSummary{}, err
	}

	for attempt := 0; attempt < 12; attempt++ {
		code, err := generateInviteCode()
		if err != nil {
			return model.InviteCodeSummary{}, err
		}
		_, err = s.db.ExecContext(ctx, `
			INSERT INTO iam_invite_codes (
				code, owner_user_id, owner_tenant_id, owner_role, status
			)
			VALUES (?, ?, ?, ?, 'active')
		`, code, userID, tenantID, role)
		if err == nil {
			return s.GetInviteCodeForUser(ctx, userID)
		}
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			if item, loadErr := s.GetInviteCodeForUser(ctx, userID); loadErr == nil {
				return item, nil
			}
			continue
		}
		return model.InviteCodeSummary{}, err
	}
	return model.InviteCodeSummary{}, fmt.Errorf("generate unique invite code")
}

func ensureUserInviteCodeTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	tenantID *int64,
	role string,
) error {
	for attempt := 0; attempt < 12; attempt++ {
		code, err := generateInviteCode()
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO iam_invite_codes (
				code, owner_user_id, owner_tenant_id, owner_role, status
			)
			VALUES (?, ?, ?, ?, 'active')
		`, code, userID, tenantID, role)
		if err == nil {
			return nil
		}
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			var count int
			if scanErr := tx.QueryRowContext(
				ctx,
				"SELECT COUNT(*) FROM iam_invite_codes WHERE owner_user_id=?",
				userID,
			).Scan(&count); scanErr == nil && count > 0 {
				return nil
			}
			continue
		}
		return err
	}
	return fmt.Errorf("generate unique invite code")
}

func (s *Store) GetInviteCodeForUser(
	ctx context.Context,
	userID int64,
) (model.InviteCodeSummary, error) {
	var item model.InviteCodeSummary
	var ownerTenantID sql.NullInt64
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			c.id,
			c.code,
			c.owner_user_id,
			c.owner_tenant_id,
			c.owner_role,
			u.username,
			u.display_name,
			c.status,
			c.max_uses,
			c.used_count,
			c.expires_at,
			c.created_at
		FROM iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		WHERE c.owner_user_id=?
		LIMIT 1
	`, userID).Scan(
		&item.ID,
		&item.Code,
		&item.OwnerUserID,
		&ownerTenantID,
		&item.OwnerRole,
		&item.OwnerUsername,
		&item.OwnerName,
		&item.Status,
		&item.MaxUses,
		&item.UsedCount,
		&expiresAt,
		&item.CreatedAt,
	)
	if err != nil {
		return model.InviteCodeSummary{}, err
	}
	if ownerTenantID.Valid {
		value := ownerTenantID.Int64
		item.OwnerTenantID = &value
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		item.ExpiresAt = &value
	}
	return item, nil
}

func (s *Store) GetInviteCodeByID(
	ctx context.Context,
	codeID int64,
) (model.InviteCodeSummary, error) {
	var item model.InviteCodeSummary
	var ownerTenantID sql.NullInt64
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			c.id,
			c.code,
			c.owner_user_id,
			c.owner_tenant_id,
			c.owner_role,
			u.username,
			u.display_name,
			c.status,
			c.max_uses,
			c.used_count,
			c.expires_at,
			c.created_at
		FROM iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		WHERE c.id=?
		LIMIT 1
	`, codeID).Scan(
		&item.ID,
		&item.Code,
		&item.OwnerUserID,
		&ownerTenantID,
		&item.OwnerRole,
		&item.OwnerUsername,
		&item.OwnerName,
		&item.Status,
		&item.MaxUses,
		&item.UsedCount,
		&expiresAt,
		&item.CreatedAt,
	)
	if err != nil {
		return model.InviteCodeSummary{}, err
	}
	if ownerTenantID.Valid {
		value := ownerTenantID.Int64
		item.OwnerTenantID = &value
	}
	if expiresAt.Valid {
		value := expiresAt.Time
		item.ExpiresAt = &value
	}
	return item, nil
}
func (s *Store) ListInviteCodes(ctx context.Context) ([]model.InviteCodeSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			c.id,
			c.code,
			c.owner_user_id,
			c.owner_tenant_id,
			c.owner_role,
			u.username,
			u.display_name,
			c.status,
			c.max_uses,
			c.used_count,
			c.expires_at,
			c.created_at
		FROM iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		ORDER BY c.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.InviteCodeSummary, 0)
	for rows.Next() {
		var item model.InviteCodeSummary
		var ownerTenantID sql.NullInt64
		var expiresAt sql.NullTime
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.OwnerUserID,
			&ownerTenantID,
			&item.OwnerRole,
			&item.OwnerUsername,
			&item.OwnerName,
			&item.Status,
			&item.MaxUses,
			&item.UsedCount,
			&expiresAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		if ownerTenantID.Valid {
			value := ownerTenantID.Int64
			item.OwnerTenantID = &value
		}
		if expiresAt.Valid {
			value := expiresAt.Time
			item.ExpiresAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateInviteCodeStatus(
	ctx context.Context,
	codeID int64,
	status string,
) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid invite code status")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE iam_invite_codes
		SET status=?
		WHERE id=?
	`, status, codeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateOwnInviteCodeStatus(
	ctx context.Context,
	userID int64,
	status string,
) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid invite code status")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE iam_invite_codes
		SET status=?
		WHERE owner_user_id=?
	`, status, userID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateInviteCodePolicy(
	ctx context.Context,
	codeID int64,
	status string,
	maxUses uint64,
	expiresAt *time.Time,
) error {
	if status != "active" && status != "disabled" {
		return fmt.Errorf("invalid invite code status")
	}

	var expiry any
	if expiresAt != nil {
		expiry = expiresAt.UTC()
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE iam_invite_codes
		SET status=?,
		    max_uses=?,
		    expires_at=?
		WHERE id=?
	`, status, maxUses, expiry, codeID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Store) ListInvitationRecords(
	ctx context.Context,
	actor model.Actor,
) ([]model.InvitationRecord, error) {
	query := `
		SELECT
			r.id,
			r.invite_code_id,
			c.code,
			r.inviter_user_id,
			r.inviter_tenant_id,
			inviter.username,
			inviter.display_name,
			r.referred_user_id,
			r.referred_tenant_id,
			referred.username,
			referred.display_name,
			r.source_type,
			r.parent_org_id,
			parent.name,
			r.bound_at
		FROM crm_registration_referrals r
		INNER JOIN iam_invite_codes c ON c.id=r.invite_code_id
		INNER JOIN mgmt_users inviter ON inviter.id=r.inviter_user_id
		INNER JOIN mgmt_users referred ON referred.id=r.referred_user_id
		INNER JOIN mgmt_tenants parent ON parent.id=r.parent_org_id
	`
	args := make([]any, 0)

	switch actor.Role {
	case "platform_admin":
		query += " ORDER BY r.id DESC"
	case "agent_admin":
		if actor.TenantID == nil {
			return []model.InvitationRecord{}, nil
		}
		query += " WHERE r.parent_org_id=? ORDER BY r.id DESC"
		args = append(args, *actor.TenantID)
	case "sales_staff", "customer":
		query += " WHERE r.inviter_user_id=? ORDER BY r.id DESC"
		args = append(args, actor.UserID)
	default:
		return []model.InvitationRecord{}, nil
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.InvitationRecord, 0)
	for rows.Next() {
		var item model.InvitationRecord
		var inviterTenantID sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.InviteCodeID,
			&item.InviteCode,
			&item.InviterUserID,
			&inviterTenantID,
			&item.InviterUsername,
			&item.InviterDisplayName,
			&item.ReferredUserID,
			&item.ReferredTenantID,
			&item.ReferredUsername,
			&item.ReferredDisplayName,
			&item.SourceType,
			&item.ParentOrgID,
			&item.ParentOrgName,
			&item.BoundAt,
		); err != nil {
			return nil, err
		}
		if inviterTenantID.Valid {
			value := inviterTenantID.Int64
			item.InviterTenantID = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) GetInvitationDashboard(
	ctx context.Context,
	actor model.Actor,
) (model.InvitationDashboard, error) {
	myCode, err := s.ensureUserInviteCode(ctx, actor.UserID, actor.TenantID, actor.Role)
	if err != nil {
		return model.InvitationDashboard{}, err
	}

	codes := []model.InviteCodeSummary{myCode}
	if actor.IsPlatformAdmin() {
		codes, err = s.ListInviteCodes(ctx)
		if err != nil {
			return model.InvitationDashboard{}, err
		}
	}

	records, err := s.ListInvitationRecords(ctx, actor)
	if err != nil {
		return model.InvitationDashboard{}, err
	}

	return model.InvitationDashboard{
		MyCode:  myCode,
		Codes:   codes,
		Records: records,
	}, nil
}

func (s *Store) RegisterCustomerByInvite(
	ctx context.Context,
	username string,
	displayName string,
	phone string,
	province string,
	city string,
	district string,
	passwordHash string,
	inviteCode string,
) (model.User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback()

	inviteCode = strings.ToUpper(strings.TrimSpace(inviteCode))

	var inviteID int64
	var inviterUserID int64
	var inviterTenantID sql.NullInt64
	var inviterRole string
	var status string
	var maxUses uint64
	var usedCount uint64
	var expiresAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT
			c.id,
			c.owner_user_id,
			c.owner_tenant_id,
			c.owner_role,
			c.status,
			c.max_uses,
			c.used_count,
			c.expires_at
		FROM iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		WHERE c.code=?
		  AND u.status='active'
		FOR UPDATE
	`, inviteCode).Scan(
		&inviteID,
		&inviterUserID,
		&inviterTenantID,
		&inviterRole,
		&status,
		&maxUses,
		&usedCount,
		&expiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrInviteCodeInvalid
		}
		return model.User{}, err
	}
	if status != "active" ||
		(maxUses > 0 && usedCount >= maxUses) ||
		(expiresAt.Valid && !expiresAt.Time.After(time.Now())) {
		return model.User{}, ErrInviteCodeInvalid
	}

	var parentOrgID int64
	var parentOrgType string
	var parentLevel int
	sourceType := "platform_invite"

	switch inviterRole {
	case "platform_admin":
		err = tx.QueryRowContext(ctx, `
			SELECT id, org_type, level
			FROM mgmt_tenants
			WHERE org_type='platform' AND status='active'
			ORDER BY id
			LIMIT 1
		`).Scan(&parentOrgID, &parentOrgType, &parentLevel)
	case "sales_staff":
		err = tx.QueryRowContext(ctx, `
			SELECT id, org_type, level
			FROM mgmt_tenants
			WHERE org_type='platform' AND status='active'
			ORDER BY id
			LIMIT 1
		`).Scan(&parentOrgID, &parentOrgType, &parentLevel)
		sourceType = "sales_invite"
	case "agent_admin":
		if !inviterTenantID.Valid {
			return model.User{}, ErrInviteCodeInvalid
		}
		err = tx.QueryRowContext(ctx, `
			SELECT id, org_type, level
			FROM mgmt_tenants
			WHERE id=? AND org_type='agent' AND status='active'
		`, inviterTenantID.Int64).Scan(&parentOrgID, &parentOrgType, &parentLevel)
		sourceType = "agent_invite"
	case "customer":
		if !inviterTenantID.Valid {
			return model.User{}, ErrInviteCodeInvalid
		}
		err = tx.QueryRowContext(ctx, `
			SELECT parent.id, parent.org_type, parent.level
			FROM mgmt_tenants customer
			INNER JOIN mgmt_tenants parent ON parent.id=customer.parent_id
			WHERE customer.id=?
			  AND customer.org_type='customer'
			  AND customer.status='active'
			  AND parent.status='active'
		`, inviterTenantID.Int64).Scan(&parentOrgID, &parentOrgType, &parentLevel)
		sourceType = "referral"
	default:
		return model.User{}, ErrInviteCodeInvalid
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.User{}, ErrInviteCodeInvalid
		}
		return model.User{}, err
	}
	if parentOrgType != "platform" && parentOrgType != "agent" {
		return model.User{}, ErrInviteCodeInvalid
	}

	tenantCode, err := randomTenantCode()
	if err != nil {
		return model.User{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (?, 'customer', ?, ?, ?, 'active')
	`, parentOrgID, parentLevel+1, tenantCode, strings.TrimSpace(displayName))
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	tenantID, err := result.LastInsertId()
	if err != nil {
		return model.User{}, err
	}
	result, err = tx.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id,
			username,
			password_hash,
			display_name,
			phone,
			province,
			city,
			district,
			role,
			status
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'customer', 'active')
	`,
		tenantID,
		strings.TrimSpace(username),
		passwordHash,
		strings.TrimSpace(displayName),
		strings.TrimSpace(phone),
		strings.TrimSpace(province),
		strings.TrimSpace(city),
		strings.TrimSpace(district),
	)
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.User{}, err
	}

	if err := ensureOrganizationResourceAccountsTx(ctx, tx, tenantID, "customer"); err != nil {
		return model.User{}, fmt.Errorf("invite registration resource accounts: %w", err)
	}

	customerTenantID := tenantID
	if err := ensureUserInviteCodeTx(
		ctx,
		tx,
		userID,
		&customerTenantID,
		"customer",
	); err != nil {
		return model.User{}, err
	}

	var commercialAgentOrgID sql.NullInt64
	if parentOrgType == "agent" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM crm_agent_orgs
			WHERE mgmt_tenant_id=?
			LIMIT 1
		`, parentOrgID).Scan(&commercialAgentOrgID); err != nil &&
			!errors.Is(err, sql.ErrNoRows) {
			return model.User{}, err
		}
	}

	var sourceSalesStaffID sql.NullInt64
	if inviterRole == "sales_staff" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM crm_sales_staff
			WHERE user_id=?
			  AND status='active'
			LIMIT 1
		`, inviterUserID).Scan(&sourceSalesStaffID); err != nil {
			return model.User{}, ErrInviteCodeInvalid
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO crm_customer_profiles (
			tenant_id,
			source_type,
			source_sales_staff_id,
			source_agent_org_id,
			source_note
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		tenantID,
		sourceType,
		sourceSalesStaffID,
		commercialAgentOrgID,
		"invite:"+inviteCode,
	)
	if err != nil {
		return model.User{}, err
	}

	if sourceSalesStaffID.Valid {
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
				'sales invite registration'
			)
		`, tenantID, sourceSalesStaffID.Int64, inviterUserID); err != nil {
			return model.User{}, err
		}
	}
	if commercialAgentOrgID.Valid {
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
			VALUES (?, ?, 'primary', 'active', CURRENT_TIMESTAMP(3), ?, 'invite registration')
		`, tenantID, commercialAgentOrgID.Int64, inviterUserID); err != nil {
			return model.User{}, err
		}
	}

	if inviterRole == "customer" && inviterTenantID.Valid {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO crm_referral_relations (
				referrer_tenant_id,
				referred_tenant_id,
				status,
				bound_at,
				created_by_user_id
			)
			VALUES (?, ?, 'active', CURRENT_TIMESTAMP(3), ?)
		`, inviterTenantID.Int64, tenantID, inviterUserID); err != nil {
			return model.User{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_registration_referrals (
			invite_code_id,
			inviter_user_id,
			inviter_tenant_id,
			referred_user_id,
			referred_tenant_id,
			source_type,
			parent_org_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		inviteID,
		inviterUserID,
		inviterTenantID,
		userID,
		tenantID,
		sourceType,
		parentOrgID,
	); err != nil {
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

	if _, err := tx.ExecContext(ctx, `
		UPDATE iam_invite_codes
		SET used_count=used_count+1
		WHERE id=?
	`, inviteID); err != nil {
		return model.User{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.User{}, err
	}

	return model.User{
		ID:           userID,
		TenantID:     &customerTenantID,
		Username:     strings.TrimSpace(username),
		PasswordHash: passwordHash,
		DisplayName:  strings.TrimSpace(displayName),
		Phone:        strings.TrimSpace(phone),
		Province:     strings.TrimSpace(province),
		City:         strings.TrimSpace(city),
		District:     strings.TrimSpace(district),
		Role:         "customer",
		Status:       "active",
		CreatedAt:    time.Now().UTC(),
	}, nil
}
func (s *Store) GetInvitePreview(
	ctx context.Context,
	code string,
) (model.InvitePreview, error) {
	code = strings.ToUpper(strings.TrimSpace(code))

	var item model.InvitePreview
	var maxUses uint64
	var usedCount uint64
	var expiresAt sql.NullTime
	err := s.db.QueryRowContext(ctx, `
		SELECT
			c.code,
			u.display_name,
			c.owner_role,
			c.max_uses,
			c.used_count,
			c.expires_at
		FROM iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		WHERE c.code=?
		  AND c.status='active'
		  AND u.status='active'
		LIMIT 1
	`, code).Scan(
		&item.Code,
		&item.InviterName,
		&item.InviterRole,
		&maxUses,
		&usedCount,
		&expiresAt,
	)
	if err != nil {
		return model.InvitePreview{}, ErrInviteCodeInvalid
	}
	if (maxUses > 0 && usedCount >= maxUses) ||
		(expiresAt.Valid && !expiresAt.Time.After(time.Now())) {
		return model.InvitePreview{}, ErrInviteCodeInvalid
	}

	switch item.InviterRole {
	case "platform_admin":
		item.SourceType = "platform_invite"
	case "agent_admin":
		item.SourceType = "agent_invite"
	case "sales_staff":
		item.SourceType = "sales_invite"
	case "customer":
		item.SourceType = "referral"
	default:
		return model.InvitePreview{}, ErrInviteCodeInvalid
	}
	return item, nil
}
