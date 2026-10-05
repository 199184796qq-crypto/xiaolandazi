package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// RoomDeletion is a durable cleanup job and minimal historical room identity.
// It is deliberately not a restorable copy of a live room.
type RoomDeletion struct {
	TenantID int64
	RoomID   int64
}

func (s *Store) MigrateRoomDeletions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS mgmt_room_deletions (
		tenant_id BIGINT UNSIGNED NOT NULL,
		room_id BIGINT UNSIGNED NOT NULL,
		actor_user_id BIGINT UNSIGNED NOT NULL,
		room_name VARCHAR(128) NOT NULL DEFAULT '',
		platform VARCHAR(32) NOT NULL DEFAULT '',
		external_room_id VARCHAR(128) NOT NULL DEFAULT '',
		status VARCHAR(24) NOT NULL DEFAULT 'pending',
		attempts INT UNSIGNED NOT NULL DEFAULT 0,
		last_error VARCHAR(1024) NOT NULL DEFAULT '',
		requested_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		completed_at DATETIME(3) NULL,
		PRIMARY KEY (tenant_id, room_id),
		KEY idx_room_deletion_pending (status, updated_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	return err
}

func (s *Store) QueueRoomDeletion(ctx context.Context, tenantID, roomID, actorID int64) error {
	if tenantID <= 0 || roomID <= 0 || actorID <= 0 {
		return fmt.Errorf("invalid room deletion identity")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO mgmt_room_deletions
		(tenant_id, room_id, actor_user_id, room_name, platform, external_room_id)
		SELECT tenant_id, id, ?, name, platform, external_room_id FROM core_rooms WHERE tenant_id=? AND id=?
		ON DUPLICATE KEY UPDATE room_id=VALUES(room_id)`, actorID, tenantID, roomID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_room_deletions WHERE tenant_id=? AND room_id=?`, tenantID, roomID).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return sql.ErrNoRows
		}
	}
	return nil
}

func (s *Store) PendingRoomDeletions(ctx context.Context) ([]RoomDeletion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id, room_id FROM mgmt_room_deletions WHERE status='pending' ORDER BY updated_at, room_id LIMIT 25`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []RoomDeletion{}
	for rows.Next() {
		var item RoomDeletion
		if err := rows.Scan(&item.TenantID, &item.RoomID); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) RecordRoomDeletionFailure(ctx context.Context, tenantID, roomID int64, failure error) error {
	detail := []rune(failure.Error())
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE mgmt_room_deletions SET attempts=attempts+1, last_error=?, updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=? AND status='pending'`, string(detail), tenantID, roomID)
	return err
}

func (s *Store) IsRoomDeletionRequested(ctx context.Context, tenantID, roomID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_room_deletions WHERE tenant_id=? AND room_id=?`, tenantID, roomID).Scan(&count)
	return count > 0, err
}

func (s *Store) CompleteRoomDeletion(ctx context.Context, tenantID, roomID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mgmt_room_deletions SET status='completed', completed_at=COALESCE(completed_at, CURRENT_TIMESTAMP(3)), last_error='', updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND room_id=?`, tenantID, roomID)
	return err
}

// Acquiring the room row prevents a concurrent hard delete from leaving fresh
// device bindings or runtime registrations after cleanup has already finished.
func lockUsableRoomTx(ctx context.Context, tx *sql.Tx, tenantID, roomID int64) error {
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM core_rooms WHERE tenant_id=? AND id=? FOR UPDATE`, tenantID, roomID).Scan(&id); err != nil {
		return err
	}
	var deleting int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mgmt_room_deletions WHERE tenant_id=? AND room_id=?`, tenantID, roomID).Scan(&deleting); err != nil {
		return err
	}
	if deleting > 0 {
		return fmt.Errorf("直播间已进入永久删除流程")
	}
	return nil
}

// ResolveRoomTenantForDeletion works while Core is unavailable and for retries
// after the live row is gone. Its caller must first establish cross-tenant access.
func (s *Store) ResolveRoomTenantForDeletion(ctx context.Context, roomID int64) (int64, error) {
	var tenantID int64
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM core_rooms WHERE id=? UNION ALL SELECT tenant_id FROM mgmt_room_deletions WHERE room_id=? LIMIT 1`, roomID, roomID).Scan(&tenantID)
	return tenantID, err
}

func (s *Store) RequestedRoomDeletionIDs(ctx context.Context, roomIDs []int64) (map[int64]bool, error) {
	result := map[int64]bool{}
	if len(roomIDs) == 0 {
		return result, nil
	}
	args := make([]any, 0, len(roomIDs))
	marks := make([]string, 0, len(roomIDs))
	for _, id := range roomIDs {
		args = append(args, id)
		marks = append(marks, "?")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT room_id FROM mgmt_room_deletions WHERE room_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = true
	}
	return result, rows.Err()
}
