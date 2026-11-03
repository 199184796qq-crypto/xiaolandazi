package db

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestPlanSaveLiveUsageGuardMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	wanted := map[string]bool{"live_agent_plans": true, "live_agent_room_plan_selections": true, "live_runtime_sessions": true}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && wanted[fields[5]] {
			if _, err := s.db.ExecContext(ctx, raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(7,14,'A',''),(8,14,'B',''),(9,24,'other','')`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(14,29,7),(14,26,8),(24,39,9)`)
	check := func(tenant, plan int64, blocked bool) {
		t.Helper()
		err := s.AssertLiveAgentPlanEditable(ctx, tenant, plan)
		if blocked != errors.Is(err, ErrLiveAgentPlanInUse) || (!blocked && err != nil) {
			t.Fatalf("tenant=%d plan=%d blocked=%v got=%v", tenant, plan, blocked, err)
		}
	}
	check(14, 7, false)
	exec(`INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,status,execution_realm,started_at,last_billed_at) VALUES('guard',14,29,'running','test',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`)
	check(14, 7, true)
	check(14, 8, false)
	check(24, 7, false)
	exec(`UPDATE live_runtime_sessions SET status='paused' WHERE external_id='guard'`)
	check(14, 7, true)
	exec(`UPDATE live_runtime_sessions SET status='stopped',ended_at=UTC_TIMESTAMP(3) WHERE external_id='guard'`)
	check(14, 7, false)
	// A start holding the plan row serializes the version save. Its transaction
	// commits a new running session before the save's locking usage read.
	start, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer start.Rollback()
	var id int64
	if err := start.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=14 AND id=7 FOR UPDATE`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := start.ExecContext(ctx, `INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,status,execution_realm,started_at,last_billed_at) VALUES('concurrent',14,29,'running','test',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`); err != nil {
		t.Fatal(err)
	}
	if err := start.Commit(); err != nil {
		t.Fatal(err)
	}
	save, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer save.Rollback()
	if err := save.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=14 AND id=7 FOR UPDATE`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := assertPlanNotLive(ctx, save, 14, 7, true); !errors.Is(err, ErrLiveAgentPlanInUse) {
		t.Fatal("final transaction did not refuse live plan", err)
	}
}
