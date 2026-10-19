package db

import (
	"context"
	"strings"
	"time"

	lib "livecompanion/management/internal/speechlibrary"
)

const speechLibraryOpeningSchema = `CREATE TABLE IF NOT EXISTS mgmt_speech_library_openings (
 room_key CHAR(64) CHARACTER SET ascii NOT NULL, runtime_id BIGINT NOT NULL,
 tenant_id BIGINT NOT NULL, room_id BIGINT NOT NULL, plan_id BIGINT NOT NULL,
 context_sha CHAR(64) CHARACTER SET ascii NOT NULL, text_content TEXT NOT NULL,
 created_ms BIGINT NOT NULL, PRIMARY KEY(room_key,runtime_id),
 INDEX(room_key,context_sha,created_ms)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func openingRoomKey(sc lib.Scope) string {
	return lib.Digest(struct {
		Realm              string
		Tenant, Room, Plan int64
	}{sc.Realm, sc.Tenant, sc.Room, sc.Plan})
}

func (s *Store) RecentFirstOpenings(ctx context.Context, sc lib.Scope, limit int) ([]string, error) {
	if e := sc.Validate(); e != nil {
		return nil, e
	}
	if limit < 1 || limit > 5 {
		limit = 5
	}
	rows, e := s.db.QueryContext(ctx, `SELECT text_content FROM mgmt_speech_library_openings
 WHERE room_key=? AND tenant_id=? AND room_id=? AND plan_id=? AND context_sha=? AND runtime_id<>?
 ORDER BY created_ms DESC,runtime_id DESC LIMIT ?`, openingRoomKey(sc), sc.Tenant, sc.Room, sc.Plan, sc.RoomContext, sc.Runtime, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var text string
		if e := rows.Scan(&text); e != nil {
			return nil, e
		}
		out = append(out, text)
	}
	return out, rows.Err()
}

func (s *Store) RememberFirstOpening(ctx context.Context, sc lib.Scope, text string) error {
	if e := sc.Validate(); e != nil {
		return e
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	_, e := s.db.ExecContext(ctx, `INSERT INTO mgmt_speech_library_openings
 (room_key,runtime_id,tenant_id,room_id,plan_id,context_sha,text_content,created_ms)
 SELECT ?,?,?,?,?,?,?,? FROM live_runtime_sessions
 WHERE id=? AND tenant_id=? AND room_id=? AND ended_at IS NULL AND status IN ('running','paused','starting','resuming')
 ON DUPLICATE KEY UPDATE runtime_id=VALUES(runtime_id)`, openingRoomKey(sc), sc.Runtime, sc.Tenant, sc.Room, sc.Plan, sc.RoomContext, text, time.Now().UnixMilli(), sc.Runtime, sc.Tenant, sc.Room)
	return e
}
