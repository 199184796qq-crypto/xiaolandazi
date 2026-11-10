package db

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"reflect"
	"strings"
	"testing"
)

func TestMaterialWorkflowCollision(t *testing.T) {
	makeClaim := func(value string, id int64) model.PlanMaterialClaimInput {
		return model.PlanMaterialClaimInput{ClaimKey: "price", Topic: "价格", Content: value, Value: value, Polarity: "positive", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: id}}}}
	}
	a := makeClaim("87.6元", 101)
	b := makeClaim("88.6元", 101)
	if !MaterialClaimCollision(a, b, nil) {
		t.Fatal("same object's price not matched")
	}
	b = makeClaim("88.6元", 102)
	if MaterialClaimCollision(a, b, nil) {
		t.Fatal("independent card price matched")
	}
	b = makeClaim("87.6元", 102)
	if MaterialClaimCollision(a, b, nil) {
		t.Fatal("equal text on separate objects matched")
	}
	b.Conditions = []byte(`[{"field":"amount","op":"gte","values":[99]}]`)
	if MaterialClaimCollision(a, b, nil) {
		t.Fatal("different conditions collapsed")
	}
	b = makeClaim("88.6元", 102)
	a.ObjectTargets = []model.MaterialObjectTarget{{Kind: "store", StoreName: "王家店"}}
	if MaterialClaimCollision(a, b, []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{101, 102}}}) {
		t.Fatal("store and its card are distinct objects")
	}
	a.ObjectTargets = []model.MaterialObjectTarget{{Kind: "plan"}}
	if MaterialClaimCollision(a, b, nil) {
		t.Fatal("global inheritance treated as explicit same owner")
	}
	b.ObjectTargets = []model.MaterialObjectTarget{{Kind: "plan"}}
	if !MaterialClaimCollision(a, b, nil) {
		t.Fatal("same global attribute omitted")
	}
	a.ObjectTargets = []model.MaterialObjectTarget{}
	b.ObjectTargets = []model.MaterialObjectTarget{}
	if MaterialClaimCollision(a, b, nil) {
		t.Fatal("pending ownership is not an object")
	}
	a = makeClaim("87.6元", 101)
	a.ObjectTargets = append(a.ObjectTargets, model.MaterialObjectTarget{Kind: "product", LinkID: 102})
	b = makeClaim("88.6元", 102)
	if !MaterialClaimCollision(a, b, nil) {
		t.Fatal("explicit shared owner omitted")
	}
	one := model.PlanMaterialClaim{ID: 1, Content: "a"}
	two := model.PlanMaterialClaim{ID: 2, Content: "b"}
	if MaterialLibraryRevision([]model.PlanMaterialClaim{one, two}) != MaterialLibraryRevision([]model.PlanMaterialClaim{two, one}) {
		t.Fatal("snapshot depends on read order")
	}
	two.Content = "changed"
	if MaterialLibraryRevision([]model.PlanMaterialClaim{one, two}) == MaterialLibraryRevision([]model.PlanMaterialClaim{one}) {
		t.Fatal("snapshot omitted a row")
	}
}

func TestMaterialDuplicateRequiresExactValueAndOriginal(t *testing.T) {
	a := model.PlanMaterialClaimInput{ClaimKey: "price", Topic: "价格", Value: "87.6元", Content: "价格87.6元", SourceQuote: "原文报价87.6元", MaterialClaimDetails: model.MaterialClaimDetails{Destination: "fact", ObjectTargets: []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}}}}
	b := a
	b.Content = "目前售价87.6元"
	if !MaterialClaimDuplicate(a, b) {
		t.Fatal("same value and original not detected")
	}
	b.Value = "88.6元"
	if MaterialClaimDuplicate(a, b) || !MaterialClaimCollision(a, b, nil) {
		t.Fatal("duplicate and replacement rules conflated")
	}
	b = a
	b.SourceQuote = "另一份原文"
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("different source duplicated")
	}
	b = a
	b.Value = "87.6元 "
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("value silently normalized")
	}
	b = a
	b.SourceQuote = "原文报价87.6元 "
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("source silently normalized")
	}
	b = a
	b.SourceSentence = a.SourceQuote
	b.SourceQuote = "shorter evidence excerpt"
	if !MaterialClaimDuplicate(a, b) {
		t.Fatal("displayed original fallback not supported")
	}
	b = a
	b.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 102}}
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("cross object duplicate")
	}
	b = a
	a.SourceQuote = ""
	b.SourceQuote = ""
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("missing originals treated as equal")
	}
	a.SourceQuote = "原文"
	b.SourceQuote = "原文"
	a.Value = ""
	b.Value = ""
	if MaterialClaimDuplicate(a, b) {
		t.Fatal("missing values treated as equal")
	}
}

func TestMaterialWorkflowMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires fully isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	stores := []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{101, 102}}}
	rev, err := s.SavePlanMaterialObjects(ctx, 14, 4, 0, stores)
	if err != nil {
		t.Fatal(err)
	}
	original := materialInput()
	original.ClientID = "old"
	n, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{original}, "fixture", [][]float32{{1, 2}})
	if n != 1 || err != nil {
		t.Fatal(n, err)
	}
	rows, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	snapshot := MaterialLibraryRevision(rows)
	oldID := rows[0].ID
	replacement := original
	replacement.ClientID = "replacement"
	replacement.Content = "替换后的回答"
	extra := original
	extra.ClientID = "extra"
	extra.ClaimKey = "spec"
	extra.Content = "新属性"
	exec(`CREATE TRIGGER reject_workflow_delete BEFORE DELETE ON live_agent_plan_material_claims FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test rollback'`)
	if _, err = s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, rev, []model.PlanMaterialClaimInput{replacement, extra}, []int64{oldID}, "fixture", [][]float32{{3, 4}, {5, 6}}); err == nil {
		t.Fatal("delete error committed")
	}
	after, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(after) != 1 || MaterialLibraryRevision(after) != snapshot {
		t.Fatal("partial append/replacement committed")
	}
	if _, err = s.ClearPlanMaterialLibrary(ctx, 14, 4, snapshot); err == nil {
		t.Fatal("clear error committed")
	}
	exec(`DROP TRIGGER reject_workflow_delete`)
	if _, err = s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, rev+1, nil, nil, "", nil); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("changed object relation accepted", err)
	}
	if n, err = s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, rev, nil, nil, "", nil); n != 0 || err != nil {
		t.Fatal("keep-only import failed", n, err)
	}
	if n, err = s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, rev, []model.PlanMaterialClaimInput{replacement, extra}, []int64{oldID}, "fixture", [][]float32{{3, 4}, {5, 6}}); n != 2 || err != nil {
		t.Fatal(n, err)
	}
	after, _ = s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(after) != 2 {
		t.Fatal("append/replacement failed")
	}
	for _, it := range after {
		if it.ID == oldID {
			t.Fatal("replaced old row retained")
		}
	}
	if _, err = s.ClearPlanMaterialLibrary(ctx, 14, 4, snapshot); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("stale clear accepted", err)
	}
	if _, err = s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, rev, nil, nil, "", nil); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("stale import accepted", err)
	}
	foreign := original
	foreign.Content = "其它租户保留"
	if _, err = s.ReplacePlanMaterialClaims(ctx, 15, 5, 9, []model.PlanMaterialClaimInput{foreign}, "fixture", [][]float32{{1, 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClearPlanMaterialLibrary(ctx, 15, 5, MaterialLibraryRevision(after)); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("foreign snapshot accepted", err)
	}
	if n, err := s.ClearPlanMaterialLibrary(ctx, 14, 4, MaterialLibraryRevision(after)); n != 2 || err != nil {
		t.Fatal(n, err)
	}
	empty, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	other, _ := s.ListPlanMaterialClaims(ctx, 15, 5)
	catalog, _ := s.PlanMaterialObjects(ctx, 14, 4)
	if len(empty) != 0 || len(other) != 1 || !reflect.DeepEqual(catalog.Stores, stores) {
		t.Fatal("clear escaped material library scope")
	}
	t.Log("append+explicit replace atomicity, delete/clear rollback, all-keep zero vector, library/object CAS, tenant isolation, scoped clear, relationship preservation passed")
}

func TestMaterialSplitMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires fully isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	old := materialInput()
	old.Content = "微波炉1分钟或空气炸锅3分钟"
	old.Value = old.Content
	old.ClaimKey = "heating"
	old.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 102}}
	other := materialInput()
	other.ClaimKey = "stock"
	other.Content = "其它资料保持"
	other.Value = other.Content
	if _, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{old, other}, "fixture", [][]float32{{1, 2}, {3, 4}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplacePlanMaterialClaims(ctx, 15, 5, 9, []model.PlanMaterialClaimInput{other}, "fixture", [][]float32{{5, 6}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	snapshot := MaterialLibraryRevision(rows)
	var oldID, otherID int64
	for _, r := range rows {
		if r.ClaimKey == "heating" {
			oldID = r.ID
		} else {
			otherID = r.ID
		}
	}
	vector := func(id int64) string {
		t.Helper()
		var v string
		if err := s.db.QueryRowContext(ctx, `SELECT embedding_json FROM live_agent_plan_material_claims WHERE id=?`, id).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	oldVector, otherVector := vector(oldID), vector(otherID)
	a, b := old, old
	a.Content = "微波炉1分钟"
	a.Value = a.Content
	b.Content = "空气炸锅3分钟"
	b.Value = b.Content
	items := []model.PlanMaterialClaimInput{a, b}
	vectors := [][]float32{{7, 8}, {9, 10}}
	unchanged := func() {
		t.Helper()
		now, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
		if MaterialLibraryRevision(now) != snapshot || vector(oldID) != oldVector || vector(otherID) != otherVector {
			t.Fatal("split failure changed original/body/vector")
		}
	}
	if _, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, 0, items, []int64{oldID}, "fixture", vectors[:1]); err == nil {
		t.Fatal("partial vectors accepted")
	}
	unchanged()
	exec(`CREATE TRIGGER reject_split_insert BEFORE INSERT ON live_agent_plan_material_claims FOR EACH ROW BEGIN IF NEW.content='空气炸锅3分钟' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test rollback'; END IF; END`)
	if _, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, 0, items, []int64{oldID}, "fixture", vectors); err == nil {
		t.Fatal("second child failure committed first")
	}
	unchanged()
	exec(`DROP TRIGGER reject_split_insert`)
	exec(`CREATE TRIGGER reject_split_delete BEFORE DELETE ON live_agent_plan_material_claims FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='test rollback'`)
	if _, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, 0, items, []int64{oldID}, "fixture", vectors); err == nil {
		t.Fatal("source deletion failure committed children")
	}
	unchanged()
	exec(`DROP TRIGGER reject_split_delete`)
	if _, err := s.ImportPlanMaterialLibrary(ctx, 15, 5, 9, snapshot, 0, items, []int64{oldID}, "fixture", vectors); !errors.Is(err, ErrMaterialClaimConflict) {
		t.Fatal("foreign source accepted", err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, 0, items, []int64{oldID}, "fixture", vectors)
			results <- err
		}()
	}
	wins, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			wins++
		} else if errors.Is(err, ErrMaterialClaimConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("concurrent split did not adopt once", wins, conflicts)
	}
	after, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(after) != 3 || vector(otherID) != otherVector {
		t.Fatal("wrong split result count or unrelated vector changed")
	}
	for _, r := range after {
		if r.ID == oldID {
			t.Fatal("original retained")
		}
		if r.ID != otherID {
			if len(r.ObjectTargets) != 2 || materialCanonical(r.Conditions) != materialCanonical(old.Conditions) || materialCanonical(r.Exceptions) != materialCanonical(old.Exceptions) {
				t.Fatal("owners/rules lost")
			}
		}
	}
	foreign, _ := s.ListPlanMaterialClaims(ctx, 15, 5)
	if len(foreign) != 1 {
		t.Fatal("foreign plan changed")
	}
	t.Log("two children+vectors atomically replace one source; insert/delete/vector failures roll back; multi-owner/rules/unrelated tenant and rows preserved; concurrent stale replay rejected")
}

func TestMaterialObjectReplacementMySQL(t *testing.T) {
	if *salesTestPrefixedTables {
		t.Fatal("requires isolated schema")
	}
	s := salesIsolatedMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14)`)
	old := materialInput()
	old.ObjectTargets = []model.MaterialObjectTarget{{Kind: "product", LinkID: 101}, {Kind: "product", LinkID: 102}}
	old.ScopeKind = "product"
	old.ProductLinkID = 101
	if _, err := s.ReplacePlanMaterialClaims(ctx, 14, 4, 8, []model.PlanMaterialClaimInput{old}, "fixture", [][]float32{{1, 2}}); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	snapshot := MaterialLibraryRevision(rows)
	a, b := old, old
	a.ObjectTargets = old.ObjectTargets[:1]
	a.Content = "A changed"
	a.Value = a.Content
	b.ObjectTargets = old.ObjectTargets[1:]
	b.ProductLinkID = 102
	if n, err := s.ImportPlanMaterialLibrary(ctx, 14, 4, 8, snapshot, 0, []model.PlanMaterialClaimInput{a, b}, []int64{rows[0].ID}, "fixture", [][]float32{{3, 4}, {5, 6}}); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	updated, _ := s.ListPlanMaterialClaims(ctx, 14, 4)
	if len(updated) != 2 {
		t.Fatal("partial scope replacement lost a row")
	}
	for _, it := range updated {
		if len(it.ObjectTargets) != 1 {
			t.Fatal("owners not partitioned")
		}
		switch it.ObjectTargets[0].LinkID {
		case 101:
			if it.Content != "A changed" {
				t.Fatal("chosen owner not replaced")
			}
		case 102:
			if it.Content != old.Content || it.Value != old.Value || string(it.Conditions) != string(old.Conditions) {
				t.Fatal("unrelated owner changed")
			}
		default:
			t.Fatal("scope leaked")
		}
	}
}
