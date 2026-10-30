package db

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestPlanVoiceRateMySQLMigrationPersistenceAndIsolation(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(24) DEFAULT 'active',updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE live_agent_plan_versions(id BIGINT PRIMARY KEY,tenant_id BIGINT,plan_id BIGINT,lifecycle_status VARCHAR(24),voice_identity_json JSON)`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id) VALUES(4,14),(5,14),(6,24),(7,14)`)
	exec(`INSERT INTO live_agent_plan_versions VALUES(1,14,4,'published','{"rate":1.1}'),(2,24,6,'published','{"rate":0.9}'),(3,14,4,'prepared','{"rate":1.2}')`)
	for i := 0; i < 2; i++ {
		if err := s.migratePlanVoiceRate(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		tenant, plan int64
		rate         float64
	}{{14, 4, 1.1}, {14, 5, 1}, {24, 6, 0.9}} {
		got, err := s.GetPlanVoiceRate(ctx, item.tenant, item.plan)
		if err != nil || got.Rate != item.rate {
			t.Fatal("default/legacy rate", got, err)
		}
	}
	if _, err := s.SetPlanVoiceRate(ctx, 14, 4, 1.01); err != nil {
		t.Fatal(err)
	}
	// A later save from a stale tab must not replace the independent plan rate.
	exec(`INSERT INTO live_agent_plan_versions VALUES(4,14,4,'published','{"rate":0.8}')`)
	if err := s.migratePlanVoiceRate(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := (&Store{db: s.db}).GetPlanVoiceRate(ctx, 14, 4)
	if err != nil || got.Rate != 1.01 {
		t.Fatal("not persisted", got, err)
	}
	for _, rate := range []float64{0.8, 0.81, 0.9, 0.99, 1, 1.01, 1.02, 1.1, 1.2, 1.21, 1.39, 1.4, 1.4} {
		if got, err = s.SetPlanVoiceRate(ctx, 14, 5, rate); err != nil || got.Rate != rate {
			t.Fatal("new plan write/idempotence", got, err)
		}
	}
	for _, scope := range [][2]int64{{24, 4}, {14, 6}, {14, 99}} {
		if _, err = s.GetPlanVoiceRate(ctx, scope[0], scope[1]); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("cross-scope read", err)
		}
		if _, err = s.SetPlanVoiceRate(ctx, scope[0], scope[1], 1); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("cross-scope write", err)
		}
	}
	for _, rate := range []float64{1.015, 1.41, 1.401, 1.405} {
		if _, err = s.SetPlanVoiceRate(ctx, 14, 4, rate); !errors.Is(err, ErrInvalidPlanVoiceRate) {
			t.Fatal("invalid rate accepted", rate, err)
		}
	}
	exec(`UPDATE live_agent_plans SET status='deleted' WHERE id=7`)
	if _, err = s.SetPlanVoiceRate(ctx, 14, 7, 1); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatal("deleted plan write", err)
	}
	got, err = s.GetPlanVoiceRate(ctx, 24, 6)
	if err != nil || got.Rate != 0.9 {
		t.Fatal("other tenant changed", got, err)
	}
}

func TestPlanVoiceRateMySQLSavedSnapshotAndRuntimeProjection(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{"live_agent_plans": true, "live_agent_plan_versions": true, "live_agent_plan_room_bindings": true, "live_agent_room_plan_selections": true, "live_agent_room_plan_publications": true, "live_runtime_sessions": true}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(raw)
		if len(f) > 5 && f[0] == "CREATE" && wanted[f[5]] {
			exec(raw)
			delete(wanted, f[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal(wanted)
	}
	if err := s.migrateLiveContentPolicies(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'new plan','')`)
	exec(`INSERT INTO live_agent_plan_room_bindings(tenant_id,room_id,plan_id) VALUES(14,26,4)`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(14,26,4)`)
	exec(`INSERT INTO live_room_content_access(tenant_id,room_id,selected_mode,dynamic_authorized,updated_by_user_id) VALUES(14,26,'ai_dynamic',1,8)`)
	input := model.CreateLiveAgentPlanVersionInput{RoomID: 26, DurationMinutes: 30, RoundMinutes: 5, VoiceIdentity: model.LiveAgentVoiceIdentity{Name: "voice", Source: "clone", VoiceID: "voice", Rate: 1.2}, GenerationContext: map[string]any{"content_mode": "ai_dynamic"}}
	first, err := s.CreateLiveAgentPlanVersion(ctx, 14, 4, 8, input)
	if err != nil || first.VoiceIdentity.Rate != 1 {
		t.Fatal("new plan inherited room rate", first, err)
	}
	if _, err = s.SetPlanVoiceRate(ctx, 14, 4, 1.4); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, 26)
	if err != nil || current.VoiceIdentity.Rate != 1.4 {
		t.Fatal("runtime ignores plan setting", current, err)
	}
	archived, err := s.GetLiveAgentPlanVersion(ctx, 14, 4, first.ID)
	if err != nil || archived.VoiceIdentity.Rate != 1 {
		t.Fatal("historical snapshot mutated", archived, err)
	}
	input.VoiceIdentity.Rate = 0.8
	second, err := s.CreateLiveAgentPlanVersion(ctx, 14, 4, 8, input)
	if err != nil || second.VoiceIdentity.Rate != 1.4 {
		t.Fatal("stale save replaced plan preference", second, err)
	}
}
