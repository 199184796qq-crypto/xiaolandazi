package db

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestLiveAnchorBindingMySQLScopeAndCancellation(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	for _, ddl := range []string{
		`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(24) DEFAULT 'active')`,
		`CREATE TABLE live_anchor_styles(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(24) DEFAULT 'active')`,
		`CREATE TABLE live_anchor_style_plan_bindings(tenant_id BIGINT,style_id BIGINT,plan_id BIGINT PRIMARY KEY,bound_by_user_id BIGINT,bound_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`,
		`CREATE TABLE live_anchor_style_samples(id BIGINT PRIMARY KEY,style_id BIGINT,content TEXT)`,
		`INSERT INTO live_agent_plans VALUES(4,14,'active'),(5,14,'active'),(6,24,'active'),(7,14,'archived')`,
		`INSERT INTO live_anchor_styles VALUES(7,14,'active'),(8,14,'active'),(9,24,'active'),(10,14,'archived')`,
		`INSERT INTO live_anchor_style_samples VALUES(1,7,'原话术'),(2,8,'另一主播话术')`,
	} {
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	bound := func(plan, want int64) {
		t.Helper()
		var got int64
		err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(style_id),0) FROM live_anchor_style_plan_bindings WHERE plan_id=?`, plan).Scan(&got)
		if err != nil || got != want {
			t.Fatalf("plan %d: got %d want %d: %v", plan, got, want, err)
		}
	}
	t.Run("unlink only this plan and preserve presenter assets", func(t *testing.T) {
		for _, binding := range [][3]int64{{14, 7, 4}, {14, 7, 5}, {24, 9, 6}} {
			if err := s.BindLiveAnchorStyleToPlan(ctx, binding[0], binding[1], binding[2], 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.UnbindLiveAnchorStyleFromPlan(ctx, 14, 7, 4); err != nil {
			t.Fatal(err)
		}
		bound(4, 0)
		bound(5, 7)
		bound(6, 9)
		var styles, samples int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_anchor_styles`).Scan(&styles); err != nil || styles != 4 {
			t.Fatal(styles, err)
		}
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_anchor_style_samples`).Scan(&samples); err != nil || samples != 2 {
			t.Fatal(samples, err)
		}
		if err := s.UnbindLiveAnchorStyleFromPlan(ctx, 14, 7, 4); err != nil {
			t.Fatal("retry must be idempotent", err)
		}
	})
	t.Run("foreign and archived assets cannot be bound or unlinked", func(t *testing.T) {
		for _, binding := range [][3]int64{{14, 9, 4}, {24, 7, 6}, {14, 10, 4}, {14, 7, 7}, {24, 9, 4}, {14, 7, 6}} {
			if err := s.BindLiveAnchorStyleToPlan(ctx, binding[0], binding[1], binding[2], 1); err == nil {
				t.Fatal("invalid binding accepted", binding)
			}
		}
		if err := s.UnbindLiveAnchorStyleFromPlan(ctx, 24, 7, 5); !errors.Is(err, ErrLiveAgentPlanNotFound) {
			t.Fatal(err)
		}
		bound(5, 7)
		bound(6, 9)
	})
	t.Run("stale cancel cannot detach a newly selected presenter", func(t *testing.T) {
		if err := s.BindLiveAnchorStyleToPlan(ctx, 14, 8, 4, 1); err != nil {
			t.Fatal(err)
		}
		if err := s.UnbindLiveAnchorStyleFromPlan(ctx, 14, 7, 4); !errors.Is(err, ErrLiveAnchorBindingChanged) {
			t.Fatal(err)
		}
		bound(4, 8)
	})
	t.Run("concurrent replacement survives cancellation of the old presenter", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			if err := s.BindLiveAnchorStyleToPlan(ctx, 14, 8, 4, 1); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)
			go func() {
				defer wg.Done()
				<-start
				if err := s.BindLiveAnchorStyleToPlan(ctx, 14, 7, 4, 1); err != nil {
					t.Error(err)
				}
			}()
			go func() {
				defer wg.Done()
				<-start
				if err := s.UnbindLiveAnchorStyleFromPlan(ctx, 14, 8, 4); err != nil && !errors.Is(err, ErrLiveAnchorBindingChanged) {
					t.Error(err)
				}
			}()
			close(start)
			wg.Wait()
			bound(4, 7)
		}
	})
}
