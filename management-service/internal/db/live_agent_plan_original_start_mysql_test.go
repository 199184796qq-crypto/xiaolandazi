package db

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

// Use the actual room-start reader with a recording larger than the filesort
// buffer. The fixture lives in a fresh isolated schema; no audio is generated.
func TestOriginalRecordingRoomVersionMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s.db.SetMaxOpenConns(1)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	wanted := map[string]bool{"live_agent_plans": true, "live_agent_plan_versions": true, "live_agent_room_plan_selections": true, "live_agent_room_plan_publications": true, "live_agent_plan_room_bindings": true}
	for _, ddl := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(ddl)
		if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
			exec(ddl)
			delete(wanted, fields[5])
		}
	}
	if len(wanted) != 0 {
		t.Fatal("missing fixture tables", wanted)
	}
	if err := s.migratePlanVoiceRate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.migrateLiveContentPolicies(ctx); err != nil {
		t.Fatal(err)
	}
	// This affects only the test connection. Production/global settings stay put.
	exec(`SET SESSION sort_buffer_size=32768`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description,voice_rate) VALUES(4,14,'录音方案','',NULL),(5,14,'另一个方案','',1.20),(6,24,'其他租户','',1.00)`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(14,29,4),(14,26,4),(14,30,5),(24,29,6)`)
	variants := []model.LiveAgentPlanVersionVariant{{VariantKey: "CUSTOM", IsFormal: true, Text: strings.Repeat("测试原音。", 18000), AudioAssetID: 123, AudioURL: "http://fixture.invalid/original.wav", AudioDurationMS: 2414654}}
	large, err := json.Marshal(variants)
	if err != nil || len(large) <= 256<<10 {
		t.Fatal("long recording fixture required", err)
	}
	add := func(id, tenant, plan, room, no int64, status, mode string) {
		t.Helper()
		exec(`INSERT INTO live_agent_plan_versions(id,tenant_id,plan_id,room_id,version_no,lifecycle_status,voice_identity_json,variants_json,generation_context_json) VALUES(?,?,?,?,?,?,?, ?,?)`, id, tenant, plan, room, no, status, `{"voice_id":"fixture-voice","rate":1.17}`, string(large), `{"content_mode":"`+mode+`","recording_voice_mode":"original"}`)
	}
	add(10, 14, 4, 15, 900, "draft", "ai_dynamic")
	add(20, 14, 4, 0, 2, "published", "user_audio")
	add(30, 14, 4, 36, 3, "superseded", "user_audio")
	add(90, 14, 4, 0, 90, "prepared", "user_audio")
	add(91, 14, 4, 0, 91, "abandoned", "user_audio")
	add(92, 14, 5, 0, 1, "published", "user_audio")
	add(93, 24, 6, 0, 1, "published", "user_audio")
	for _, room := range []int64{29, 26} {
		v, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, room)
		if err != nil {
			t.Fatalf("room reader rejected large recording: %v", err)
		}
		if v.ID != 30 || v.PlanID != 4 || v.RoomID != room || v.SavedFromRoomID != 36 || v.VoiceIdentity.Rate != 1.17 || len(v.Variants) != 1 || v.Variants[0].Text != variants[0].Text {
			t.Fatal("latest effective plan/recording/voice identity changed")
		}
	}
	for _, tc := range []struct{ tenant, room, want int64 }{{14, 30, 92}, {24, 29, 93}} {
		v, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, tc.tenant, tc.room)
		if err != nil || v.ID != tc.want {
			t.Fatal("plan or tenant isolation changed", tc, err)
		}
	}
	if _, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 24, 26); err != ErrLiveAgentPlanVersionNotFound {
		t.Fatal("missing selection did not remain missing", err)
	}
	// The two legacy pointer paths must choose each mode's greatest ID without
	// sorting the large recording row or assuming the database's return order.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := publishLatestPlanPointersTx(ctx, tx, 14, 4, 29, 8); err != nil {
		tx.Rollback()
		t.Fatalf("legacy selection pointer sort: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.UnifyLiveAgentPlanSavedVersions(ctx, 14, 4); err != nil {
		t.Fatal("legacy repair still sorts full JSON", err)
	}
	var published int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_agent_plan_versions WHERE tenant_id=14 AND plan_id=4 AND lifecycle_status='published' AND id IN (10,30)`).Scan(&published); err != nil || published != 2 {
		t.Fatal("per-mode latest heads changed", published, err)
	}
}
