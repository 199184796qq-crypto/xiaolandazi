package db

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) BeginAdminAudit(
	ctx context.Context,
	entry model.AdminAuditLog,
) (string, error) {
	if entry.OccurredAt.IsZero() {
		entry.OccurredAt = time.Now().UTC()
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mgmt_admin_audit_ledger (
			occurred_at,
			actor_user_id,
			actor_username,
			action,
			target_user_id,
			target_username,
			target_tenant_id,
			http_method,
			path,
			client_ip,
			result
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		entry.OccurredAt.UTC(),
		entry.ActorUserID,
		entry.ActorUsername,
		entry.Action,
		entry.TargetUserID,
		entry.TargetUsername,
		entry.TargetTenantID,
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
		SET actor_user_id=?,
		    actor_username=?,
		    action=?,
		    target_user_id=?,
		    target_username=?,
		    target_tenant_id=?,
		    http_method=?,
		    path=?,
		    client_ip=?,
		    result=?
		WHERE id=?
	`,
		entry.ActorUserID,
		entry.ActorUsername,
		entry.Action,
		entry.TargetUserID,
		entry.TargetUsername,
		entry.TargetTenantID,
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
			id,
			occurred_at,
			actor_user_id,
			actor_username,
			action,
			target_user_id,
			target_username,
			target_tenant_id,
			http_method,
			path,
			client_ip,
			result
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
			&id,
			&item.OccurredAt,
			&item.ActorUserID,
			&item.ActorUsername,
			&item.Action,
			&item.TargetUserID,
			&item.TargetUsername,
			&item.TargetTenantID,
			&item.HTTPMethod,
			&item.Path,
			&item.ClientIP,
			&item.Result,
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

func (s *Store) DBStats() sql.DBStats {
	return s.db.Stats()
}
