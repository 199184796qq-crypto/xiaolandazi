package db

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"livecompanion/management/internal/model"
)

func TestMaterialObjectsMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires fully isolated schema")
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
	for _, statement := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(statement)
		}
	}
	exec(`INSERT INTO live_agent_plans(id,tenant_id) VALUES(4,14),(5,15),(6,14)`)
	stores := []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{101, 102}}, {Name: "李家店", LinkIDs: []int64{103, 104}}}
	rev, err := s.SavePlanMaterialObjects(ctx, 14, 4, 0, stores)
	if err != nil || rev != 1 {
		t.Fatalf("save %d %v", rev, err)
	}
	got, err := s.PlanMaterialObjects(ctx, 14, 4)
	if err != nil || got.Revision != 1 || !reflect.DeepEqual(got.Stores, stores) {
		t.Fatalf("read %+v %v", got, err)
	}
	if _, err = s.SavePlanMaterialObjects(ctx, 14, 4, 0, stores); !errors.Is(err, ErrMaterialObjectsConflict) {
		t.Fatal("stale revision accepted")
	}
	if _, err = s.SavePlanMaterialObjects(ctx, 15, 4, 1, stores); err == nil {
		t.Fatal("foreign plan save accepted")
	}
	// One service attribute, two target links, exactly one vector/row.
	claim := materialInput()
	claim.ScopeKind = "product"
	claim.StoreName = ""
	claim.ProductLinkID = 101
	claim.Content = "支持切块"
	claim.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101, LinkKey: "1号链接"}, {Kind: "product", LinkID: 102, LinkKey: "2号链接"}}
	n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{claim}, "fixture", [][]float32{{1, 2}})
	if err != nil || n != 1 {
		t.Fatalf("multi insert %d %v", n, err)
	}
	rows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0].ObjectTargets, claim.ObjectTargets) {
		t.Fatalf("multi roundtrip %+v %v", rows, err)
	}
	id := rows[0].ID
	claim.ObjectTargets = []model.MaterialObjectTarget{{Kind: "store", StoreName: "王家店"}}
	claim.ScopeKind = "store"
	claim.StoreName = "王家店"
	claim.ProductLinkID = 0
	vector, _ := json.Marshal([]float32{2, 3})
	if err = s.UpdatePlanMaterialClaim(ctx, 14, 4, id, claim, "fixture", vector); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(rows) != 1 || rows[0].ID != id || len(rows[0].ObjectTargets) != 1 {
		t.Fatal("scope edit cloned row")
	}
	if _, err = s.SavePlanMaterialObjects(ctx, 14, 4, 1, stores[1:]); !errors.Is(err, ErrMaterialObjectsInUse) {
		t.Fatalf("used store removed: %v", err)
	}
	got, _ = s.PlanMaterialObjects(ctx, 14, 4)
	if got.Revision != 1 {
		t.Fatal("failed save changed catalog")
	}
	stores[0].LinkIDs = []int64{102}
	rev, err = s.SavePlanMaterialObjects(ctx, 14, 4, 1, stores)
	if err != nil || rev != 2 {
		t.Fatal(err)
	}
	var raw []byte
	_ = s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, id).Scan(&raw)
	if string(raw) != "[2, 3]" && string(raw) != "[2,3]" {
		t.Fatal("relation save rewrote vector")
	}
	// Parent locking serializes first saves; exactly one optimistic revision wins.
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.SavePlanMaterialObjects(ctx, 14, 6, 0, stores); results <- e }()
	}
	wg.Wait()
	close(results)
	winners := 0
	for e := range results {
		if e == nil {
			winners++
		} else if !errors.Is(e, ErrMaterialObjectsConflict) {
			t.Fatal(e)
		}
	}
	if winners != 1 {
		t.Fatalf("first save winners %d", winners)
	}
	t.Log("catalog roundtrip, CAS, tenant isolation, single-row multi-target/vector, stable ID edit, used-store guard, relation without reembedding, concurrent first save passed")
}
