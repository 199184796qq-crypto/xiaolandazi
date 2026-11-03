package db

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/livecost"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voicebatch"
)

func TestRuntimeVoiceBudgetMigrationMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := t.Context()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	// Exercise the exact additive DDL included in Store.Migrate, not a fixture
	// copy that could silently disagree with the production migration.
	raw, err := os.ReadFile("db.go")
	if err != nil {
		t.Fatal(err)
	}
	ddl := regexp.MustCompile("(?s)`(CREATE TABLE IF NOT EXISTS live_runtime_voice_batches \\(.*?)`").FindSubmatch(raw)
	if len(ddl) != 2 {
		t.Fatal("voice migration DDL missing")
	}
	exec(string(ddl[1]))
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) > 5 && fields[0] == "CREATE" && fields[5] == "live_runtime_sessions" {
			exec(statement)
		}
	}
	exec(`INSERT INTO live_runtime_sessions(id,external_id,tenant_id,room_id,status,started_at,last_billed_at) VALUES(600,'local-voice',14,29,'running',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`)
	state := voicebatch.State{Tenant: 14, Room: 29, Session: 600, Version: 44, Batch: 1, Active: model.LiveAgentVoiceIdentity{VoiceID: "A", Model: "m"}}
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	state, err = s.RuntimeVoiceBatch(ctx, 14, 29, 600, 44)
	if err != nil || state.Revision != 1 {
		t.Fatal(state, err)
	}
	stale := state
	state.Requested = &model.LiveAgentVoiceIdentity{VoiceID: "B", Model: "m"}
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, state); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, stale); err != nil || ok {
		t.Fatal("stale CAS accepted", ok, err)
	}
	exec(string(ddl[1])) // Idempotent migration must preserve the state row.
	state, err = s.RuntimeVoiceBatch(ctx, 14, 29, 600, 44)
	if err != nil || state.Revision != 2 || state.Requested == nil || state.Requested.VoiceID != "B" {
		t.Fatal(state, err)
	}
	if _, err := s.RuntimeVoiceBatch(ctx, 24, 29, 600, 44); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant state leak", err)
	}
	exec(`UPDATE live_runtime_sessions SET status='stopped',ended_at=UTC_TIMESTAMP(3) WHERE id=600`)
	if ok, err := s.SaveRuntimeVoiceBatch(ctx, state); err != nil || ok {
		t.Fatal("stopped session accepted", ok, err)
	}

	if err := s.MigrateLiveCostRequests(ctx); err != nil {
		t.Fatal(err)
	}
	s.db.SetMaxOpenConns(16)
	b, _ := livecost.RoomBudgetForPool("standard")
	now := time.Now().UTC()
	var wg sync.WaitGroup
	var accepted atomic.Int64
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := sqlCostRequest(fmt.Sprintf("mysql-mainline-%d", i))
			r.EstimatedMicros = 100_000
			r.BudgetPool = b.Pool
			r.BudgetClass = livecost.BudgetMainline
			out, err := s.ReserveLiveCost(ctx, r, b.Policy(), now)
			if err != nil {
				t.Error(err)
				return
			}
			if out.Allowed {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 8 {
		t.Fatal("concurrent mainline exceeded category", accepted.Load())
	}
	r := sqlCostRequest("mysql-interaction")
	r.BudgetPool = b.Pool
	r.BudgetClass = livecost.BudgetInteraction
	r.EstimatedMicros = b.Interaction
	if out, err := s.ReserveLiveCost(ctx, r, b.Policy(), now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	r.Key = "mysql-rewrite"
	r.Kind = livecost.KindModel
	r.BudgetClass = livecost.BudgetRewrite
	r.EstimatedMicros = b.Rewrite
	if out, err := s.ReserveLiveCost(ctx, r, b.Policy(), now); err != nil || !out.Allowed {
		t.Fatal(out, err)
	}
	r.Key = "mysql-new-session"
	r.SessionID = 999
	r.EstimatedMicros = 1
	r.BudgetClass = livecost.BudgetMainline
	if out, err := s.ReserveLiveCost(ctx, r, b.Policy(), now.Add(2*time.Hour)); err != nil || out.Allowed {
		t.Fatal("unknown reservation expired", out, err)
	}
	summary, err := s.LiveCostSummary(ctx, livecost.Scope{TenantID: 14, RoomID: 29}, b.Policy(), now)
	if err != nil || summary.TotalMicros != 1_200_000 {
		t.Fatal(summary, err)
	}
}
