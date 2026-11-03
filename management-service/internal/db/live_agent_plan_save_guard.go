package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var ErrLiveAgentPlanInUse = errors.New("方案正在直播间使用，暂时不能保存，请先结束使用该方案的直播后再保存。")

type LiveAgentPlanInUseError struct{ RoomIDs []int64 }

func (e *LiveAgentPlanInUseError) Unwrap() error { return ErrLiveAgentPlanInUse }
func (e *LiveAgentPlanInUseError) Error() string {
	rooms := make([]string, len(e.RoomIDs))
	for i, id := range e.RoomIDs {
		rooms[i] = fmt.Sprintf("%d", id)
	}
	return fmt.Sprintf("该方案正在直播间 %s 使用，暂时不能保存。请先结束这些直播间的直播后再保存，页面中的修改已保留。", strings.Join(rooms, "、"))
}

type planLiveUsageReader interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func assertPlanNotLive(ctx context.Context, reader planLiveUsageReader, tenant, plan int64, locking bool) error {
	query := `SELECT DISTINCT session.room_id FROM live_runtime_sessions session
 INNER JOIN live_agent_room_plan_selections selection ON selection.tenant_id=session.tenant_id AND selection.room_id=session.room_id
 WHERE session.tenant_id=? AND selection.plan_id=? AND session.status IN ('running','paused') AND session.ended_at IS NULL ORDER BY session.room_id`
	if locking {
		query += " FOR UPDATE"
	}
	rows, err := reader.QueryContext(ctx, query, tenant, plan)
	if err != nil {
		return err
	}
	defer rows.Close()
	var rooms []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		rooms = append(rooms, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(rooms) > 0 {
		return &LiveAgentPlanInUseError{RoomIDs: rooms}
	}
	return nil
}
func (s *Store) AssertLiveAgentPlanEditable(ctx context.Context, tenant, plan int64) error {
	return assertPlanNotLive(ctx, s.db, tenant, plan, false)
}
