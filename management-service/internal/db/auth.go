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

	if _, err := s.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS mgmt_sessions (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id BIGINT UNSIGNED NOT NULL,
			token_hash CHAR(64) NOT NULL,
			expires_at DATETIME(3) NOT NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_mgmt_sessions_token (token_hash),
			KEY idx_mgmt_sessions_user (user_id),
			KEY idx_mgmt_sessions_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
	`); err != nil {
		return fmt.Errorf("create sessions table: %w", err)
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
		SELECT id, tenant_id, username, password_hash, display_name, role, status, created_at
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
		SELECT id, tenant_id, username, password_hash, display_name, role, status, created_at
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
			u.role,
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
		&item.Role,
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
) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_sessions (
			user_id,
			token_hash,
			expires_at
		)
		VALUES (?, ?, ?)
	`, userID, tokenHash, expiresAt.UTC())
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
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
		SET password_hash = ?
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
	passwordHash string,
) (model.User, error) {
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
		INSERT INTO mgmt_tenants (code, name, status)
		VALUES (?, ?, 'active')
	`, tenantCode, displayName)
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
			role,
			status
		)
		VALUES (?, ?, ?, ?, 'customer', 'active')
	`, tenantID, username, passwordHash, displayName)
	if err != nil {
		return model.User{}, normalizeDuplicate(err)
	}
	userID, err := result.LastInsertId()
	if err != nil {
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
		&item.Role,
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
