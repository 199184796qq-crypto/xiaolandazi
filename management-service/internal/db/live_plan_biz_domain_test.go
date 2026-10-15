package db

import (
	"context"
	"errors"
	"testing"
)

func TestPlanBizDomainMySQLPersistenceMigrationAndScope(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	_, err := s.db.ExecContext(ctx, `CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,name VARCHAR(160),description TEXT,status VARCHAR(24) DEFAULT 'active')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'原方案','原资料'),(5,14,'另一方案','另一资料'),(6,24,'其他租户','其他资料')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = s.migratePlanBizDomain(ctx); err != nil {
			t.Fatal(err)
		}
	}
	assertMode := func(tenant, plan int64, want string) {
		t.Helper()
		got, err := s.GetPlanBizDomain(ctx, tenant, plan)
		if err != nil || got != want {
			t.Fatalf("mode %d/%d: %q %v", tenant, plan, got, err)
		}
	}
	assertMode(14, 4, "ecommerce")
	if got, err := s.SetPlanBizDomain(ctx, 14, 4, "local_service"); err != nil || got != "local_service" {
		t.Fatal(got, err)
	}
	otherStore := &Store{db: s.db}
	if got, err := otherStore.GetPlanBizDomain(ctx, 14, 4); err != nil || got != "local_service" {
		t.Fatal("not persisted", got, err)
	}
	for _, bad := range []string{"", "local", "commerce", "LOCAL_SERVICE", "ecommerce; DROP TABLE live_agent_plans"} {
		if _, err := s.SetPlanBizDomain(ctx, 14, 4, bad); !errors.Is(err, ErrInvalidPlanBizDomain) {
			t.Fatal("invalid mode accepted", bad, err)
		}
	}
	assertMode(14, 4, "local_service")
	for _, pair := range [][2]int64{{24, 4}, {14, 6}, {14, 99}} {
		if _, err := s.GetPlanBizDomain(ctx, pair[0], pair[1]); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("scope read", pair, err)
		}
		if _, err := s.SetPlanBizDomain(ctx, pair[0], pair[1], "local_service"); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal("scope write", pair, err)
		}
	}
	assertMode(14, 5, "ecommerce")
	assertMode(24, 6, "ecommerce")
	for i := 0; i < 2; i++ {
		if _, err := s.SetPlanBizDomain(ctx, 14, 4, "ecommerce"); err != nil {
			t.Fatal("idempotent save", err)
		}
	}
	var name, description string
	if err := s.db.QueryRowContext(ctx, `SELECT name,description FROM live_agent_plans WHERE id=4`).Scan(&name, &description); err != nil || name != "原方案" || description != "原资料" {
		t.Fatal("changed other plan fields", name, description, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE live_agent_plans SET status='archived' WHERE id=4`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetPlanBizDomain(ctx, 14, 4, "local_service"); !errors.Is(err, ErrLiveAgentPlanNotFound) {
		t.Fatal("archived plan writable", err)
	}
}
