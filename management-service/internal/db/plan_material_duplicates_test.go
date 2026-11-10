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

func TestMaterialDuplicateRevisions(t *testing.T) {
	a := model.PlanMaterialClaim{ID: 1, TenantID: 14, PlanID: 4, Content: "支持"}
	b := a
	b.ID = 2
	expected := []MaterialClaimExpectation{{1, MaterialClaimRevision(a)}, {2, MaterialClaimRevision(b)}}
	if CheckMaterialClaimExpectations([]model.PlanMaterialClaim{a, b}, expected, 1) != nil {
		t.Fatal("valid rejected")
	}
	for _, e := range [][]MaterialClaimExpectation{nil, {{1, ""}}, {{1, expected[0].Revision}, {1, expected[0].Revision}}, {{3, expected[0].Revision}}} {
		if CheckMaterialClaimExpectations([]model.PlanMaterialClaim{a, b}, e, 1) == nil {
			t.Fatal("bad expectations accepted")
		}
	}
	a.SourceQuote = "修改了出处"
	if CheckMaterialClaimExpectations([]model.PlanMaterialClaim{a, b}, expected, 1) == nil {
		t.Fatal("metadata race ignored")
	}
}

func TestMaterialDuplicatesMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := s.db.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	a := materialInput()
	a.ScopeKind = "product"
	a.StoreName = ""
	a.ProductLinkID = 101
	a.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101, LinkKey: "1号链接"}}
	b := a
	b.Content = "另一种说法"
	b.Value = b.Content
	b.ProductLinkID = 102
	b.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 102, LinkKey: "2号链接"}}
	n, e := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{a, b}, "fixture", [][]float32{{1, 2}, {3, 4}})
	if e != nil || n != 2 {
		t.Fatal(n, e)
	}
	rows, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	keep := rows[1].ID
	expectations := func(rows []model.PlanMaterialClaim) []MaterialClaimExpectation {
		out := []MaterialClaimExpectation{}
		for _, it := range rows {
			out = append(out, MaterialClaimExpectation{it.ID, MaterialClaimRevision(it)})
		}
		return out
	}
	expected := expectations(rows)
	merged := a
	merged.ObjectTargets = append(append([]model.MaterialObjectTarget{}, a.ObjectTargets...), b.ObjectTargets...)
	// Inject a DELETE failure after the keeper UPDATE. Both old rows must survive.
	exec(`CREATE TRIGGER reject_duplicate_delete BEFORE DELETE ON live_agent_plan_material_claims FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test rollback'`)
	if s.ResolvePlanMaterialDuplicates(ctx, 14, 4, keep, expected, merged, "new", []float32{5, 6}) == nil {
		t.Fatal("delete failure committed")
	}
	after, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(after) != 2 || MaterialClaimRevision(after[1]) != MaterialClaimRevision(rows[1]) {
		t.Fatal("partial keeper update committed")
	}
	exec(`DROP TRIGGER reject_duplicate_delete`)
	if err := s.ResolvePlanMaterialDuplicates(ctx, 15, 5, keep, expected, merged, "new", []float32{5, 6}); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("foreign rows accepted", err)
	}
	// Another editor's write invalidates the original comparison before any delete.
	raw, _ := json.Marshal([]float32{1, 2})
	changed := a
	changed.SourceQuote = "并发更新的出处"
	if e = s.UpdatePlanMaterialClaim(ctx, 14, 4, keep, changed, "fixture", raw); e != nil {
		t.Fatal(e)
	}
	if err := s.ResolvePlanMaterialDuplicates(ctx, 14, 4, keep, expected, merged, "new", []float32{5, 6}); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("stale comparison accepted", err)
	}
	rows, _ = s.ListPlanMaterialClaims(ctx, 14, 4)
	expected = expectations(rows)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- s.ResolvePlanMaterialDuplicates(ctx, 14, 4, keep, expected, merged, "new", []float32{5, 6})
		}()
	}
	wg.Wait()
	close(results)
	won := 0
	for e := range results {
		if e == nil {
			won++
		} else if !errors.Is(e, ErrMaterialClaimConflict) {
			t.Fatal(e)
		}
	}
	if won != 1 {
		t.Fatal("concurrent winners", won)
	}
	rows, _ = s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(rows) != 1 || rows[0].ID != keep || len(rows[0].ObjectTargets) != 2 {
		t.Fatal("not one retained row/two targets", rows)
	}
	var vector string
	_ = s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, keep).Scan(&vector)
	e = s.ResolvePlanMaterialDuplicates(ctx, 14, 4, keep, expectations(rows), dbInputForDuplicateTest(rows[0]), "", nil)
	if e != nil {
		t.Fatal(e)
	}
	var preserved string
	_ = s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, keep).Scan(&preserved)
	if vector != preserved {
		t.Fatal("unchanged vector repurchased/rewritten")
	}
	t.Log("atomic rollback, stale comparison, tenant isolation, concurrent single winner, stable ID, one multi-target vector and unchanged-vector preservation passed")
}
func dbInputForDuplicateTest(it model.PlanMaterialClaim) model.PlanMaterialClaimInput {
	return MaterialClaimInput(it)
}
