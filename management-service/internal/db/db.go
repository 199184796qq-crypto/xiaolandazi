package db

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"

	"livecompanion/management/internal/config"
	"livecompanion/management/internal/model"
)

type Store struct {
	db *sql.DB
}

func Open(cfg config.Config) (*Store, error) {
	mysqlCfg := mysql.NewConfig()
	mysqlCfg.User = cfg.DBUser
	mysqlCfg.Passwd = cfg.DBPassword
	mysqlCfg.Net = "tcp"
	mysqlCfg.Addr = net.JoinHostPort(cfg.DBHost, cfg.DBPort)
	mysqlCfg.DBName = cfg.DBName
	mysqlCfg.ParseTime = true
	mysqlCfg.Loc = time.UTC
	mysqlCfg.Timeout = 3 * time.Second
	mysqlCfg.ReadTimeout = 5 * time.Second
	mysqlCfg.WriteTimeout = 5 * time.Second
	mysqlCfg.Params = map[string]string{
		"charset": "utf8mb4",
	}
	dsn := mysqlCfg.FormatDSN()

	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	database.SetMaxOpenConns(cfg.DBMaxOpenConns)
	database.SetMaxIdleConns(cfg.DBMaxIdleConns)
	database.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &Store{db: database}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS mgmt_tenants (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			code VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_mgmt_tenants_code (code)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS mgmt_users (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			tenant_id BIGINT UNSIGNED NULL,
			username VARCHAR(128) NOT NULL,
			display_name VARCHAR(128) NOT NULL,
			role VARCHAR(32) NOT NULL,
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_mgmt_users_username (username),
			KEY idx_mgmt_users_tenant (tenant_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS mgmt_admin_audit_ledger (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			occurred_at DATETIME(3) NOT NULL,
			actor_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			actor_username VARCHAR(128) NOT NULL DEFAULT '',
			action VARCHAR(160) NOT NULL,
			target_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			target_username VARCHAR(128) NOT NULL DEFAULT '',
			target_tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			http_method VARCHAR(16) NOT NULL DEFAULT '',
			path VARCHAR(512) NOT NULL DEFAULT '',
			client_ip VARCHAR(64) NOT NULL DEFAULT '',
			result VARCHAR(64) NOT NULL DEFAULT '',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			KEY idx_admin_audit_occurred (occurred_at, id),
			KEY idx_admin_audit_actor (actor_user_id, occurred_at),
			KEY idx_admin_audit_target_tenant (target_tenant_id, occurred_at),
			KEY idx_admin_audit_action (action, occurred_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply management schema: %w", err)
		}
	}
	return nil
}

func (s *Store) EnsureDevelopmentSeed(
	ctx context.Context,
	code string,
	name string,
) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_tenants (code, name, status)
		VALUES (?, ?, 'active')
		ON DUPLICATE KEY UPDATE name = VALUES(name), status = 'active'
	`, code, name); err != nil {
		return 0, fmt.Errorf("seed tenant: %w", err)
	}

	var tenantID int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT id FROM mgmt_tenants WHERE code = ?
	`, code).Scan(&tenantID); err != nil {
		return 0, fmt.Errorf("load dev tenant: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_users (tenant_id, username, display_name, role, status)
		VALUES (NULL, 'platform-admin', '超级系统管理员', 'platform_admin', 'active')
		ON DUPLICATE KEY UPDATE
			display_name = VALUES(display_name),
			role = VALUES(role),
			status = 'active'
	`); err != nil {
		return 0, fmt.Errorf("seed platform admin: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_users (tenant_id, username, display_name, role, status)
		VALUES (?, 'demo-customer', ?, 'customer', 'active')
		ON DUPLICATE KEY UPDATE
			tenant_id = VALUES(tenant_id),
			display_name = VALUES(display_name),
			role = VALUES(role),
			status = 'active'
	`, tenantID, name); err != nil {
		return 0, fmt.Errorf("seed customer: %w", err)
	}

	return tenantID, nil
}

func (s *Store) ListTenants(ctx context.Context) ([]model.Tenant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, name, status, created_at
		FROM mgmt_tenants
		WHERE status = 'active'
		ORDER BY id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Tenant, 0)
	for rows.Next() {
		var item model.Tenant
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *Store) GetTenant(ctx context.Context, tenantID int64) (model.Tenant, error) {
	var item model.Tenant
	err := s.db.QueryRowContext(ctx, `
		SELECT id, code, name, status, created_at
		FROM mgmt_tenants
		WHERE id = ? AND status = 'active'
	`, tenantID).Scan(
		&item.ID,
		&item.Code,
		&item.Name,
		&item.Status,
		&item.CreatedAt,
	)
	return item, err
}
