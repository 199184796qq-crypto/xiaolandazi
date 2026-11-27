package db

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"livecompanion/management/internal/model"
)

func TestLivePlanSupportLifecycleMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE quota_buckets(id BIGINT UNSIGNED PRIMARY KEY, remaining_seconds BIGINT UNSIGNED NOT NULL)`)
	if err := s.MigrateLiveRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TABLE IF NOT EXISTS inv_devices(id BIGINT PRIMARY KEY, sn VARCHAR(64))`)
	changePlan := func(tenantID, roomID, planID, staffID int64, clear bool) error {
		_, err := s.ChangeLiveAgentRoomPlan(ctx, tenantID, roomID, LiveAgentRoomPlanChange{
			PlanID: planID, ActorID: staffID, Clear: clear, RequireSupportGrant: true,
		})
		return err
	}
	checkSelection := func(roomID, want int64) {
		t.Helper()
		var selected int64
		err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT plan_id FROM live_agent_room_plan_selections WHERE tenant_id=7 AND room_id=?),0)`, roomID).Scan(&selected)
		if err != nil || selected != want {
			t.Fatalf("room %d selected plan %d, want %d: %v", roomID, selected, want, err)
		}
	}
	exec(`CREATE TABLE staff_groups(id BIGINT PRIMARY KEY, code VARCHAR(32), status VARCHAR(32))`)
	exec(`INSERT INTO staff_groups VALUES (30,'live_operations','active')`)
	exec(`INSERT INTO mgmt_users(id,username,display_name,phone,role,status) VALUES
		(901,'operator_b','Operations B','test-901','platform_admin','active')`)
	exec(`INSERT INTO staff_employees(user_id,primary_group_id,employment_status) VALUES (900,30,'active'), (901,30,'active')`)
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES
		(9,7,'未绑定直播间',''), (10,7,'同客户别的方案',''), (11,8,'别的客户','')`)
	exec(`CREATE TABLE core_rooms(id BIGINT PRIMARY KEY, tenant_id BIGINT NOT NULL, name VARCHAR(128), external_room_id VARCHAR(128))`)
	exec(`CREATE TABLE mgmt_room_deletions(tenant_id BIGINT NOT NULL, room_id BIGINT NOT NULL)`)
	exec(`INSERT INTO core_rooms VALUES (29, 7, '授权直播间', '29001'), (30, 7, '同客户另外的直播间', '30001'), (31, 7, '删除中的直播间', '31001'), (39, 8, '其他客户直播间', '39001')`)
	exec(`INSERT INTO mgmt_room_deletions VALUES (7,31)`)
	exec(`INSERT INTO live_agent_plan_room_bindings(tenant_id,room_id,plan_id,status) VALUES(7,29,9,'active'),(7,30,9,'active')`)
	exec(`INSERT INTO live_agent_room_plan_selections(tenant_id, room_id, plan_id) VALUES (7, 29, 10)`)
	exec(`INSERT INTO live_agent_room_plan_publications(tenant_id,room_id,plan_id,version_id) VALUES (7,29,10,100)`)
	if err := s.MigrateWorkInbox(ctx); err != nil {
		t.Fatal(err)
	}
	expectInbox := func(staffID, want int64) {
		t.Helper()
		scope := model.InboxScope{Actor: model.Actor{Role: "platform_admin", UserID: staffID}}
		page, err := s.InboxItems(ctx, scope, "live_plan_support_accept", 1, 10, false)
		if err != nil || page.Total != want || int64(len(page.Items)) != want {
			t.Fatalf("inbox for employee %d: %+v %v", staffID, page, err)
		}
	}

	expectTenant := func(plan, staff, want int64) {
		t.Helper()
		tenant, err := s.AuthorizedLivePlanSupportTenant(ctx, plan, staff)
		if want == 0 && errors.Is(err, sql.ErrNoRows) {
			return
		}
		if tenant != want || err != nil || want == 0 {
			t.Fatalf("plan %d / employee %d: tenant=%d err=%v; want %d", plan, staff, tenant, err, want)
		}
	}
	if _, err := s.CreateLivePlanSupport(ctx, 7, 11, 900, 77, 0); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatalf("another tenant created a request: %v", err)
	}
	for _, roomID := range []int64{31, 39, 999} {
		if _, err := s.CreateLivePlanSupport(ctx, 7, 9, 900, 77, roomID); !errors.Is(err, ErrLivePlanSupportRoomUnavailable) {
			t.Fatalf("authorized an unavailable room %d: %v", roomID, err)
		}
	}
	initial, err := s.CreateLivePlanSupport(ctx, 7, 9, 900, 77, 29)
	if err != nil || initial.Status != "pending" || initial.PlanID != 9 || initial.PlanName != "未绑定直播间" || initial.AuthorizedRoomID != 29 || initial.AuthorizedRoomName != "授权直播间" {
		t.Fatalf("request for unbound plan: %+v %v", initial, err)
	}
	expectInbox(900, 1)
	expectInbox(901, 0)
	duplicate, err := s.CreateLivePlanSupport(ctx, 7, 9, 900, 77, 29)
	if err != nil || duplicate.ID != initial.ID {
		t.Fatalf("duplicate request: %+v %v", duplicate, err)
	}
	if _, err := s.CreateLivePlanSupport(ctx, 7, 9, 900, 77, 30); !errors.Is(err, ErrLiveSupportRequestState) {
		t.Fatalf("changed the room of an open request: %v", err)
	}
	expectTenant(9, 900, 0)
	if items, err := s.ListAuthorizedRoomPlans(ctx, 7, 29, 900); err != nil || len(items) != 0 {
		t.Fatalf("pending grant was offered for mounting: %+v %v", items, err)
	}
	if err := changePlan(7, 29, 9, 900, false); !errors.Is(err, ErrLiveSupportRequestAccess) {
		t.Fatalf("mount with pending grant: %v", err)
	}
	if _, err = s.DecideLivePlanSupport(ctx, initial.ID, 901, true); !errors.Is(err, ErrLiveSupportRequestAccess) {
		t.Fatalf("unassigned employee accepted: %v", err)
	}
	if items, err := s.ListLivePlanSupportForStaff(ctx, 901); err != nil || len(items) != 0 {
		t.Fatalf("request leaked into teammate inbox: %+v %v", items, err)
	}
	accepted, err := s.DecideLivePlanSupport(ctx, initial.ID, 900, true)
	if err != nil || accepted.Status != "accepted" || accepted.DecidedAt == nil {
		t.Fatalf("accept: %+v %v", accepted, err)
	}
	expectInbox(900, 0)
	expectTenant(9, 900, 7)
	expectTenant(9, 901, 0)
	expectTenant(10, 900, 0)
	expectTenant(11, 900, 0)
	choices, err := s.ListAuthorizedRoomPlans(ctx, 7, 29, 900)
	if err != nil || len(choices) != 1 || choices[0].ID != 9 || choices[0].Selected {
		t.Fatalf("mount choices must be limited to the accepted plan: %+v %v", choices, err)
	}
	if items, err := s.ListAuthorizedRoomPlans(ctx, 7, 30, 900); err != nil || len(items) != 0 {
		t.Fatalf("grant was offered for a different room of the same customer: %+v %v", items, err)
	}
	for _, denied := range []struct{ tenant, room, plan, staff int64 }{
		{7, 30, 9, 900},  // same plan and employee, different room
		{7, 29, 10, 900}, // same customer, different plan
		{7, 29, 9, 901},  // another employee
		{8, 39, 9, 900},  // another customer
	} {
		for _, clear := range []bool{false, true} {
			if err := changePlan(denied.tenant, denied.room, denied.plan, denied.staff, clear); !errors.Is(err, ErrLiveSupportRequestAccess) {
				t.Fatalf("room selection changed outside its grant %+v: %v", denied, err)
			}
		}
	}
	if err := changePlan(7, 29, 9, 900, false); !errors.Is(err, ErrLiveAgentPlanNotPublished) {
		t.Fatalf("unsaved plan was selected: %v", err)
	}
	checkSelection(29, 10)
	exec(`INSERT INTO live_agent_plan_versions(id,tenant_id,plan_id,room_id,version_no,lifecycle_status,voice_identity_json,variants_json,generation_context_json)
		VALUES (89,7,9,0,1,'published','{}','[]','{}'),(100,7,10,0,1,'published','{}','[]','{}')`)
	exec(`INSERT INTO live_runtime_sessions(external_id,tenant_id,room_id,status,started_at,last_billed_at) VALUES ('plan-mount',7,29,'running',NOW(3),NOW(3))`)
	for _, clear := range []bool{false, true} {
		if err := changePlan(7, 29, 9, 900, clear); !errors.Is(err, ErrLiveAgentPlanInUse) {
			t.Fatalf("modified plan while playing: %v", err)
		}
	}
	exec(`UPDATE live_runtime_sessions SET status='paused' WHERE external_id='plan-mount'`)
	for range 2 {
		if err := changePlan(7, 29, 9, 900, false); err != nil {
			t.Fatalf("authorized mount or retry: %v", err)
		}
	}
	checkSelection(29, 9)
	choices, err = s.ListAuthorizedRoomPlans(ctx, 7, 29, 900)
	if err != nil || len(choices) != 1 || choices[0].ID != 9 || !choices[0].Selected {
		t.Fatalf("selected plan was not marked as current: %+v %v", choices, err)
	}
	published, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 7, 29)
	if err != nil || published.ID != 89 {
		t.Fatalf("room does not use the mounted plan's shared version: %+v %v", published, err)
	}
	var sessionVersion int64
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM live_runtime_sessions WHERE external_id='plan-mount'`).Scan(&sessionVersion); err != nil || sessionVersion != 2 {
		t.Fatalf("mount should invalidate an old paused resume exactly once: %d %v", sessionVersion, err)
	}
	// A stale cancellation cannot clear the plan chosen meanwhile by the customer.
	if _, err := s.ChangeLiveAgentRoomPlan(ctx, 7, 29, LiveAgentRoomPlanChange{PlanID: 10, ActorID: 77}); err != nil {
		t.Fatal(err)
	}
	if err := changePlan(7, 29, 9, 900, true); !errors.Is(err, ErrLiveAgentPlanSelectionChanged) {
		t.Fatalf("stale unmount changed another selection: %v", err)
	}
	checkSelection(29, 10)
	if err := changePlan(7, 29, 9, 900, false); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := changePlan(7, 29, 9, 900, true); err != nil {
			t.Fatalf("authorized unmount or retry: %v", err)
		}
	}
	if _, err := s.GetLiveAgentPlanForRoom(ctx, 7, 29); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatalf("room has a current plan after unmount: %v", err)
	}
	if _, err := s.GetPublishedLiveAgentPlanVersionForRoom(ctx, 7, 29); !errors.Is(err, ErrLiveAgentPlanVersionNotFound) {
		t.Fatalf("room still has a playable version after unmount: %v", err)
	}
	if err := s.MigrateLiveRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	checkSelection(29, 0)
	checkSelection(30, 0) // An existing binding alone must not select a plan on restart.
	if err := s.RevokeLivePlanSupport(ctx, 7, 10, initial.ID, 77); !errors.Is(err, ErrLiveSupportRequestState) {
		t.Fatalf("another plan revoked grant: %v", err)
	}
	if err := s.RevokeLivePlanSupport(ctx, 7, 9, initial.ID, 77); err != nil {
		t.Fatal(err)
	}
	expectTenant(9, 900, 0)
	if items, err := s.ListAuthorizedRoomPlans(ctx, 7, 29, 900); err != nil || len(items) != 0 {
		t.Fatalf("revoked grant was offered for mounting: %+v %v", items, err)
	}
	for _, clear := range []bool{false, true} {
		if err := changePlan(7, 29, 9, 900, clear); !errors.Is(err, ErrLiveSupportRequestAccess) {
			t.Fatalf("modified selection with revoked grant: %v", err)
		}
	}
	next, err := s.CreateLivePlanSupport(ctx, 7, 9, 900, 77, 0)
	if err != nil || next.ID == initial.ID || next.Status != "pending" || next.AuthorizedRoomID != 0 {
		t.Fatalf("new request after revoke: %+v %v", next, err)
	}
	if _, err = s.DecideLivePlanSupport(ctx, initial.ID, 900, true); !errors.Is(err, ErrLiveSupportRequestState) {
		t.Fatalf("old request was reactivated: %v", err)
	}
	if _, err = s.DecideLivePlanSupport(ctx, next.ID, 900, false); err != nil {
		t.Fatal(err)
	}
	expectTenant(9, 900, 0)
}
