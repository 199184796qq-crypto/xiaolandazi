package db

import (
	"context"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestCustomerPasswordResetGrantMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(query string) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS sys_feature_records (") {
			exec(statement)
		}
	}
	exec("CREATE TABLE IF NOT EXISTS crm_registration_referrals(tenant_id BIGINT, referred_user_id BIGINT, inviter_user_id BIGINT)")
	exec("CREATE TABLE IF NOT EXISTS live_policy_tenant_industries(tenant_id BIGINT, industry_code VARCHAR(64))")
	exec("CREATE TABLE IF NOT EXISTS live_policy_industries(code VARCHAR(64), name VARCHAR(128))")
	exec("INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES (20,'customer',1,'target','Target','active'),(21,'customer',1,'other','Other','active')")
	exec("INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,role,status) VALUES (300,20,'target','Target','test-300','customer','active'),(301,21,'other','Other','test-301','customer','active')")
	exec("INSERT INTO mgmt_sessions(user_id) VALUES (300),(301)")

	scope := model.StaffBusinessScope{Mode: "groups", ActorUserID: 101, GroupIDs: []int64{10}, ManagerView: true}
	check := func(sc model.StaffBusinessScope, employee, target int64, want bool) {
		t.Helper()
		got, err := s.CanResetCustomerPassword(ctx, sc, employee, target)
		if err != nil || got != want {
			t.Fatalf("employee %d target %d: got %t, want %t, err %v", employee, target, got, want, err)
		}
	}
	check(scope, 1, 300, false)
	exec("INSERT INTO sys_feature_records(feature_key,record_key,title,status) VALUES ('staff-customer-password-reset-grants','1:300:20','Fixture grant','active')")
	check(scope, 1, 300, true)
	check(scope, 1, 301, false)
	check(scope, 2, 300, false)
	check(scope, 0, 300, false)
	otherActor := scope
	otherActor.ActorUserID = 102
	check(otherActor, 1, 300, false)
	check(otherActor, 2, 300, false)
	check(model.StaffBusinessScope{Mode: "all", ActorUserID: 900}, 0, 301, true)
	if _, err := s.AdminResetCustomerPassword(ctx, 300, "fixture-reset-hash"); err != nil {
		t.Fatal(err)
	}
	var changed, targetSessions, otherSessions int
	if err := s.db.QueryRowContext(ctx, "SELECT must_change_password, (SELECT COUNT(*) FROM mgmt_sessions WHERE user_id=300), (SELECT COUNT(*) FROM mgmt_sessions WHERE user_id=301) FROM mgmt_users WHERE id=300").
		Scan(&changed, &targetSessions, &otherSessions); err != nil || changed != 1 || targetSessions != 0 || otherSessions != 1 {
		t.Fatalf("reset lifecycle: %d %d %d err %v", changed, targetSessions, otherSessions, err)
	}
	exec("UPDATE mgmt_users SET status='disabled' WHERE id=300")
	check(scope, 1, 300, false)
	exec("UPDATE mgmt_users SET status='active', tenant_id=21 WHERE id=300")
	check(scope, 1, 300, false)
	exec("UPDATE mgmt_users SET tenant_id=20 WHERE id=300")
	exec("UPDATE sys_feature_records SET status='disabled' WHERE record_key='1:300:20'")
	check(scope, 1, 300, false)
}
