package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/model"
)

func (s *Store) MigrateAuth(ctx context.Context) error {
	hasPasswordHash, err := s.columnExists(ctx, "mgmt_users", "password_hash")
	if err != nil {
		return err
	}
	if !hasPasswordHash {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE mgmt_users
			ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT '' AFTER username
		`); err != nil {
			return fmt.Errorf("add password_hash: %w", err)
		}
	}

	hasMustChangePassword, err := s.columnExists(ctx, "mgmt_users", "must_change_password")
	if err != nil {
		return err
	}
	if !hasMustChangePassword {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE mgmt_users
			ADD COLUMN must_change_password TINYINT(1) NOT NULL DEFAULT 0 AFTER password_hash
		`); err != nil {
			return fmt.Errorf("add must_change_password: %w", err)
		}

		if _, err := s.db.ExecContext(ctx, `
			UPDATE mgmt_users u
			INNER JOIN mgmt_tenants customer_org
				ON customer_org.id = u.tenant_id
			   AND customer_org.org_type = 'customer'
			INNER JOIN mgmt_tenants agent_org
				ON agent_org.id = customer_org.parent_id
			   AND agent_org.org_type = 'agent'
			SET u.must_change_password = 1
			WHERE u.role = 'customer'
		`); err != nil {
			return fmt.Errorf("backfill agent customer password change flag: %w", err)
		}
	}

	contactColumns := []struct {
		name string
		sql  string
	}{
		{"phone", "ALTER TABLE mgmt_users ADD COLUMN phone VARCHAR(32) NOT NULL DEFAULT '' AFTER display_name"},
		{"email", "ALTER TABLE mgmt_users ADD COLUMN email VARCHAR(254) NOT NULL DEFAULT '' AFTER phone"},
		{"qq", "ALTER TABLE mgmt_users ADD COLUMN qq VARCHAR(32) NOT NULL DEFAULT '' AFTER email"},
		{"wechat", "ALTER TABLE mgmt_users ADD COLUMN wechat VARCHAR(64) NOT NULL DEFAULT '' AFTER qq"},
		{"province", "ALTER TABLE mgmt_users ADD COLUMN province VARCHAR(64) NOT NULL DEFAULT '' AFTER wechat"},
		{"city", "ALTER TABLE mgmt_users ADD COLUMN city VARCHAR(64) NOT NULL DEFAULT '' AFTER province"},
		{"district", "ALTER TABLE mgmt_users ADD COLUMN district VARCHAR(64) NOT NULL DEFAULT '' AFTER city"},
		{"address", "ALTER TABLE mgmt_users ADD COLUMN address VARCHAR(255) NOT NULL DEFAULT '' AFTER district"},
	}
	for _, column := range contactColumns {
		exists, err := s.columnExists(ctx, "mgmt_users", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add mgmt_users.%s: %w", column.name, err)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `
		ALTER TABLE mgmt_users
		MODIFY COLUMN phone VARCHAR(128) NOT NULL DEFAULT ''
	`); err != nil {
		return fmt.Errorf("widen mgmt_users.phone: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mgmt_sessions (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id BIGINT UNSIGNED NOT NULL,
			token_hash CHAR(64) NOT NULL,
			expires_at DATETIME(3) NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			client_ip VARCHAR(64) NOT NULL DEFAULT '',
			user_agent VARCHAR(512) NOT NULL DEFAULT '',
			PRIMARY KEY (id),
			UNIQUE KEY uk_mgmt_sessions_token (token_hash),
			KEY idx_mgmt_sessions_user (user_id),
			KEY idx_mgmt_sessions_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
	`); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
	}

	sessionColumns := []struct {
		name string
		sql  string
	}{
		{"client_ip", "ALTER TABLE mgmt_sessions ADD COLUMN client_ip VARCHAR(64) NOT NULL DEFAULT '' AFTER last_seen_at"},
		{"user_agent", "ALTER TABLE mgmt_sessions ADD COLUMN user_agent VARCHAR(512) NOT NULL DEFAULT '' AFTER client_ip"},
	}
	for _, column := range sessionColumns {
		exists, err := s.columnExists(ctx, "mgmt_sessions", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add mgmt_sessions.%s: %w", column.name, err)
			}
		}
	}

	return nil
}

func (s *Store) columnExists(
	ctx context.Context,
	tableName string,
	columnName string,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = ?
		  AND COLUMN_NAME = ?
	`, tableName, columnName).Scan(&count)
	return count > 0, err
}

func (s *Store) EnsureDefaultAdmin(
	ctx context.Context,
	username string,
	displayName string,
	passwordHash string,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_users (
			tenant_id,
			username,
			password_hash,
			display_name,
			role,
			status
		)
		VALUES (NULL, ?, ?, ?, 'platform_admin', 'active')
		ON DUPLICATE KEY UPDATE
			display_name = VALUES(display_name),
			role = 'platform_admin',
			status = 'active'
	`, username, passwordHash, displayName)
	if err != nil {
		return fmt.Errorf("seed default admin: %w", err)
	}
	return nil
}

func (s *Store) GetUserByUsername(
	ctx context.Context,
	username string,
) (model.User, error) {
	return s.queryUser(ctx, `
		SELECT id, tenant_id, username, password_hash, display_name, avatar_url, role, must_change_password, phone, province, city, district, status, created_at
		FROM mgmt_users
		WHERE username = ?
		LIMIT 1
	`, strings.TrimSpace(username))
}

func (s *Store) GetUserByID(
	ctx context.Context,
	userID int64,
) (model.User, error) {
	return s.queryUser(ctx, `
		SELECT id, tenant_id, username, password_hash, display_name, avatar_url, role, must_change_password, phone, province, city, district, status, created_at
		FROM mgmt_users
		WHERE id = ?
		LIMIT 1
	`, userID)
}

func (s *Store) ResolveSession(
	ctx context.Context,
	tokenHash string,
	now time.Time,
) (model.User, error) {
	var item model.User
	var tenantID sql.NullInt64

	err := s.db.QueryRowContext(ctx, `
		SELECT
			u.id,
			u.tenant_id,
			u.username,
			u.password_hash,
			u.display_name,
			u.avatar_url,
			u.role,
			u.must_change_password,
			u.phone,
			u.province,
			u.city,
			u.district,
			u.status,
			u.created_at
		FROM mgmt_sessions s
		INNER JOIN mgmt_users u ON u.id = s.user_id
		WHERE s.token_hash = ?
		  AND s.expires_at > ?
		  AND u.status = 'active'
		LIMIT 1
	`, tokenHash, now.UTC()).Scan(
		&item.ID,
		&tenantID,
		&item.Username,
		&item.PasswordHash,
		&item.DisplayName,
		&item.AvatarURL,
		&item.Role,
		&item.MustChangePassword,
		&item.Phone,
		&item.Province,
		&item.City,
		&item.District,
		&item.Status,
		&item.CreatedAt,
	)
	if err != nil {
		return model.User{}, err
	}
	if tenantID.Valid {
		value := tenantID.Int64
		item.TenantID = &value
	}

	_, _ = s.db.ExecContext(ctx, `
		UPDATE mgmt_sessions
		SET last_seen_at = ?
		WHERE token_hash = ?
	`, now.UTC(), tokenHash)

	return item, nil
}

func (s *Store) CreateSession(
	ctx context.Context,
	userID int64,
	tokenHash string,
	expiresAt time.Time,
	clientIP string,
	userAgent string,
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_sessions (
			user_id,
			token_hash,
			expires_at,
			client_ip,
			user_agent
		)
		VALUES (?, ?, ?, ?, ?)
	`, userID, tokenHash, expiresAt.UTC(), strings.TrimSpace(clientIP), strings.TrimSpace(userAgent))
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (s *Store) ListUserSessions(
	ctx context.Context,
	userID int64,
	currentTokenHash string,
) ([]model.AuthSessionSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, token_hash, client_ip, user_agent, created_at, last_seen_at, expires_at
		FROM mgmt_sessions
		WHERE user_id=? AND expires_at>?
		ORDER BY last_seen_at DESC, id DESC
	`, userID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AuthSessionSummary, 0)
	for rows.Next() {
		var item model.AuthSessionSummary
		var tokenHash string
		if err := rows.Scan(&item.ID, &tokenHash, &item.ClientIP, &item.UserAgent, &item.CreatedAt, &item.LastSeenAt, &item.ExpiresAt); err != nil {
			return nil, err
		}
		item.Current = tokenHash == currentTokenHash
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) DeleteOtherUserSessions(ctx context.Context, userID int64, currentTokenHash string) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM mgmt_sessions
		WHERE user_id=? AND token_hash<>?
	`, userID, currentTokenHash)
	return err
}

func (s *Store) DeleteSession(
	ctx context.Context,
	tokenHash string,
) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM mgmt_sessions
		WHERE token_hash = ?
	`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(
	ctx context.Context,
	userID int64,
) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM mgmt_sessions
		WHERE user_id = ?
	`, userID)
	return err
}

func (s *Store) DeleteExpiredSessions(
	ctx context.Context,
	now time.Time,
) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM mgmt_sessions
		WHERE expires_at <= ?
	`, now.UTC())
	return err
}

func (s *Store) UpdatePassword(
	ctx context.Context,
	userID int64,
	passwordHash string,
) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_users
		SET password_hash = ?,
		    must_change_password = 0
		WHERE id = ? AND status = 'active'
	`, passwordHash, userID)
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

func (s *Store) RegisterCustomer(
	ctx context.Context,
	username string,
	displayName string,
	phone string,
	passwordHash string,
) (model.User, error) {
	platformID, err := s.PlatformOrganizationID(ctx)
	if err != nil {
		return model.User{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.User{}, err
	}
	defer tx.Rollback()

	tenantCode, err := randomTenantCode()
	if err != nil {
		return model.User{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (
			parent_id, org_type, level, code, name, status
		)
		VALUES (?, 'customer', 1, ?, ?, 'active')
	`, platformID, tenantCode, displayName)
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
			role,
			status
		)
		VALUES (?, ?, ?, ?, ?, 'customer', 'active')
	`, tenantID, username, passwordHash, displayName, strings.TrimSpace(phone))
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
		return model.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_customer_profiles (tenant_id, source_type)
		VALUES (?, 'direct')
	`, tenantID); err != nil {
		return model.User{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_accounts (tenant_id, account_type, currency, balance_cents, status)
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
		ID:           userID,
		TenantID:     &tenantID,
		Username:     username,
		PasswordHash: passwordHash,
		DisplayName:  displayName,
		Phone:        strings.TrimSpace(phone),
		Role:         "customer",
		Status:       "active",
		CreatedAt:    time.Now().UTC(),
	}, nil
}

func (s *Store) queryUser(
	ctx context.Context,
	query string,
	args ...any,
) (model.User, error) {
	var item model.User
	var tenantID sql.NullInt64

	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&item.ID,
		&tenantID,
		&item.Username,
		&item.PasswordHash,
		&item.DisplayName,
		&item.AvatarURL,
		&item.Role,
		&item.MustChangePassword,
		&item.Phone,
		&item.Province,
		&item.City,
		&item.District,
		&item.Status,
		&item.CreatedAt,
	)
	if err != nil {
		return model.User{}, err
	}
	if tenantID.Valid {
		value := tenantID.Int64
		item.TenantID = &value
	}
	return item, nil
}

func randomTenantCode() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "cust_" + hex.EncodeToString(raw[:]), nil
}

func normalizeDuplicate(err error) error {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("duplicate: %w", err)
	}
	return err
}
