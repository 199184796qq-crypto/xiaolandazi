package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-sql-driver/mysql"

	"livecompanion/core/internal/config"
)

func Open(cfg config.Config) (*sql.DB, error) {
	mysqlCfg := mysql.Config{
		User:      cfg.DBUser,
		Passwd:    cfg.DBPassword,
		Net:       "tcp",
		Addr:      net.JoinHostPort(cfg.DBHost, cfg.DBPort),
		DBName:    cfg.DBName,
		ParseTime: true,
		Loc:       time.UTC,
		Params: map[string]string{
			"charset": "utf8mb4",
		},
	}
	dsn := mysqlCfg.FormatDSN()

	database, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	database.SetMaxOpenConns(30)
	database.SetMaxIdleConns(10)
	database.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := database.PingContext(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return database, nil
}

func Migrate(ctx context.Context, database *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS core_rooms (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			tenant_id BIGINT UNSIGNED NOT NULL,
			platform VARCHAR(32) NOT NULL,
			external_room_id VARCHAR(128) NOT NULL,
			name VARCHAR(128) NOT NULL DEFAULT '',
			status VARCHAR(32) NOT NULL DEFAULT 'pending',
			collector_mode VARCHAR(32) NOT NULL DEFAULT 'auto',
			monitor_enabled TINYINT(1) NOT NULL DEFAULT 0,
			device_online TINYINT(1) NOT NULL DEFAULT 0,
			online_count INT UNSIGNED NOT NULL DEFAULT 0,
			last_event_at DATETIME(3) NULL,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_core_rooms_tenant_platform_external (tenant_id, platform, external_room_id),
			KEY idx_core_rooms_tenant_status (tenant_id, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	}

	for _, statement := range statements {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply core schema: %w", err)
		}
	}

	if _, err := database.ExecContext(ctx, "ALTER TABLE core_rooms ADD COLUMN source_url TEXT NOT NULL AFTER external_room_id"); err != nil {
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1060 {
			return fmt.Errorf("add core_rooms.source_url: %w", err)
		}
	}

	if _, err := database.ExecContext(
		ctx,
		"ALTER TABLE core_rooms ADD COLUMN monitor_enabled TINYINT(1) NOT NULL DEFAULT 0 AFTER collector_mode",
	); err != nil {
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1060 {
			return fmt.Errorf("add core_rooms.monitor_enabled: %w", err)
		}
	}

	if _, err := database.ExecContext(
		ctx,
		"ALTER TABLE core_rooms ADD COLUMN device_online TINYINT(1) NOT NULL DEFAULT 0 AFTER monitor_enabled",
	); err != nil {
		var mysqlErr *mysql.MySQLError
		if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1060 {
			return fmt.Errorf("add core_rooms.device_online: %w", err)
		}
	}

	if _, err := database.ExecContext(
		ctx,
		"ALTER TABLE core_rooms MODIFY COLUMN monitor_enabled TINYINT(1) NOT NULL DEFAULT 0",
	); err != nil {
		return fmt.Errorf("set core_rooms.monitor_enabled default: %w", err)
	}

	if _, err := database.ExecContext(
		ctx,
		"ALTER TABLE core_rooms MODIFY COLUMN device_online TINYINT(1) NOT NULL DEFAULT 0",
	); err != nil {
		return fmt.Errorf("set core_rooms.device_online default: %w", err)
	}

	if _, err := database.ExecContext(
		ctx,
		"DROP TABLE IF EXISTS core_room_events",
	); err != nil {
		return fmt.Errorf("drop legacy core_room_events: %w", err)
	}

	return nil
}
