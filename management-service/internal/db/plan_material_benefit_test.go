package db

import (
	"context"
	"errors"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
)

func TestPlanMaterialBenefitChangesMetadataWithoutReplacingVector(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") {
			if _, err := s.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO live_agent_plans VALUES(4,14)`); err != nil {
		t.Fatal(err)
	}
	if n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{materialInput()}, "qwen", [][]float32{{1, 2}}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	items, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	id, oldRevision := items[0].ID, MaterialClaimRevision(items[0])
	var oldVector string
	if err := s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, id).Scan(&oldVector); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlanMaterialClaimBenefit(ctx, 14, 4, id, oldRevision, true); err != nil {
		t.Fatal(err)
	}
	items, err = s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(items) != 1 || !items[0].IsBenefit {
		t.Fatal(items, err)
	}
	var currentVector string
	if err := s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, id).Scan(&currentVector); err != nil || currentVector != oldVector {
		t.Fatal("benefit edit changed embedding", err)
	}
	if err := s.SetPlanMaterialClaimBenefit(ctx, 14, 4, id, oldRevision, false); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("stale edit was accepted", err)
	}
	if err := s.SetPlanMaterialClaimBenefit(ctx, 14, 4, id, MaterialClaimRevision(items[0]), false); err != nil {
		t.Fatal(err)
	}
	items, err = s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || items[0].IsBenefit {
		t.Fatal("benefit edit did not clear", err)
	}
}
