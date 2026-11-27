package db

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/qapair"
	"strings"
	"sync"
	"testing"
)

func TestQAPairsIsolatedMySQLAtomicIdempotentAndTenant(t *testing.T) {
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
	exec(`CREATE TABLE live_agent_plan_product_links(id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,link_key VARCHAR(100) NOT NULL,product_name VARCHAR(300) NOT NULL)`)
	for _, q := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		q = strings.TrimSpace(q)
		if strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_qa_") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_objects (") || strings.HasPrefix(q, "CREATE TABLE IF NOT EXISTS live_agent_plan_material_claims (") {
			exec(q)
		}
	}
	exec(`INSERT INTO live_agent_plans VALUES(4,14),(5,15)`)
	exec(`INSERT INTO live_agent_plan_product_links VALUES(11,14,4,'1','商品一'),(12,14,4,'2','商品二')`)
	snapshot := func() qapair.Catalog {
		t.Helper()
		rs, err := s.PlanQAPairs(ctx, 14, 4)
		if err != nil {
			t.Fatal(err)
		}
		_, _, rev, err := s.PlanQAObjects(ctx, 14, 4)
		if err != nil {
			t.Fatal(err)
		}
		return QACatalog(rs, rev)
	}
	first := snapshot()
	entry := qapair.Entry{Question: "观众问题", Answer: "第一版原始回答", Enabled: true, Targets: []model.MaterialObjectTarget{{Kind: "plan"}}}
	start := func(id string, cat qapair.Catalog, items []qapair.Mutation, remove []string) QAOperation {
		t.Helper()
		op, claimed, err := s.BeginQAOperation(ctx, 14, 4, 8, id, qapair.Hash(id), QAOperation{Revision: cat.Revision, ObjectRevision: cat.ObjectRevision, Writes: items, Remove: remove})
		if err != nil || !claimed {
			t.Fatal(id, op, claimed, err)
		}
		return op
	}
	op := start("first-operation-id", first, []qapair.Mutation{{Entry: entry}}, nil)
	if got := snapshot(); len(got.Items) != 0 {
		t.Fatal("unready version visible", got)
	}
	if _, err := s.CommitQAOperation(ctx, 14, 4, 8, "first-operation-id"); !errors.Is(err, ErrQAOperationPending) {
		t.Fatal("unready commit", err)
	}
	op.Writes[0].Model = "qwen"
	op.Writes[0].Space = qapair.SpaceVersion
	op.Writes[0].Vectors = [][]float32{{1, 0}}
	if err := s.QAOperationReady(ctx, 14, 4, "first-operation-id", op); err != nil {
		t.Fatal(err)
	}
	saved, err := s.CommitQAOperation(ctx, 14, 4, 8, "first-operation-id")
	if err != nil || len(saved.Items) != 1 || saved.Items[0].Answer != entry.Answer {
		t.Fatal(saved, err)
	}
	again, err := s.CommitQAOperation(ctx, 14, 4, 8, "first-operation-id")
	if err != nil || again.Revision != saved.Revision {
		t.Fatal("duplicate commit", again, err)
	}
	lookup, found, err := s.QAOperation(ctx, 14, 4, "first-operation-id", qapair.Hash("first-operation-id"))
	if err != nil || !found || lookup.State != "done" {
		t.Fatal(lookup, found, err)
	}
	if _, _, err = s.QAOperation(ctx, 14, 4, "first-operation-id", "changed-hash"); !errors.Is(err, ErrQAPairsConflict) {
		t.Fatal("operation reused with different payload", err)
	}
	foreign, err := s.PlanQAPairs(ctx, 15, 4)
	if err != nil || len(foreign) != 0 {
		t.Fatal("tenant leak", foreign, err)
	}
	if _, _, err = s.BeginQAOperation(ctx, 15, 4, 8, "forbidden-operation", qapair.Hash("x"), QAOperation{}); err == nil {
		t.Fatal("foreign plan mutation")
	}
	// Concurrent same-key requests acquire the plan lock and only one is owner.
	current := snapshot()
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, e := s.BeginQAOperation(ctx, 14, 4, 8, "concurrent-operation", qapair.Hash("same"), QAOperation{Revision: current.Revision, ObjectRevision: current.ObjectRevision})
			results <- claimed
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	owners := 0
	for yes := range results {
		if yes {
			owners++
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if owners != 1 {
		t.Fatal("paid ownership not unique", owners)
	}
	// A failure after the first revision/row is written rolls back all pointers,
	// deletion and vectors; the previously approved answer stays current.
	before := snapshot()
	badEntry := entry
	badEntry.Answer = "替换但不应生效"
	op = start("failed-atomic-operation", before, []qapair.Mutation{{Entry: badEntry}, {Entry: qapair.Entry{Question: "第二问", Answer: "第二答", Enabled: true, Targets: entry.Targets}}}, []string{saved.Items[0].ID})
	for i := range op.Writes {
		op.Writes[i].Model = "qwen"
		op.Writes[i].Space = qapair.SpaceVersion
		op.Writes[i].Vectors = [][]float32{{1, 0}}
	}
	if err = s.QAOperationReady(ctx, 14, 4, "failed-atomic-operation", op); err != nil {
		t.Fatal(err)
	}
	exec(`CREATE TRIGGER qa_fail_second BEFORE INSERT ON live_agent_plan_qa_revisions FOR EACH ROW BEGIN IF JSON_UNQUOTE(JSON_EXTRACT(NEW.content_json,'$.question'))='第二问' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='injected second row failure'; END IF; END`)
	if _, err = s.CommitQAOperation(ctx, 14, 4, 8, "failed-atomic-operation"); err == nil {
		t.Fatal("injected failure accepted")
	}
	if got := snapshot(); got.Revision != before.Revision {
		t.Fatal("atomic rollback failed", got)
	}
	exec(`DROP TRIGGER qa_fail_second`)
	if out, e := s.CommitQAOperation(ctx, 14, 4, 8, "failed-atomic-operation"); e != nil || len(out.Items) != 2 {
		t.Fatal("retry cached ready operation", out, e)
	}
	// Object changes after vector work reject the activation without deleting
	// the old answers. No recovery request needs another provider call.
	current = snapshot()
	op = start("objects-stale-operation", current, []qapair.Mutation{{Entry: entry}}, nil)
	op.Writes[0].Model = "qwen"
	op.Writes[0].Space = qapair.SpaceVersion
	op.Writes[0].Vectors = [][]float32{{1, 0}}
	if err = s.QAOperationReady(ctx, 14, 4, "objects-stale-operation", op); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE live_agent_plan_product_links SET link_key='renamed' WHERE id=11`)
	if _, err = s.CommitQAOperation(ctx, 14, 4, 8, "objects-stale-operation"); !errors.Is(err, ErrQAPairsConflict) {
		t.Fatal("object CAS", err)
	}
	if len(snapshot().Items) != 2 {
		t.Fatal("old answers lost after CAS")
	}
	current = snapshot()
	_ = start("unknown-operation-id", current, nil, nil)
	if err = s.QAOperationUncertain(ctx, 14, 4, "unknown-operation-id"); err != nil {
		t.Fatal(err)
	}
	state, _, err := s.QAOperation(ctx, 14, 4, "unknown-operation-id", qapair.Hash("unknown-operation-id"))
	if err != nil || state.State != "unknown" {
		t.Fatal(state, err)
	}
	// A referenced store cannot be removed through the material UI either.
	rev, err := s.SavePlanMaterialObjects(ctx, 14, 4, 0, []model.MaterialStoreObject{{Name: "王家店", LinkIDs: []int64{12}}})
	if err != nil {
		t.Fatal(err)
	}
	current = snapshot()
	storeEntry := entry
	storeEntry.Targets = []model.MaterialObjectTarget{{Kind: "store", StoreName: "王家店"}}
	storeEntry.Enabled = false
	op = start("store-reference-operation", current, []qapair.Mutation{{Entry: storeEntry}}, nil)
	if err = s.QAOperationReady(ctx, 14, 4, "store-reference-operation", op); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitQAOperation(ctx, 14, 4, 8, "store-reference-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SavePlanMaterialObjects(ctx, 14, 4, rev, nil); !errors.Is(err, ErrMaterialObjectsInUse) {
		t.Fatal("orphaned QA store", err)
	}
}
