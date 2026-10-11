package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/contentpools"
)

var poolEditPrefixTest = flag.Bool("pool-edit-prefix-test", false, "Explicit opt-in isolated random-prefix MySQL tables; no customer tables or rows are touched")

type poolTestConnector struct {
	driver.Connector
	replace *strings.Replacer
}
type poolTestConn struct {
	driver.Conn
	replace *strings.Replacer
}

func (c poolTestConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return poolTestConn{conn, c.replace}, nil
}
func (c poolTestConn) Prepare(query string) (driver.Stmt, error) {
	return c.Conn.Prepare(c.replace.Replace(query))
}
func (c poolTestConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, options)
}
func (c poolTestConn) args(args []driver.NamedValue) []driver.NamedValue {
	next := append([]driver.NamedValue{}, args...)
	for i, v := range next {
		if s, ok := v.Value.(string); ok && strings.HasPrefix(s, "xl_pool_source_") {
			next[i].Value = c.replace.Replace(s)
		}
	}
	return next
}
func (c poolTestConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, c.replace.Replace(query), c.args(args))
}
func (c poolTestConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, c.replace.Replace(query), c.args(args))
}

func TestContentPoolEditPrefixedMySQL(t *testing.T) {
	if !*poolEditPrefixTest {
		t.Skip("explicit isolated table integration opt-in required")
	}
	config := mysql.NewConfig()
	config.User, config.Passwd, config.DBName = os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	config.Net, config.Addr, config.ParseTime = "tcp", net.JoinHostPort(os.Getenv("DB_HOST"), port), true
	config.Timeout, config.ReadTimeout, config.WriteTimeout = 8*time.Second, 15*time.Second, 15*time.Second
	connector, err := mysql.NewConnector(config)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	nonce := make([]byte, 8)
	if _, err = rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	prefix := "zm" + hex.EncodeToString(nonce) + "_"
	tables := []string{"live_agent_plan_facts", "live_agent_plan_product_links", "live_agent_plan_product_attributes", "live_agent_plan_benefits", "live_selling_point_wheels", "live_plan_content_changes", "live_plan_content_revisions", "live_plan_content_observations", "live_plan_content_sets", "live_plan_content_jobs", "live_plan_content_pool_records", "live_plan_content_tasks", "live_plan_content_task_products", "live_plan_benefit_scopes", "live_plan_benefit_scope_products", "live_plan_content_corrections"}
	pairs := []string{"xl_pool_source_", prefix + "trigger_"}
	for _, name := range tables {
		pairs = append(pairs, name, prefix+name)
	}
	rewrite := strings.NewReplacer(pairs...)
	database := sql.OpenDB(poolTestConnector{connector, rewrite})
	s := &Store{db: database}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		conn, e := database.Conn(cleanup)
		if e == nil {
			_, _ = conn.ExecContext(cleanup, "SET FOREIGN_KEY_CHECKS=0")
			for _, table := range tables {
				if _, e = conn.ExecContext(cleanup, "DROP TABLE IF EXISTS "+table); e != nil {
					t.Error("isolated fixture cleanup failed", e)
				}
			}
			_, _ = conn.ExecContext(cleanup, "SET FOREIGN_KEY_CHECKS=1")
			_ = conn.Close()
		} else {
			t.Error("isolated fixture cleanup could not connect", e)
		}
		_ = database.Close()
	})
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := database.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range tables[:5] {
		exec("CREATE TABLE " + name + " (id BIGINT UNSIGNED PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL) ENGINE=InnoDB")
	}
	if err = s.MigrateContentPools(ctx); err != nil {
		t.Fatal(err)
	}
	if err = s.MigrateContentCorrections(ctx); err != nil {
		t.Fatal(err)
	}
	exec("INSERT INTO live_agent_plan_product_links VALUES(1,14,4),(2,14,4)")
	source := contentpools.Sources{Products: []contentpools.Product{{ID: 1, Link: "1"}, {ID: 2, Link: "2"}}, Rows: []contentpools.SourceRow{{ID: "fact", Fields: map[string]string{"内容": "用户资料"}}}}
	source.Revision, _ = s.ContentSourceRevision(ctx, 14, 4)
	pool := func(key string, owner int64, products []int64, task string) contentpools.Pool {
		return contentpools.Pool{Key: key, Owner: owner, Products: products, Facts: []contentpools.Fact{{ID: "f", Sources: []string{"fact"}, Products: products}}, Tasks: []contentpools.Task{{ID: task, Point: "卖点", Angle: "原始" + task, Sources: []string{"f"}}}}
	}
	set := contentpools.Set{Sources: source, SourceSHA: source.SHA(), SafeFacts: "用户资料", Pools: []contentpools.Pool{pool("common", 0, []int64{1, 2}, "s"), pool("product:1", 1, []int64{1}, "a"), pool("product:2", 2, []int64{2}, "b")}}
	job, err := s.ClaimPoolJob(ctx, 14, 4, 1, source)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.CommitContentPoolSet(ctx, 14, 4, 1, job, set)
	if err != nil {
		t.Fatal(err)
	}
	edit := contentpools.PoolEdit{ExpectedSetID: saved.ID, PoolKey: "product:1", TaskIDs: []string{"a"}, Tasks: []contentpools.TaskEdit{{ID: "a", Point: "修改", Angle: "用户修改"}, {Point: "新增", Angle: "用户新增"}}}
	if _, e := s.EditContentPool(ctx, 15, 5, 1, source, edit); e == nil {
		t.Fatal("tenant isolation failed")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := []contentpools.Set{}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			next, e := s.EditContentPool(ctx, 14, 4, 1, source, edit)
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				winners = append(winners, next)
			}
		}()
	}
	wg.Wait()
	if len(winners) != 1 {
		t.Fatal("concurrent edit lost update protection", len(winners))
	}
	latest := winners[0]
	original, err := s.GetContentPoolSet(ctx, 14, 4, saved.ID)
	if err != nil || original.Pools[1].Tasks[0].Angle != "原始a" || latest.Pools[0].Tasks[0].Angle != "原始s" || latest.Pools[2].Tasks[0].Angle != "原始b" || len(latest.Pools[1].Tasks) != 2 {
		t.Fatal("immutable/product/common isolation", err)
	}
	var count int
	if err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM live_plan_content_task_products WHERE set_id=?", latest.ID).Scan(&count); err != nil || count != 5 {
		t.Fatal("task product associations", count, err)
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = validatePoolSnapshotTx(ctx, tx, 14, 4, map[string]any{"content_pools": map[string]any{"set_id": saved.ID, "source_sha": saved.SourceSHA, "source_revision": source.Revision}}); err == nil {
		t.Fatal("in-flight stale agent save accepted")
	}
	_ = tx.Rollback()
	correction := contentpools.Correction{ID: "program-correction", Products: []int64{1}, Original: "用户修改", Feedback: "只改这个商品，以后不要推荐机洗", Content: contentpools.CorrectionContent{Summary: "修正清洗方式", Fact: "仅支持手洗", Forbidden: "不推荐机洗"}}
	job, err = s.ClaimPoolJob(ctx, 14, 4, 1, latest.Sources)
	if err != nil {
		t.Fatal(err)
	}
	repaired, err := contentpools.PartitionCorrectionPools(latest, correction.Products)
	if err != nil {
		t.Fatal(err)
	}
	repaired.Sources.Corrections = append(repaired.Sources.Corrections, correction)
	repaired.SourceSHA = repaired.Sources.SHA()
	next, err := s.SaveContentCorrection(ctx, 14, 4, 1, latest, repaired, correction, job)
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.ListContentCorrections(ctx, 14, 4)
	if err != nil || len(items) != 1 || items[0].Feedback != correction.Feedback || next.ID <= latest.ID {
		t.Fatal("correction not durably committed", items, next.ID, err)
	}
	if other, err := s.ListContentCorrections(ctx, 15, 4); err != nil || len(other) != 0 {
		t.Fatal("cross-tenant correction", other, err)
	}
	if _, err = s.SaveContentCorrection(ctx, 14, 4, 1, latest, repaired, correction, job); err == nil {
		t.Fatal("duplicate/stale correction committed")
	}
	before, err := s.GetContentPoolSet(ctx, 14, 4, latest.ID)
	if err != nil || len(before.Sources.Corrections) != 0 {
		t.Fatal("old snapshot overwritten", before, err)
	}
	partial := next
	partial.GenerationProtocol = contentpools.GenerationProtocol
	partial.Sources.Revision, err = s.ContentSourceRevision(ctx, 14, 4)
	if err != nil {
		t.Fatal(err)
	}
	partial.SourceSHA = partial.Sources.SHA()
	for i := range partial.Pools {
		if partial.Pools[i].Owner != 1 {
			partial.Pools[i].Tasks = []contentpools.Task{}
		}
	}
	partial.Notices = []contentpools.Notice{{Pool: "2号", Content: "旧包邮内容", Kind: "excluded", Reason: "缺少当前依据"}, {Pool: "公共内容", Kind: "unavailable", Reason: "单条审核未完成", Retryable: true}}
	job, err = s.ClaimPoolJob(ctx, 14, 4, 1, partial.Sources)
	if err != nil {
		t.Fatal(err)
	}
	partial, err = s.CommitContentPoolSet(ctx, 14, 4, 1, job, partial)
	if err != nil {
		t.Fatal("partial approved draft blocked", err)
	}
	loaded, err := s.GetContentPoolSet(ctx, 14, 4, partial.ID)
	if err != nil || len(loaded.Notices) != 2 || !loaded.HasUnfinishedContent() || loaded.GenerationProtocol != contentpools.GenerationProtocol {
		t.Fatal("partial draft lost excluded material", loaded, err)
	}
	var excludedCount int
	if err = database.QueryRowContext(ctx, "SELECT COUNT(*) FROM live_plan_content_tasks WHERE set_id=? AND pool_key=?", partial.ID, "product:2").Scan(&excludedCount); err != nil || excludedCount != 0 {
		t.Fatal("excluded content entered executable pool", excludedCount, err)
	}
	previous, err := s.GetContentPoolSet(ctx, 14, 4, next.ID)
	if err != nil || len(previous.Notices) != 0 || previous.GenerationProtocol != "" {
		t.Fatal("partial snapshot overwrote earlier approved set", err)
	}
	t.Log(fmt.Sprintf("PASS isolated tables %s*: persistence, 2-way edit CAS, tenant/pool isolation, old snapshot, new angle references, product associations, stale agent save", prefix))
}
