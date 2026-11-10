package db

import (
	"context"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestMaterialRecallMySQL(t *testing.T) {
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
		if strings.HasPrefix(strings.TrimSpace(q), "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	input := materialInput()
	if _, e := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{input}, "qwen", [][]float32{{1, 2}}); e != nil {
		t.Fatal(e)
	}
	got, e := s.PlanMaterialRecallSnapshot(ctx, 14, 4)
	if e != nil || len(got) != 1 || got[0].Claim.Content != input.Content || len(got[0].Vector) != 2 {
		t.Fatal(got, e)
	}
	for _, pair := range [][2]int64{{15, 4}, {14, 5}, {15, 5}} {
		out, e := s.PlanMaterialRecallSnapshot(ctx, pair[0], pair[1])
		if e != nil || len(out) != 0 {
			t.Fatal("scope leak", out, e)
		}
	}
	exec(`UPDATE live_agent_plan_material_claims SET embedding_json=JSON_ARRAY(0,0) WHERE tenant_id=14 AND plan_id=4`)
	out, e := s.PlanMaterialRecallSnapshot(ctx, 14, 4)
	if e != nil || len(out) != 1 || len(out[0].Vector) != 0 || out[0].Claim.Content != input.Content {
		t.Fatal("invalid vector erased body or leaked", out, e)
	}
	var count int
	var vector string
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*),CAST(MAX(CAST(embedding_json AS CHAR)) AS CHAR) FROM live_agent_plan_material_claims WHERE tenant_id=14 AND plan_id=4`).Scan(&count, &vector); e != nil || count != 1 || !strings.Contains(vector, "0") {
		t.Fatal("read mutated data", count, vector, e)
	}
	exec(`UPDATE live_agent_plan_material_claims SET status='retired' WHERE tenant_id=14 AND plan_id=4`)
	out, e = s.PlanMaterialRecallSnapshot(ctx, 14, 4)
	if e != nil || len(out) != 0 {
		t.Fatal("inactive included", out, e)
	}
}
