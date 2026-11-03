package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"livecompanion/management/internal/livecost"
)

type costRowsQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Used under the same room row lock as reservation. A late settlement remains
// charged for an hour after its accounting receipt; unresolved calls never age
// out into free budget. Released/declined work consumes no paid allowance.
func strictLiveCostSummary(ctx context.Context, q costRowsQuery, scope livecost.Scope, policy livecost.Policy, at time.Time) (livecost.Summary, error) {
	out := livecost.Summary{Scope: scope, WindowStart: at.Add(-policy.Window).UTC(), WindowEnd: at.UTC()}
	rows, err := q.QueryContext(ctx, `SELECT request_json,status,reserved_micros,charged_micros,usage_source,requested_at,updated_at FROM live_cost_requests
	 WHERE tenant_id=? AND room_id=? AND ((requested_at>=? AND requested_at<=?) OR (updated_at>=? AND updated_at<=?) OR status IN ('reserved','unknown'))`, scope.TenantID, scope.RoomID, out.WindowStart, out.WindowEnd, out.WindowStart, out.WindowEnd)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		var entry livecost.Entry
		var requested, updated time.Time
		if err = rows.Scan(&raw, &entry.Status, &entry.ReservedMicros, &entry.ChargedMicros, &entry.UsageSource, &requested, &updated); err != nil {
			return out, err
		}
		if err = json.Unmarshal([]byte(raw), &entry.Request); err != nil {
			return out, err
		}
		if entry.Request.TenantID != scope.TenantID || entry.Request.RoomID != scope.RoomID {
			return out, errors.New("费用账本房间身份不匹配")
		}
		if err = out.AddBudgetEntry(entry, requested.Before(out.WindowStart)); err != nil {
			return out, err
		}
	}
	return out, rows.Err()
}
