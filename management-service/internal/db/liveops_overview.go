package db

import (
	"context"

	"livecompanion/management/internal/model"
)

type LiveOpsRoomKey struct {
	TenantID int64
	RoomID   int64
}

type LiveOpsOverview struct {
	CooperatingUsers int
	TotalRooms       int
	RunningRooms     []LiveOpsRoomKey
}

func (s *Store) GetLiveOpsOverview(ctx context.Context, realm string) (LiveOpsOverview, error) {
	var overview LiveOpsOverview
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mgmt_users u
		JOIN mgmt_tenants tenant ON tenant.id=u.tenant_id
		LEFT JOIN crm_customer_profiles profile ON profile.tenant_id=u.tenant_id
		WHERE u.role='customer' AND u.status<>'archived'
		  AND COALESCE(profile.cooperation_status, 'cooperating')='cooperating'
	`).Scan(&overview.CooperatingUsers)
	if err != nil {
		return LiveOpsOverview{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT room.tenant_id, room.id, EXISTS (
			SELECT 1 FROM live_runtime_sessions session
			WHERE session.tenant_id=room.tenant_id AND session.room_id=room.id
			  AND session.execution_realm=? AND session.status='running' AND session.ended_at IS NULL
		)
		FROM core_rooms room
		JOIN mgmt_tenants tenant ON tenant.id=room.tenant_id
		WHERE EXISTS (
			SELECT 1 FROM mgmt_users u WHERE u.tenant_id=room.tenant_id AND u.role='customer'
		)
		  AND NOT EXISTS (
			SELECT 1 FROM mgmt_room_deletions deleted WHERE deleted.tenant_id=room.tenant_id AND deleted.room_id=room.id
		)
	`, model.NormalizeExecutionRealm(realm))
	if err != nil {
		return LiveOpsOverview{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var room LiveOpsRoomKey
		var running bool
		if err := rows.Scan(&room.TenantID, &room.RoomID, &running); err != nil {
			return LiveOpsOverview{}, err
		}
		overview.TotalRooms++
		if running {
			overview.RunningRooms = append(overview.RunningRooms, room)
		}
	}
	return overview, rows.Err()
}
