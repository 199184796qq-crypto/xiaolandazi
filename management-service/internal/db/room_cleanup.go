package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
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
	var existing int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM core_rooms WHERE tenant_id=? AND id=?", tenantID, roomID).Scan(&existing); err != nil {
		return err
	}
	if existing != 0 {
		return fmt.Errorf("room still exists; refusing to complete deletion")
	}

	// Stop durable runtime registrations without deleting any billing/event ledger.
	// Core is already stopped; no further wall-clock consumption may accrue here.
	if _, err := tx.ExecContext(ctx, `UPDATE live_runtime_sessions SET status='stopped', stop_reason='room_deleted', ended_at=COALESCE(ended_at, ?), last_billed_at=?, version=version+1, updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status IN ('running','paused')`, time.Now().UTC(), time.Now().UTC(), tenantID, roomID); err != nil {
		return fmt.Errorf("stop deleted room runtime: %w", err)
	}
	if err := cancelDeletedRoomQuotaLeases(ctx, tx, tenantID, roomID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, "UPDATE live_device_runtime_state SET current_room_id=NULL, work_status='idle', stop_reason='room_deleted', updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND current_room_id=?", tenantID, roomID); err != nil {
		return fmt.Errorf("release live device runtime state: %w", err)
	}
	if _, err := tx.ExecContext(ctx, "UPDATE live_device_room_bindings SET status='released', unbound_at=COALESCE(unbound_at, CURRENT_TIMESTAMP(3)), updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='active'", tenantID, roomID); err != nil {
		return fmt.Errorf("release live device room bindings: %w", err)
	}

	for _, statement := range []string{
		"UPDATE live_content_refresh_jobs SET status='cancelled',last_error='room_deleted',lease_token='',lease_until=NULL,next_attempt_at=CURRENT_TIMESTAMP(3),updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status IN ('pending','running','retry','ready')",
		"UPDATE live_agent_plan_room_bindings SET status='released', unbound_at=COALESCE(unbound_at, CURRENT_TIMESTAMP(3)), updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='active'",
		"UPDATE live_agent_room_bindings SET status='inactive', updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=?",
		"UPDATE live_support_authorizations SET status='revoked', revoked_at=COALESCE(revoked_at, CURRENT_TIMESTAMP(3)), updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='active'",
		"UPDATE live_support_requests SET status='cancelled', decision_note='直播间已永久删除', decided_at=COALESCE(decided_at, CURRENT_TIMESTAMP(3)), updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='pending'",
		"UPDATE live_policy_scopes SET status='archived', current_version_id=NULL, updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_agent_room_plan_selections WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_agent_room_plan_publications WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_room_interaction_preferences WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_room_addressing_preferences WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_room_human_behavior_profiles WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_content_policies WHERE tenant_id=? AND room_id=?",
		"DELETE FROM live_room_content_access WHERE tenant_id=? AND room_id=?",
	} {
		if _, err := tx.ExecContext(ctx, statement, tenantID, roomID); err != nil {
			return fmt.Errorf("cleanup room derived state: %w", err)
		}
	}

	// Semantic data is derived room state. Keep plan-level vectors intact because
	// the same plan may still be bound to other rooms.
	if _, err := tx.ExecContext(ctx, "DELETE FROM semantic_documents WHERE tenant_id=? AND room_id=?", tenantID, roomID); err != nil {
		return fmt.Errorf("delete room semantic documents: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE mgmt_user_ui_preferences SET selected_live_room_id=NULL WHERE selected_live_room_id=?`, roomID); err != nil {
		return err
	}
	return tx.Commit()
}

func cancelDeletedRoomQuotaLeases(ctx context.Context, tx *sql.Tx, tenantID, roomID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, runtime_session_id, core_working_start_seconds, allocated_seconds
		FROM live_quota_leases WHERE tenant_id=? AND room_id=? AND status IN ('reserved','active') ORDER BY id FOR UPDATE`, tenantID, roomID)
	if err != nil {
		return err
	}
	var leases []LiveQuotaLeaseGrant
	for rows.Next() {
		var lease LiveQuotaLeaseGrant
		if err := rows.Scan(&lease.ID, &lease.RuntimeSessionID, &lease.CoreWorkingStartSeconds, &lease.AllocatedSeconds); err != nil {
			rows.Close()
			return err
		}
		leases = append(leases, lease)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(leases) == 0 {
		return nil
	}
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return err
	}
	for _, lease := range leases {
		session := model.LiveRuntimeSession{ID: lease.RuntimeSessionID, TenantID: tenantID, RoomID: roomID}
		if _, err := settleLiveQuotaLeaseTx(ctx, tx, session, lease, 0, true, time.Now().UTC()); err != nil {
			return fmt.Errorf("release deleted room quota: %w", err)
		}
	}
	return nil
}
