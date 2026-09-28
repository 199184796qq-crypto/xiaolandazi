package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) MigrateAdminAudit(ctx context.Context) error {
	columns := []struct {
		name string
		sql  string
	}{
		{"actor_role", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN actor_role VARCHAR(32) NOT NULL DEFAULT '' AFTER actor_username"},
		{"actor_type", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN actor_type VARCHAR(32) NOT NULL DEFAULT 'user' AFTER actor_role"},
		{"source", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN source VARCHAR(64) NOT NULL DEFAULT '' AFTER actor_type"},
		{"target_room_id", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN target_room_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER target_tenant_id"},
		{"object_type", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN object_type VARCHAR(64) NOT NULL DEFAULT '' AFTER target_room_id"},
		{"object_id", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN object_id VARCHAR(128) NOT NULL DEFAULT '' AFTER object_type"},
		{"object_name", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN object_name VARCHAR(255) NOT NULL DEFAULT '' AFTER object_id"},
		{"reason", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN reason VARCHAR(255) NOT NULL DEFAULT '' AFTER object_name"},
		{"before_state", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN before_state MEDIUMTEXT NULL AFTER reason"},
		{"after_state", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN after_state MEDIUMTEXT NULL AFTER before_state"},
		{"runtime_session_id", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN runtime_session_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER after_state"},
		{"core_boot_id", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN core_boot_id VARCHAR(128) NOT NULL DEFAULT '' AFTER runtime_session_id"},
		{"request_id", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN request_id VARCHAR(128) NOT NULL DEFAULT '' AFTER core_boot_id"},
		{"detail_json", "ALTER TABLE mgmt_admin_audit_ledger ADD COLUMN detail_json MEDIUMTEXT NULL AFTER request_id"},
	}
	for _, column := range columns {
		exists, err := s.columnExists(ctx, "mgmt_admin_audit_ledger", column.name)
		if err != nil {
			return fmt.Errorf("check audit column %s: %w", column.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
			return fmt.Errorf("add audit column %s: %w", column.name, err)
		}
	}
	indexes := []struct {
		name string
		sql  string
	}{
		{"idx_admin_audit_target_room", "ALTER TABLE mgmt_admin_audit_ledger ADD KEY idx_admin_audit_target_room (target_room_id, occurred_at)"},
		{"idx_admin_audit_object", "ALTER TABLE mgmt_admin_audit_ledger ADD KEY idx_admin_audit_object (object_type, object_id, occurred_at)"},
		{"idx_admin_audit_source", "ALTER TABLE mgmt_admin_audit_ledger ADD KEY idx_admin_audit_source (source, occurred_at)"},
	}
	for _, index := range indexes {
		exists, err := s.indexExists(ctx, "mgmt_admin_audit_ledger", index.name)
		if err != nil {
			return fmt.Errorf("check audit index %s: %w", index.name, err)
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx, index.sql); err != nil {
			return fmt.Errorf("add audit index %s: %w", index.name, err)
		}
	}
	return nil
}

func (s *Store) BeginAdminAudit(
	ctx context.Context,
	entry model.AdminAuditLog,
) (string, error) {
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_admin_audit_ledger (
			occurred_at, actor_user_id, actor_username, actor_role, actor_type, source,
			action, target_user_id, target_username, target_tenant_id, target_room_id,
			object_type, object_id, object_name, reason, before_state, after_state,
			runtime_session_id, core_boot_id, request_id, detail_json,
			http_method, path, client_ip, result
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.OccurredAt.UTC(),
		entry.ActorUserID,
		entry.ActorUsername,
		entry.ActorRole,
		entry.ActorType,
		entry.Source,
		entry.Action,
		entry.TargetUserID,
		entry.TargetUsername,
		entry.TargetTenantID,
		entry.TargetRoomID,
		entry.ObjectType,
		entry.ObjectID,
		entry.ObjectName,
		entry.Reason,
		nullableAuditString(entry.BeforeState),
		nullableAuditString(entry.AfterState),
		entry.RuntimeSessionID,
		entry.CoreBootID,
		entry.RequestID,
		nullableAuditString(entry.DetailJSON),
		entry.HTTPMethod,
		entry.Path,
		entry.ClientIP,
		entry.Result,
	)
	if err != nil {
		return "", fmt.Errorf("insert admin audit ledger: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return "", fmt.Errorf("read admin audit id: %w", err)
	}
	return strconv.FormatInt(id, 10), nil
}

func (s *Store) CompleteAdminAudit(
	ctx context.Context,
	id string,
	entry model.AdminAuditLog,
) error {
	parsedID, err := strconv.ParseInt(id, 10, 64)
	if err != nil || parsedID <= 0 {
		return fmt.Errorf("invalid admin audit id")
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE mgmt_admin_audit_ledger
		SET actor_user_id=?, actor_username=?, actor_role=?, actor_type=?, source=?,
		    action=?, target_user_id=?, target_username=?, target_tenant_id=?, target_room_id=?,
		    object_type=?, object_id=?, object_name=?, reason=?, before_state=?, after_state=?,
		    runtime_session_id=?, core_boot_id=?, request_id=?, detail_json=?,
		    http_method=?, path=?, client_ip=?, result=?
		WHERE id=?
	`,
		entry.ActorUserID,
		entry.ActorUsername,
		entry.ActorRole,
		entry.ActorType,
		entry.Source,
		entry.Action,
		entry.TargetUserID,
		entry.TargetUsername,
		entry.TargetTenantID,
		entry.TargetRoomID,
		entry.ObjectType,
		entry.ObjectID,
		entry.ObjectName,
		entry.Reason,
		nullableAuditString(entry.BeforeState),
		nullableAuditString(entry.AfterState),
		entry.RuntimeSessionID,
		entry.CoreBootID,
		entry.RequestID,
		nullableAuditString(entry.DetailJSON),
		entry.HTTPMethod,
		entry.Path,
		entry.ClientIP,
		entry.Result,
		parsedID,
	)
	if err != nil {
		return fmt.Errorf("update admin audit ledger: %w", err)
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

func (s *Store) ListAdminAudits(
	ctx context.Context,
	limit int64,
) ([]model.AdminAuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, occurred_at, actor_user_id, actor_username, actor_role, actor_type, source,
			action, target_user_id, target_username, target_tenant_id, target_room_id,
			object_type, object_id, object_name, reason,
			COALESCE(before_state,''), COALESCE(after_state,''), runtime_session_id,
			core_boot_id, request_id, COALESCE(detail_json,''),
			http_method, path, client_ip, result
		FROM mgmt_admin_audit_ledger
		ORDER BY id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminAuditLog, 0)
	for rows.Next() {
		var item model.AdminAuditLog
		var id int64
		if err := rows.Scan(
			&id, &item.OccurredAt, &item.ActorUserID, &item.ActorUsername,
			&item.ActorRole, &item.ActorType, &item.Source, &item.Action,
			&item.TargetUserID, &item.TargetUsername, &item.TargetTenantID, &item.TargetRoomID,
			&item.ObjectType, &item.ObjectID, &item.ObjectName, &item.Reason,
			&item.BeforeState, &item.AfterState, &item.RuntimeSessionID,
			&item.CoreBootID, &item.RequestID, &item.DetailJSON,
			&item.HTTPMethod, &item.Path, &item.ClientIP, &item.Result,
		); err != nil {
			return nil, err
		}
		item.ID = strconv.FormatInt(id, 10)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func nullableAuditString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) DBStats() sql.DBStats {
	return s.db.Stats()
}
