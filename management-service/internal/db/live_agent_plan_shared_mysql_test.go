package db

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSharedPlanRoomsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, a ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, a...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	wanted := map[string]bool{}
	for _, table := range []string{"live_agent_plans", "live_agent_plan_room_bindings", "live_agent_room_plan_selections", "live_agent_room_plan_publications", "live_agent_plan_versions", "live_runtime_sessions"} {
		wanted[table] = true
	}
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
	if err := s.MigrateLiveContentRefresh(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'同一个用户方案',''),(5,14,'另一个方案',''),(6,24,'其他用户方案','')`)
	for _, room := range []int64{15, 26, 27, 35, 36} {
		exec(`INSERT INTO live_agent_plan_room_bindings(tenant_id,room_id,plan_id,status) VALUES(14,?,4,'active')`, room)
		selected, mode := int64(4), "ai_dynamic"
		if room == 35 {
			selected = 5
		}
		if room == 36 {
			mode = "user_audio"
		}
		exec(`INSERT INTO live_agent_room_plan_selections(tenant_id,room_id,plan_id) VALUES(14,?,?)`, room, selected)
		exec(`INSERT INTO live_room_content_access(tenant_id,room_id,selected_mode,dynamic_authorized,updated_by_user_id) VALUES(14,?,?,1,8)`, room, mode)
	}
	exec(`INSERT INTO live_agent_plan_versions(id,tenant_id,plan_id,room_id,version_no,lifecycle_status,voice_identity_json,variants_json,generation_context_json,published_by_user_id) VALUES(74,14,4,15,2,'published','{"voice_id":"voice-old"}','[]','{"content_mode":"ai_dynamic","storage":"6天"}',8),(76,14,4,26,27,'published','{"voice_id":"voice-shared"}','[]','{"content_mode":"ai_dynamic","storage":"抽真空6天，不抽真空3天"}',8),(72,14,4,36,1,'published','{}','[]','{"content_mode":"user_audio"}',8),(73,14,5,35,1,'published','{}','[]','{"content_mode":"ai_dynamic"}',8)`)
	exec(`INSERT INTO live_agent_room_plan_publications(tenant_id,room_id,plan_id,version_id) VALUES(14,15,4,74),(14,26,4,76),(14,27,4,74),(14,35,5,73),(14,36,4,72)`)
	assertShared := func(id int64) {
		t.Helper()
		for _, room := range []int64{15, 26, 27} {
			v, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 14, room)
			if err != nil || v.ID != id || v.RoomID != room || v.GenerationContext["storage"] != "抽真空6天，不抽真空3天" {
				t.Fatal("room still reads another copy", room, v, err)
			}
		}
	}
	assertShared(76) // Reproduces the old room15/26 version split before any new save.
	if err := s.UnifyLiveAgentPlanSavedVersions(ctx, 14, 4); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT COUNT(DISTINCT version_id) FROM live_agent_room_plan_publications WHERE tenant_id=14 AND room_id IN (15,26,27)`) != 1 || scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE tenant_id=14 AND plan_id=4`) != 3 {
		t.Fatal("repair copied content instead of sharing existing version")
	}
	if scalar(`SELECT version_id FROM live_agent_room_plan_publications WHERE tenant_id=14 AND room_id=35`) != 73 || scalar(`SELECT version_id FROM live_agent_room_plan_publications WHERE tenant_id=14 AND room_id=36`) != 72 {
		t.Fatal("repair switched a different plan or content mode")
	}
	input := model.CreateLiveAgentPlanVersionInput{RoomID: 15, VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "voice-shared"}, GenerationContext: map[string]any{"content_mode": "ai_dynamic", "storage": "抽真空6天，不抽真空3天"}}
	v, err := s.CreateLiveAgentPlanVersion(ctx, 14, 4, 8, input)
	if err != nil || v.VersionNo != 28 {
		t.Fatal("save is still numbered per room", v, err)
	}
	assertShared(v.ID)
	if scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE tenant_id=14 AND plan_id=4 AND lifecycle_status='published' AND JSON_UNQUOTE(JSON_EXTRACT(generation_context_json,'$.content_mode'))='ai_dynamic'`) != 1 {
		t.Fatal("more than one active saved copy")
	}
	for _, room := range []int64{15, 26, 27} {
		items, err := s.ListLiveAgentPlanVersions(ctx, 14, 4, room)
		if err != nil || len(items) == 0 || items[0].ID != v.ID {
			t.Fatal("workspace reads a room-local copy", room, items, err)
		}
	}
	if _, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 24, 26); err != ErrLiveAgentPlanVersionNotFound {
		t.Fatal("other user obtained this plan", err)
	}
	exec(`INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,status,execution_realm,started_at,last_billed_at) VALUES('shared-test',14,15,'running','test',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))`)
	var wg sync.WaitGroup
	results := make(chan model.LiveAgentPlanVersion, 2)
	errs := make(chan error, 2)
	for _, room := range []int64{26, 27} {
		wg.Add(1)
		go func(room int64) {
			defer wg.Done()
			copy := input
			copy.RoomID = room
			v, err := s.CreateLiveAgentPlanVersion(ctx, 14, 4, 8, copy)
			if err != nil {
				errs <- err
			} else {
				results <- v
			}
		}(room)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal("concurrent room saves failed", err)
	}
	numbers := map[int64]bool{}
	var latest int64
	for v := range results {
		numbers[v.VersionNo] = true
		if v.ID > latest {
			latest = v.ID
		}
	}
	if !numbers[29] || !numbers[30] {
		t.Fatal("same plan allocated room-specific version numbers", numbers)
	}
	assertShared(latest)
	// A running room protects the startup version, even when another room saves.
	before := scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE tenant_id=14 AND plan_id=4`)
	input.RoomID = 26
	if _, err = s.CreateLiveAgentPlanVersion(ctx, 14, 4, 8, input); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT COUNT(*) FROM live_agent_plan_versions WHERE tenant_id=14 AND plan_id=4`) != before+1 {
		t.Fatal("save pruned a different room's startup version")
	}
	if _, err = s.GetLiveAgentPlanVersion(ctx, 14, 4, 74); err != nil {
		t.Fatal("running room's version disappeared", err)
	}
}
