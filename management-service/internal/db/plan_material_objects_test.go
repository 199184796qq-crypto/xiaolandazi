package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
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
		if strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") || strings.HasPrefix(statement, "CREATE TABLE IF NOT EXISTS live_agent_plan_qa_") {
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

	apply := func(action, kind string, id int64, name string) model.MaterialObjectCatalog {
		t.Helper()
		current, err := s.PlanMaterialObjects(ctx, 14, 4)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ChangePlanMaterialObject(ctx, 14, 4, MaterialObjectChange{Revision: current.Revision, Action: action, Kind: kind, ID: id, Name: name}, map[int64]string{101: "1号链接"}, 1); err != nil {
			t.Fatal(err)
		}
		current, err = s.PlanMaterialObjects(ctx, 14, 4)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	first := apply("add", "object", 0, "1号套餐").Directory.Objects[0].ID
	second := apply("add", "object", 0, "2号套餐").Directory.Objects[1].ID
	if first <= 0 || second <= 0 || first == second {
		t.Fatal("invalid object IDs", first, second)
	}
	firstTarget := model.MaterialObjectTarget{Kind: "object", ObjectID: first}
	secondTarget := model.MaterialObjectTarget{Kind: "object", ObjectID: second}
	solo := materialInput()
	solo.ScopeKind, solo.ProductLinkID, solo.StoreName = "object", 0, ""
	solo.ObjectTargets = []model.MaterialObjectTarget{firstTarget}
	solo.Content, solo.SourceSentence = "独占属性", "本套餐提供独占属性"
	shared := solo
	shared.Content = "共享属性"
	shared.ObjectTargets = []model.MaterialObjectTarget{firstTarget, secondTarget}
	extra, other := solo, solo
	extra.Content = "额外属性"
	other.Content, other.ObjectTargets = "另一个对象的属性", []model.MaterialObjectTarget{secondTarget}
	if n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{solo, shared, extra, other}, "", nil); n != 4 || err != nil {
		t.Fatalf("confirm objects: %d %v", n, err)
	}
	beforeOrder, err := s.PlanMaterialObjects(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	order := []string{materialObjectOrderKey("object", second), "product:101", materialObjectOrderKey("object", first)}
	change := MaterialObjectChange{Revision: beforeOrder.Revision, Action: "reorder", DisplayOrder: order}
	links := map[int64]string{101: "1号链接"}
	if err := s.ChangePlanMaterialObject(ctx, 14, 4, change, links, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePlanMaterialObject(ctx, 14, 4, change, links, 0); !errors.Is(err, ErrMaterialObjectsConflict) {
		t.Fatalf("stale reorder accepted: %v", err)
	}
	sorted, err := s.PlanMaterialObjects(ctx, 14, 4)
	if err != nil || sorted.Revision != beforeOrder.Revision+1 || !reflect.DeepEqual(sorted.Directory.DisplayOrder, order) || !reflect.DeepEqual(sorted.Stores, stores) {
		t.Fatalf("reorder did not round trip: %+v %v", sorted, err)
	}
	rows, err = s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || !reflect.DeepEqual(rows, beforeRows) {
		t.Fatalf("reorder changed claim records: %+v %v", rows, err)
	}
	displayIDs := func() []int64 {
		t.Helper()
		rows, err := s.ListPlanMaterialClaimsForDisplay(ctx, 14, 4)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]int64, len(rows))
		for i, row := range rows {
			ids[i] = row.ID
		}
		return ids
	}
	// An order from the previous release is the initial order for each object.
	reversed := displayIDs()
	slices.Reverse(reversed)
	baseOrder, err := json.Marshal(reversed)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE live_agent_plan_material_objects SET directory_json=JSON_SET(directory_json,'$.fact_display_order',CAST(? AS JSON)) WHERE tenant_id=14 AND plan_id=4`, string(baseOrder))
	if shown := displayIDs(); !slices.Equal(shown, reversed) {
		t.Fatal("existing display order differs", shown)
	}
	selector := func(target model.MaterialObjectTarget) func([]model.PlanMaterialClaim, model.MaterialObjectDirectory) []model.PlanMaterialClaim {
		return func(rows []model.PlanMaterialClaim, _ model.MaterialObjectDirectory) []model.PlanMaterialClaim {
			var matches []model.PlanMaterialClaim
			for _, row := range rows {
				if slices.Contains(row.ObjectTargets, target) {
					matches = append(matches, row)
				}
			}
			return matches
		}
	}
	scoped := func(target model.MaterialObjectTarget) MaterialFactOrderChange {
		var previous []int64
		for _, row := range materialClaimsInDisplayOrder(selector(target)(beforeRows, model.MaterialObjectDirectory{}), reversed) {
			previous = append(previous, row.ID)
		}
		order := slices.Clone(previous)
		slices.Reverse(order)
		return MaterialFactOrderChange{
			ObjectKey: materialObjectOrderKey("object", target.ObjectID), LibraryRevision: MaterialLibraryRevision(beforeRows),
			Previous: previous, Order: order,
		}
	}
	factOrder, secondOrder := scoped(firstTarget), scoped(secondTarget)
	invalid := factOrder
	invalid.Order = []int64{factOrder.Order[0], factOrder.Order[0], factOrder.Order[2]}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, invalid, selector(firstTarget)); !errors.Is(err, ErrMaterialFactOrderInvalid) {
		t.Fatal("duplicate fact ID accepted", err)
	}
	invalid.Order = []int64{factOrder.Order[0], factOrder.Order[1], secondOrder.Previous[1]}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, invalid, selector(firstTarget)); !errors.Is(err, ErrMaterialFactOrderInvalid) {
		t.Fatal("another object's fact ID accepted", err)
	}
	invalid.ObjectKey = "all"
	if _, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, invalid, selector(firstTarget)); !errors.Is(err, ErrMaterialFactOrderInvalid) {
		t.Fatal("global order accepted", err)
	}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 15, 4, factOrder, selector(firstTarget)); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant order accepted", err)
	}
	for i := 0; i < 2; i++ {
		saved, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, factOrder, selector(firstTarget))
		if err != nil || !slices.Equal(saved.Directory.FactDisplayOrders[factOrder.ObjectKey], factOrder.Order) || len(saved.Directory.FactDisplayOrders) != 1 {
			t.Fatal("save or retry did not preserve object scope", saved, err)
		}
	}
	saved, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, secondOrder, selector(secondTarget))
	if err != nil || !slices.Equal(saved.Directory.FactDisplayOrders[secondOrder.ObjectKey], secondOrder.Order) ||
		!slices.Equal(saved.Directory.FactDisplayOrders[factOrder.ObjectKey], factOrder.Order) || !slices.Equal(displayIDs(), reversed) {
		t.Fatal("overlapping objects did not retain independent orders", saved, err)
	}
	legacyFirst, legacySecond := materialInput(), materialInput()
	legacySecond.Content = "另一条"
	if _, err := s.ReplacePlanMaterialClaims(ctx, 15, 5, 8, []model.PlanMaterialClaimInput{legacyFirst, legacySecond}, "fixture", [][]float32{{1, 2}, {3, 4}}); err != nil {
		t.Fatal(err)
	}
	legacyRows, err := s.ListPlanMaterialClaimsForDisplay(ctx, 15, 5)
	if err != nil || len(legacyRows) != 2 {
		t.Fatalf("first display without a directory: %d %v", len(legacyRows), err)
	}
	legacyIDs := []int64{legacyRows[0].ID, legacyRows[1].ID}
	legacyOrder := MaterialFactOrderChange{
		ObjectKey:       "store:" + legacyFirst.StoreName,
		LibraryRevision: MaterialLibraryRevision(legacyRows), Previous: legacyIDs, Order: []int64{legacyIDs[1], legacyIDs[0]},
	}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 15, 5, legacyOrder, func(rows []model.PlanMaterialClaim, _ model.MaterialObjectDirectory) []model.PlanMaterialClaim {
		return rows
	}); err != nil {
		t.Fatal("first sort without a directory failed", err)
	}
	legacyCatalog, err := s.PlanMaterialObjects(ctx, 15, 5)
	if err != nil || legacyCatalog.Revision != 1 || !slices.Equal(legacyCatalog.Directory.FactDisplayOrders[legacyOrder.ObjectKey], legacyOrder.Order) {
		t.Fatalf("first sort did not initialize the directory: %+v %v", legacyCatalog, err)
	}
	otherEdit := factOrder
	otherEdit.Order = []int64{factOrder.Previous[1], factOrder.Previous[2], factOrder.Previous[0]}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, otherEdit, selector(firstTarget)); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("stale alternative order accepted", err)
	}
	plainRows, err := s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || !reflect.DeepEqual(plainRows, beforeRows) {
		t.Fatal("display order modified records or the default list", err)
	}
	renamed := apply("rename", "object", first, "1号礼盒").Directory
	if got := renamed.Objects[0]; got.ID != first || got.DisplayName != "1号礼盒" {
		t.Fatal("rename changed identity", got)
	}
	if !slices.Equal(renamed.FactDisplayOrders[factOrder.ObjectKey], factOrder.Order) {
		t.Fatal("rename discarded the object's fact order")
	}
	if got := apply("rename", "plan", 0, "通用资料"); got.Directory.GlobalDisplayName != "通用资料" || len(got.Stores) != len(stores) {
		t.Fatal("global rename changed store associations", got)
	}
	removed := apply("delete", "object", first, "").Directory
	if len(removed.Objects) != 1 || removed.Objects[0].ID != second || !reflect.DeepEqual(removed.DisplayOrder, order[:2]) ||
		len(removed.FactDisplayOrders) != 1 || !slices.Equal(removed.FactDisplayOrders[secondOrder.ObjectKey], secondOrder.Order) {
		t.Fatal("delete touched another object", removed)
	}
	rows, err = s.ListPlanMaterialClaims(ctx, 14, 4)
	if err != nil || len(rows) != len(reversed) {
		t.Fatalf("delete lost rows: %d %v", len(rows), err)
	}
	for _, row := range rows {
		switch row.Content {
		case solo.Content:
			if row.ScopeKind != "unresolved" || len(row.ObjectTargets) != 0 || row.SourceSentence != solo.SourceSentence {
				t.Fatal("unassigned claim lost scope or evidence", row)
			}
		case shared.Content:
			if !reflect.DeepEqual(row.ObjectTargets, []model.MaterialObjectTarget{secondTarget}) {
				t.Fatal("delete lost the other owner", row.ObjectTargets)
			}
		}
	}
	if _, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{solo}, "", nil); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatalf("stale object was accepted: %v", err)
	}
	legacy := materialInput()
	legacy.Content = "原链接属性"
	legacy.ScopeKind, legacy.ProductLinkID, legacy.StoreName = "product", 101, ""
	legacy.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}}
	if n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{legacy}, "", nil); n != 1 || err != nil {
		t.Fatalf("confirm legacy link: %d %v", n, err)
	}
	afterInsert := displayIDs()
	if len(afterInsert) != len(reversed)+1 || !slices.Equal(afterInsert[:len(reversed)], reversed) {
		t.Fatal("later fact did not append or object edits discarded the order", afterInsert)
	}
	if _, err := s.ReorderPlanMaterialFacts(ctx, 14, 4, factOrder, selector(firstTarget)); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("new fact did not invalidate old snapshot", err)
	}
	if got := apply("delete", "product", 101, ""); !reflect.DeepEqual(got.Directory.RemovedLinkIDs, []int64{101}) || !reflect.DeepEqual(got.Stores, stores) {
		t.Fatal("legacy removal changed store associations", got)
	}
	if _, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{legacy}, "", nil); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatalf("removed link was accepted: %v", err)
	}
}

func TestValidMaterialObjectOrder(t *testing.T) {
	dir := model.MaterialObjectDirectory{
		Objects:        []model.MaterialNamedObject{{ID: 42}, {ID: 43}},
		RemovedLinkIDs: []int64{12},
	}
	links := map[int64]string{42: "原商品", 12: "已删除商品"}
	if !validMaterialObjectOrder(dir, []string{"object:43", "product:42", "object:42"}, links) {
		t.Fatal("valid cross-kind order rejected")
	}
	for _, order := range [][]string{
		{"object:42", "product:42"},
		{"object:42", "object:42", "product:42"},
		{"object:42", "object:43", "product:12"},
		{"object:42", "object:43", "plan"},
		{"object:42", "object:43", "product:404"},
	} {
		if validMaterialObjectOrder(dir, order, links) {
			t.Errorf("invalid order accepted: %v", order)
		}
	}
}
