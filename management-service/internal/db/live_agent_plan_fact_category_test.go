package db

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestFactCategoryMySQLCASIsolationAuditAndRollback(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, a...); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(q)
		if len(f) > 5 && (f[5] == "live_agent_plans" || f[5] == "live_agent_plan_facts" || f[5] == "live_agent_plan_fact_revisions") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'当前',''),(5,14,'另一方案','')`)
	exec(`INSERT INTO live_agent_plan_facts(id,tenant_id,plan_id,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,scope_json) VALUES(7,14,4,'other','发货','次日上午发货','不要承诺次日到','保留时间条件','原资料',JSON_OBJECT('kind','common'))`)
	original, err := s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range [][3]int64{{15, 4, 7}, {14, 5, 7}, {14, 4, 999}} {
		if _, err = s.UpdateLiveAgentPlanFactCategory(ctx, target[0], target[1], target[2], 8, 1, "fulfillment"); err == nil {
			t.Fatal("cross scope accepted", target)
		}
	}
	if _, err = s.UpdateLiveAgentPlanFactCategory(ctx, 14, 4, 7, 8, 1, ""); !errors.Is(err, ErrLiveFactCategoryInvalid) {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.UpdateLiveAgentPlanFactCategory(ctx, 14, 4, 7, 8, 1, "fulfillment")
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrLiveAgentPlanFactVersionConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	got, err := s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	before, after := original[0], got[0]
	after.Category = before.Category
	after.VersionNo = before.VersionNo
	after.UpdatedAt = before.UpdatedAt
	after.UpdatedByUserID = before.UpdatedByUserID
	if !reflect.DeepEqual(before, after) {
		t.Fatal("category-only save changed other fields", before, after)
	}
	same, err := s.UpdateLiveAgentPlanFactCategory(ctx, 14, 4, 7, 8, 2, "fulfillment")
	if err != nil || same.VersionNo != 2 {
		t.Fatal(same, err)
	}
	var count int
	if err = s.db.QueryRow(`SELECT COUNT(*) FROM live_agent_plan_fact_revisions WHERE fact_id=7 AND action='category' AND version_no=2 AND category='fulfillment' AND JSON_UNQUOTE(JSON_EXTRACT(scope_json,'$.kind'))='common'`).Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	exec(`CREATE TRIGGER fail_category_audit BEFORE INSERT ON live_agent_plan_fact_revisions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated audit failure'`)
	if _, err = s.UpdateLiveAgentPlanFactCategory(ctx, 14, 4, 7, 8, 2, "other"); err == nil {
		t.Fatal("audit failure accepted")
	}
	exec(`DROP TRIGGER fail_category_audit`)
	got, err = s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil || got[0].Category != "fulfillment" || got[0].VersionNo != 2 {
		t.Fatal("audit failure did not roll back", got, err)
	}
}
