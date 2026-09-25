package db

import (
	"context"
	"fmt"
)

// CleanupDeletedRoomDerivedState removes room-scoped operational/configuration
// state that should not outlive a deleted room. Historical billing/runtime
// ledgers and audit logs are intentionally retained for traceability.
func (s *Store) CleanupDeletedRoomDerivedState(ctx context.Context, tenantID, roomID int64) error {
	if tenantID <= 0 || roomID <= 0 {
		return fmt.Errorf("tenant_id and room_id must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "UPDATE live_device_runtime_state SET current_room_id=NULL, work_status='idle', stop_reason='room_deleted', updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND current_room_id=?", tenantID, roomID); err != nil {
		return fmt.Errorf("release live device runtime state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE live_device_room_bindings SET status='released', unbound_at=COALESCE(unbound_at, CURRENT_TIMESTAMP(3)), updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='active'", tenantID, roomID); err != nil {
		return fmt.Errorf("release live device room bindings: %w", err)
	}

	for _, statement := range []string{
		"DELETE FROM live_agent_plan_room_bindings WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_agent_room_bindings WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_support_authorizations WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_support_requests WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_support_config_versions WHERE tenant_id=? AND room_id=?",
	} {
		if _, err := tx.ExecContext(ctx, statement, tenantID, roomID); err != nil {
			return fmt.Errorf("cleanup room derived state: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM live_policy_versions WHERE policy_id IN (SELECT id FROM live_policy_scopes WHERE tenant_id=? AND room_id=?)", tenantID, roomID); err != nil {
		return fmt.Errorf("delete room policy versions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM live_policy_scopes WHERE tenant_id=? AND room_id=?", tenantID, roomID); err != nil {
		return fmt.Errorf("delete room policy scopes: %w", err)
	}

	return tx.Commit()
}
