package db

import (
	"context"
	"fmt"
)

// RoomHasActiveSession 判断房间是否有正在进行的直播场次。
// 缓存清理必须先问它：有在播场次时删掉正在使用的音频，会把声音打断。
func (s *Store) RoomHasActiveSession(ctx context.Context, roomID int64) (bool, error) {
	if roomID <= 0 {
		return false, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM live_runtime_sessions
		WHERE room_id = ? AND ended_at IS NULL
		  AND LOWER(status) IN ('running','paused','starting','start','resuming')`, roomID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查房间在播状态: %w", err)
	}
	return count > 0, nil
}

// TenantHasActiveSessions 判断租户下是否有任何在播场次，
// 删租户前用它挡住“正在直播还被清理”。
func (s *Store) TenantHasActiveSessions(ctx context.Context, tenantID int64) (bool, error) {
	if tenantID <= 0 {
		return false, nil
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM live_runtime_sessions
		WHERE tenant_id = ? AND ended_at IS NULL
		  AND LOWER(status) IN ('running','paused','starting','start','resuming')`, tenantID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("检查租户在播状态: %w", err)
	}
	return count > 0, nil
}
