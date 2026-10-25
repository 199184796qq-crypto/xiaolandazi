package db

import (
	"context"
	"database/sql"
	"errors"

	"livecompanion/management/internal/model"
)

// CurrentLiveRoomCost returns the latest rolling-hour cost row for one room.
// A missing row means the reporter has not produced a snapshot yet; callers
// should treat it as unknown, not as zero cost.
func (s *Store) CurrentLiveRoomCost(ctx context.Context, tenantID, roomID int64) (model.LiveRoomCost, error) {
	var item model.LiveRoomCost
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id, room_id, window_start, window_end,
		       model_cost_micros, tts_cost_micros, total_cost_micros
		FROM live_cost_ledger
		WHERE tenant_id=? AND room_id=?
		ORDER BY id DESC LIMIT 1
	`, tenantID, roomID).Scan(
		&item.TenantID, &item.RoomID, &item.WindowStart, &item.WindowEnd,
		&item.ModelCostMicros, &item.TTSCostMicros, &item.TotalCostMicros,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	return item, err
}
