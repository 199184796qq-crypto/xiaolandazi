package db

import (
	"context"
	"database/sql"
	"errors"

	"livecompanion/management/internal/model"
)

type liveSupportPlanScope struct {
	CreatorID         int64
	RoomIDs           []int64
	AuthorizedRoomIDs map[int64]bool
}

func liveSupportPlanScopeAllowed(scope liveSupportPlanScope, roomID, staffID int64, write bool) bool {
	if roomID <= 0 || staffID <= 0 || !scope.AuthorizedRoomIDs[roomID] {
		return false
	}
	bound := false
	for _, id := range scope.RoomIDs {
		if id == roomID {
			bound = true
		}
		if write && !scope.AuthorizedRoomIDs[id] {
			return false
		}
	}
	return bound || (len(scope.RoomIDs) == 0 && scope.CreatorID == staffID)
}

// Shared plan mutations can change another room's facts and scripts, so every
// active binding must have a live customer grant. Unbound customer plans are
// private unless this support employee created the draft.
func (s *Store) CanAccessLiveSupportPlan(ctx context.Context, tenantID, roomID, planID, staffID int64, write bool) (bool, error) {
	scope := liveSupportPlanScope{AuthorizedRoomIDs: map[int64]bool{}}
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(created_by_user_id, 0) FROM live_agent_plans WHERE tenant_id=? AND id=? AND status <> 'deleted'`, tenantID, planID).Scan(&scope.CreatorID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT room_id FROM live_agent_plan_room_bindings
		WHERE tenant_id=? AND plan_id=? AND status='active'
	`, tenantID, planID)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return false, err
		}
		scope.RoomIDs = append(scope.RoomIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	rows, err = s.db.QueryContext(ctx, `
		SELECT room_id FROM live_support_authorizations
		WHERE tenant_id=? AND staff_user_id=? AND capability=? AND status='active' AND revoked_at IS NULL
	`, tenantID, staffID, model.LiveSupportCapabilityL3Policy)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return false, err
		}
		scope.AuthorizedRoomIDs[id] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return liveSupportPlanScopeAllowed(scope, roomID, staffID, write), nil
}
