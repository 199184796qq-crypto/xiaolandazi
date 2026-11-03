package db

import (
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voicebatch"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// Exercise actual transactional runtime metering, quota/resource ledgers and
// shared-plan save guards. The fixture always runs in a fresh local schema.
func TestSharedPlanMultiRoomMeterChargesTwoHoursAndKeepsIndependentSessionsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for schema, wanted := range map[string]map[string]bool{
		commercialSchema:  {"quota_buckets": true, "quota_ledger": true, "quota_usage_events": true},
		liveRuntimeSchema: {"live_runtime_sessions": true, "live_runtime_events": true, "live_agent_plans": true, "live_agent_room_plan_selections": true},
	} {
		for _, statement := range strings.Split(strings.ReplaceAll(schema, "\r\n", "\n"), "\n-- +statement\n") {
			fields := strings.Fields(statement)
			if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
				exec(statement)
			}
		}
	}
	// Use the exact account ledger DDL rather than a simplified substitute.
	raw, err := os.ReadFile("resources.go")
	if err != nil {
		t.Fatal(err)
	}
	ddl := regexp.MustCompile("(?s)`\\s*(CREATE TABLE IF NOT EXISTS org_resource_ledger \\(.*?)`").FindSubmatch(raw)
	if len(ddl) != 2 {
		t.Fatal("resource ledger DDL unavailable")
	}
	exec(string(ddl[1]))
	exec(`CREATE TABLE IF NOT EXISTS inv_devices(id BIGINT UNSIGNED PRIMARY KEY,sn VARCHAR(128))`)
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-time.Hour)
	exec(`INSERT INTO quota_buckets(external_id,tenant_id,source_type,original_seconds,remaining_seconds,effective_at,expires_at) VALUES('shared-plan-hours',14,'fixture',20000,20000,?,?)`, start.Add(-time.Hour), now.Add(24*time.Hour))
	exec(`INSERT INTO org_resource_accounts(organization_id,resource_type,unit,balance,reserved,status) VALUES(14,'ai_seconds','seconds',20000,0,'active')`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'shared plan','')`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(14,29,4),(14,30,4)`)
	exec(`INSERT INTO live_runtime_sessions(id,external_id,tenant_id,room_id,status,started_at,last_billed_at) VALUES(1001,'room-a',14,29,'running',?,?),(1002,'room-b',14,30,'running',?,?)`, start, start, start, start)
	runtimeDDL, err := os.ReadFile("db.go")
	if err != nil {
		t.Fatal(err)
	}
	voiceDDL := regexp.MustCompile("(?s)`(CREATE TABLE IF NOT EXISTS live_runtime_voice_batches \\(.*?)`").FindSubmatch(runtimeDDL)
	if len(voiceDDL) != 2 {
		t.Fatal("voice runtime DDL unavailable")
	}
	exec(string(voiceDDL[1]))
	for _, pair := range [][2]int64{{29, 1001}, {30, 1002}} {
		state := voicebatch.State{Tenant: 14, Room: pair[0], Session: pair[1], Version: 195, Active: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "same-model"}, Batch: 1}
		if saved, err := s.SaveRuntimeVoiceBatch(ctx, state); err != nil || !saved {
			t.Fatal(saved, err)
		}
	}
	roomA, err := s.RuntimeVoiceBatch(ctx, 14, 29, 1001, 195)
	if err != nil {
		t.Fatal(err)
	}
	roomA.Requested = &model.LiveAgentVoiceIdentity{VoiceID: "B", Model: "same-model"}
	if saved, err := s.SaveRuntimeVoiceBatch(ctx, roomA); err != nil || !saved {
		t.Fatal(saved, err)
	}
	roomB, err := s.RuntimeVoiceBatch(ctx, 14, 30, 1002, 195)
	if err != nil || roomB.Requested != nil || roomB.Active.VoiceID != "A" {
		t.Fatal("room A voice command crossed into B", roomB, err)
	}
	s.db.SetMaxOpenConns(4)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, id := range []int64{1001, 1002} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			session, err := s.ReconcileLiveRuntimeMeter(ctx, id, true, 3600, now)
			if err == nil && session.TotalBilledSeconds != 3600 {
				err = errors.New("room meter did not charge one hour")
			}
			errs <- err
		}(id)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := scalar(`SELECT remaining_seconds FROM quota_buckets WHERE tenant_id=14`); got != 12800 {
		t.Fatalf("two one-hour rooms must consume 7200 seconds, remaining=%d", got)
	}
	if got := scalar(`SELECT SUM(billable_seconds) FROM quota_usage_events WHERE tenant_id=14`); got != 7200 {
		t.Fatal("usage merged by plan", got)
	}
	if got := scalar(`SELECT balance FROM org_resource_accounts WHERE organization_id=14 AND resource_type='ai_seconds'`); got != 12800 {
		t.Fatal("customer balance mismatch", got)
	}
	for _, room := range []int64{29, 30} {
		if got := scalar(`SELECT SUM(billable_seconds) FROM quota_usage_events WHERE tenant_id=14 AND room_id=?`, room); got != 3600 {
			t.Fatal("room bill mismatch", room, got)
		}
	}
	// Replayed cumulative meters charge neither room twice.
	for _, id := range []int64{1001, 1002} {
		if _, err := s.ReconcileLiveRuntimeMeter(ctx, id, true, 3600, now); err != nil {
			t.Fatal(err)
		}
	}
	if got := scalar(`SELECT SUM(billable_seconds) FROM quota_usage_events WHERE tenant_id=14`); got != 7200 {
		t.Fatal("meter retry charged twice", got)
	}
	// Pausing A does not pause B or share its timer.
	exec(`UPDATE live_runtime_sessions SET status='paused' WHERE id=1001`)
	if _, err := s.ReconcileLiveRuntimeMeter(ctx, 1001, true, 4000, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReconcileLiveRuntimeMeter(ctx, 1002, true, 3900, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT total_billed_seconds FROM live_runtime_sessions WHERE id=1001`) != 3600 || scalar(`SELECT total_billed_seconds FROM live_runtime_sessions WHERE id=1002`) != 3900 {
		t.Fatal("room pause crossed session boundary")
	}
	if err := s.AssertLiveAgentPlanEditable(ctx, 14, 4); !errors.Is(err, ErrLiveAgentPlanInUse) {
		t.Fatal("live shared plan editable", err)
	}
	if _, err := s.ReconcileLiveRuntimeMeter(ctx, 1001, false, 3600, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if saved, err := s.SaveRuntimeVoiceBatch(ctx, roomA); err != nil || saved {
		t.Fatal("stopped room accepted voice update", saved, err)
	}
	if saved, err := s.SaveRuntimeVoiceBatch(ctx, roomB); err != nil || !saved {
		t.Fatal("stopping A stopped B's voice state", saved, err)
	}
	var usage *LiveAgentPlanInUseError
	if err := s.AssertLiveAgentPlanEditable(ctx, 14, 4); !errors.As(err, &usage) || len(usage.RoomIDs) != 1 || usage.RoomIDs[0] != 30 {
		t.Fatal("stopping A released B's save guard", err)
	}
	if _, err := s.ReconcileLiveRuntimeMeter(ctx, 1002, false, 3900, now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.AssertLiveAgentPlanEditable(ctx, 14, 4); err != nil {
		t.Fatal("all rooms stopped but plan still locked", err)
	}
	if got := scalar(`SELECT SUM(billable_seconds) FROM quota_usage_events WHERE tenant_id=14`); got != 7500 {
		t.Fatal("final combined room consumption mismatch", got)
	}
}
