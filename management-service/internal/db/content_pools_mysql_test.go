package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/contentpools"
)

func TestContentPoolsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	wanted := map[string]bool{}
	for _, name := range []string{"live_agent_plan_facts", "live_agent_plan_product_links", "live_agent_plan_product_attributes", "live_agent_plan_benefits", "live_selling_point_wheels"} {
		wanted[name] = true
	}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
			exec(raw)
			delete(wanted, fields[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal("missing actual source table fixtures", wanted)
	}
	for i := 0; i < 2; i++ {
		if err := s.MigrateContentPools(ctx); err != nil {
			t.Fatal("idempotent migration", err)
		}
	}
	for i := int64(1); i <= 20; i++ {
		exec(`INSERT INTO live_agent_plan_product_links(id,tenant_id,plan_id,link_key,product_name,source_quote) VALUES(?,14,4,?,?,'')`, i, fmt.Sprint(i), fmt.Sprintf("商品%d", i))
	}
	exec(`INSERT INTO live_agent_plan_product_links(id,tenant_id,plan_id,link_key,product_name,source_quote) VALUES(30,15,5,'30','其他租户','')`)
	for i := int64(1); i <= 3; i++ {
		exec(`INSERT INTO live_agent_plan_benefits(id,tenant_id,plan_id,benefit_key,link_key,activity_text,source_quote,status,validity_mode) VALUES(?,14,4,?,'1','福利','','active','permanent')`, i, fmt.Sprint(i))
	}
	rev, _ := s.ContentSourceRevision(ctx, 14, 4)
	if rev != 23 {
		t.Fatal("DML triggers did not count source writes", rev)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_product_links SET product_name='回滚编辑' WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	after, _ := s.ContentSourceRevision(ctx, 14, 4)
	if after != rev {
		t.Fatal("rolled-back source revision survived")
	}
	if err = s.SaveBenefitScope(ctx, 14, 4, 1, 1, BenefitScope{Products: []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveBenefitScope(ctx, 14, 4, 2, 1, BenefitScope{All: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveBenefitScope(ctx, 14, 4, 3, 1, BenefitScope{Products: []int64{20}}); err != nil {
		t.Fatal(err)
	}
	scopes, err := s.ListBenefitScopes(ctx, 14, 4)
	if err != nil || len(scopes) != 3 || len(scopes[1].Products) != 10 || !scopes[2].All || scopes[1].VersionNo != 1 {
		t.Fatal(scopes, err)
	}
	if err = s.SaveBenefitScope(ctx, 14, 4, 1, 1, BenefitScope{Products: []int64{1}, VersionNo: 0}); !errors.Is(err, ErrPoolSourceChanged) {
		t.Fatal("stale scope overwrote current", err)
	}
	if err = s.SaveBenefitScope(ctx, 14, 4, 1, 1, BenefitScope{Products: []int64{30}, VersionNo: 1}); err == nil {
		t.Fatal("cross-tenant scope accepted")
	}
	if scalar(`SELECT COUNT(*) FROM live_plan_benefit_scope_products WHERE benefit_id=1`) != 10 {
		t.Fatal("failed scope update was not atomic")
	}
	source := contentpools.Sources{ObservedAt: 100, Products: []contentpools.Product{}, Rows: []contentpools.SourceRow{{ID: "facts:1", Area: "knowledge", Label: "公共事实", Fields: map[string]string{"事实": "原值"}}}}
	for i := int64(1); i <= 20; i++ {
		source.Products = append(source.Products, contentpools.Product{ID: i, Link: fmt.Sprint(i)})
	}
	source.Revision, _ = s.ContentSourceRevision(ctx, 14, 4)
	baseline, err := s.ObserveContentSources(ctx, 14, 4, source)
	if err != nil || baseline.SHA() != source.SHA() {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := []PoolJob{}
	failures := []error{}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, e := s.ClaimPoolJob(ctx, 14, 4, 1, source)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				winners = append(winners, j)
			} else {
				failures = append(failures, e)
			}
		}()
	}
	wg.Wait()
	if len(winners) != 1 || len(failures) != 7 {
		t.Fatal("duplicate claims", winners, failures)
	}
	for _, e := range failures {
		if !errors.Is(e, ErrPoolJobRunning) {
			t.Fatal(e)
		}
	}
	ids := []int64{}
	for _, p := range source.Products {
		ids = append(ids, p.ID)
	}
	set := contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: "公共事实", Pools: []contentpools.Pool{{Key: "common", Products: ids, Facts: []contentpools.Fact{{ID: "f", Text: "公共事实", Sources: []string{"facts:1"}, Products: ids}}, Tasks: []contentpools.Task{{ID: "t", Point: "卖点", Angle: "角度", Sources: []string{"f"}}}}}}
	saved, err := s.CommitContentPoolSet(ctx, 14, 4, 1, winners[0], set)
	if err != nil {
		t.Fatal("atomic content set", err)
	}
	if scalar(`SELECT COUNT(*) FROM live_plan_content_task_products WHERE set_id=?`, saved.ID) != 20 {
		t.Fatal("not all products persisted")
	}
	if _, err = s.GetContentPoolSet(ctx, 15, 5, saved.ID); err == nil {
		t.Fatal("cross-tenant snapshot exposed")
	}
	exec(`UPDATE live_agent_plan_product_links SET product_name='资料又变了' WHERE id=1`)
	next := source
	next.Rows = []contentpools.SourceRow{{ID: "facts:1", Area: "knowledge", Label: "公共事实", Fields: map[string]string{"事实": "新值"}}}
	next.ObservedAt = 200
	next.Revision, _ = s.ContentSourceRevision(ctx, 14, 4)
	if _, err = s.ObserveContentSources(ctx, 14, 4, source); !errors.Is(err, ErrPoolSourceChanged) {
		t.Fatal("stale observation accepted", err)
	}
	if _, err = s.ObserveContentSources(ctx, 14, 4, next); err != nil {
		t.Fatal(err)
	}
	older := next
	older.ObservedAt = 150
	older.Rows = source.Rows
	if _, err = s.ObserveContentSources(ctx, 14, 4, older); err != nil {
		t.Fatal(err)
	}
	history, err := s.ContentChangeHistory(ctx, 14, 4)
	if err != nil || len(history) != 1 || history[0].Changes[0].After != "新值" {
		t.Fatal("observation went backwards", history, err)
	}
	snapshotTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	ref := map[string]any{"content_pools": map[string]any{"set_id": saved.ID, "source_sha": saved.SourceSHA, "source_revision": source.Revision}}
	if err = validatePoolSnapshotTx(ctx, snapshotTx, 14, 4, ref); !errors.Is(err, ErrPoolSourceChanged) {
		t.Fatal("stale publish allowed", err)
	}
	_ = snapshotTx.Rollback()
	snapshotTx, _ = s.db.BeginTx(ctx, nil)
	ref["content_pools"].(map[string]any)["voice_only"] = true
	if err = validatePoolSnapshotTx(ctx, snapshotTx, 14, 4, ref); err != nil {
		t.Fatal("frozen voice-only save changed content", err)
	}
	_ = snapshotTx.Rollback()
	currentJob, err := s.ClaimPoolJob(ctx, 14, 4, 1, next)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE live_agent_plan_product_links SET product_name='又一次编辑' WHERE id=1`)
	set.Sources = next
	set.SourceSHA = next.SHA()
	if _, err = s.CommitContentPoolSet(ctx, 14, 4, 1, currentJob, set); !errors.Is(err, ErrPoolSourceChanged) {
		t.Fatal("late result accepted", err)
	}
	if scalar(`SELECT COUNT(*) FROM live_plan_content_sets`) != 1 {
		t.Fatal("partial late draft persisted")
	}
	loaded, err := s.GetContentPoolSet(ctx, 14, 4, saved.ID)
	if err != nil || loaded.SourceSHA != source.SHA() {
		t.Fatal("saved immutable content changed", err)
	}
	exec(`UPDATE live_plan_content_jobs SET lease_until=DATE_SUB(CURRENT_TIMESTAMP(3),INTERVAL 1 MINUTE) WHERE id=?`, currentJob.ID)
	latest, err := s.LatestPoolJob(ctx, 14, 4)
	if err != nil || latest.Status != "failed" {
		t.Fatal("dead worker not retryable", latest, err)
	}
	if ok, _ := s.RenewPoolJob(ctx, 14, 4, currentJob.ID); ok {
		t.Fatal("dead job renewed")
	}
	t.Log("PASS: 20 products, 3 independent benefit scopes, atomic triggers/rollback, 8-way duplicate claims, 20 task bindings, tenant isolation, observation history, stale publish/result CAS, immutable snapshot and dead-job retry")
}
