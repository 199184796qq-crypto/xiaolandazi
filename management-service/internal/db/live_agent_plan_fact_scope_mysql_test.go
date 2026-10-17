package db

import (
	"context"
	"encoding/json"
	"errors"
	"livecompanion/management/internal/model"
	"strings"
	"sync"
	"testing"
)

func TestFactScopeMySQLIsolationCASAuditAndRollback(t *testing.T) {
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
		if len(f) > 5 && (f[5] == "live_agent_plans" || f[5] == "live_agent_plan_facts" || f[5] == "live_agent_plan_fact_revisions" || f[5] == "live_agent_plan_product_links") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id,name,description) VALUES(4,14,'当前',''),(5,14,'另一个','')`)
	exec(`INSERT INTO live_agent_plan_product_links(id,tenant_id,plan_id,link_key,product_name,source_quote) VALUES(21,14,4,'1号链接','产品一',''),(22,14,5,'2号链接','其他方案','')`)
	exec(`INSERT INTO live_agent_plan_facts(id,tenant_id,plan_id,fact_key,fact_value,source_quote) VALUES(7,14,4,'使用条件','每桌一券，周末不可用','原条件')`)
	original, err := s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil || len(original) != 1 || original[0].Scope != nil {
		t.Fatal(original, err)
	}
	for _, target := range []struct{ tenant, plan, product int64 }{{15, 4, 21}, {14, 5, 21}, {14, 4, 22}, {14, 4, 999}} {
		_, err = s.UpdateLiveAgentPlanFactScope(ctx, target.tenant, target.plan, 7, 8, 1, model.LiveAgentPlanFactScope{Kind: "product", ProductLinkID: target.product})
		if err == nil {
			t.Fatal("cross-scope or missing product accepted", target)
		}
	}
	for _, scope := range []model.LiveAgentPlanFactScope{{Kind: ""}, {Kind: "common", ProductLinkID: 21}, {Kind: "product"}, {Kind: "unknown"}} {
		if _, err = s.UpdateLiveAgentPlanFactScope(ctx, 14, 4, 7, 8, 1, scope); !errors.Is(err, ErrLiveFactScopeInvalid) {
			t.Fatal(scope, err)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.UpdateLiveAgentPlanFactScope(ctx, 14, 4, 7, 8, 1, model.LiveAgentPlanFactScope{Kind: "product", ProductLinkID: 21})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrLiveAgentPlanFactVersionConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	facts, err := s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil || facts[0].Scope.ProductLinkID != 21 || facts[0].Value != original[0].Value || facts[0].VersionNo != 2 {
		t.Fatal(facts, err)
	}
	same, err := s.UpdateLiveAgentPlanFactScope(ctx, 14, 4, 7, 8, 2, *facts[0].Scope)
	if err != nil || same.VersionNo != 2 {
		t.Fatal("identical save changed version", same, err)
	}
	updated, err := s.UpdateLiveAgentPlanFactWithExpectedVersion(ctx, 14, 4, 7, 8, 2, "other", "使用条件", "每桌一券，周末及节假日不可用", "", "保留使用条件")
	if err != nil || updated.Scope.ProductLinkID != 21 {
		t.Fatal("content edit cleared scope", updated, err)
	}
	exec(`CREATE TRIGGER fail_scope_audit BEFORE INSERT ON live_agent_plan_fact_revisions FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='isolated audit failure'`)
	if _, err = s.UpdateLiveAgentPlanFactScope(ctx, 14, 4, 7, 8, 3, model.LiveAgentPlanFactScope{Kind: "common"}); err == nil {
		t.Fatal("audit failure accepted")
	}
	exec(`DROP TRIGGER fail_scope_audit`)
	facts, err = s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil || facts[0].VersionNo != 3 || facts[0].Scope.ProductLinkID != 21 {
		t.Fatal("failed audit did not roll back", facts, err)
	}
	updated, err = s.UpdateLiveAgentPlanFactScope(ctx, 14, 4, 7, 8, 3, model.LiveAgentPlanFactScope{Kind: "common"})
	if err != nil || updated.VersionNo != 4 || updated.Scope.Kind != "common" {
		t.Fatal(updated, err)
	}
	if err = s.DeleteLiveAgentPlanFactWithExpectedVersion(ctx, 14, 4, 7, 8, 4); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT action,scope_json FROM live_agent_plan_fact_revisions WHERE fact_id=7 ORDER BY version_no`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var action, raw string
		if err = rows.Scan(&action, &raw); err != nil {
			t.Fatal(err)
		}
		var scope model.LiveAgentPlanFactScope
		if json.Unmarshal([]byte(raw), &scope) != nil {
			t.Fatal(raw)
		}
		if count < 2 && scope.ProductLinkID != 21 || count >= 2 && scope.Kind != "common" {
			t.Fatal("scope history lost", action, scope)
		}
		count++
	}
	if count != 4 || rows.Err() != nil {
		t.Fatal(count, rows.Err())
	}
}
