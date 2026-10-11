package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

var lecturePrefixTest = flag.Bool("lecture-prefix-test", false, "Explicit opt-in seven random-prefixed isolated MySQL tables; customer tables are never touched")

func lectureTestStore(t *testing.T) *Store {
	t.Helper()
	if !*lecturePrefixTest {
		return salesIsolatedMySQL(t)
	}
	cfg := mysql.NewConfig()
	cfg.User, cfg.Passwd, cfg.DBName = os.Getenv("DB_USER"), os.Getenv("DB_PASSWORD"), os.Getenv("DB_NAME")
	port := os.Getenv("DB_PORT")
	if port == "" {
		port = "3306"
	}
	cfg.Net, cfg.Addr, cfg.ParseTime = "tcp", net.JoinHostPort(os.Getenv("DB_HOST"), port), true
	cfg.Timeout, cfg.ReadTimeout, cfg.WriteTimeout = 8*time.Second, 15*time.Second, 15*time.Second
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		t.Fatal("invalid isolated database configuration")
	}
	key := make([]byte, 8)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	prefix := "lt" + hex.EncodeToString(key) + "_"
	tables := []string{"live_agent_plans", "live_agent_plan_facts", "live_agent_plan_fact_revisions", "live_plan_content_revisions", "live_plan_lecture_tasks", "live_plan_lecture_policies", "live_plan_content_corrections"}
	pairs := []string{"xl_pool_source_", prefix + "trigger_"}
	for _, table := range tables {
		pairs = append(pairs, table, prefix+table)
	}
	database := sql.OpenDB(poolTestConnector{connector, strings.NewReplacer(pairs...)})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for i := len(tables) - 1; i >= 0; i-- {
			if _, err := database.ExecContext(ctx, "DROP TABLE IF EXISTS "+tables[i]); err != nil {
				t.Error("isolated fixture cleanup", err)
			}
		}
		database.Close()
	})
	return &Store{db: database}
}

func TestLectureLibraryMySQL(t *testing.T) {
	s := lectureTestStore(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE live_agent_plans(id BIGINT PRIMARY KEY,tenant_id BIGINT,status VARCHAR(20))`)
	exec(`INSERT INTO live_agent_plans VALUES(4,14,'active'),(5,15,'active')`)
	wanted := map[string]bool{"live_agent_plan_facts": true, "live_agent_plan_fact_revisions": true}
	for _, raw := range strings.Split(strings.ReplaceAll(liveRuntimeSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(raw)
		if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
			exec(raw)
			delete(wanted, fields[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal(wanted)
	}
	exec(`CREATE TABLE live_plan_content_revisions(tenant_id BIGINT,plan_id BIGINT,revision BIGINT,PRIMARY KEY(tenant_id,plan_id))`)
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		row := "NEW"
		if op == "DELETE" {
			row = "OLD"
		}
		exec("CREATE TRIGGER xl_pool_source_0_" + op + " AFTER " + op + " ON live_agent_plan_facts FOR EACH ROW INSERT INTO live_plan_content_revisions VALUES(" + row + ".tenant_id," + row + ".plan_id,1) ON DUPLICATE KEY UPDATE revision=revision+1")
	}
	for i := 0; i < 2; i++ {
		if err := s.MigrateLectureTasks(ctx); err != nil {
			t.Fatal(err)
		}
	}
	task := lecturetask.Task{Subject: "原料A", RootFact: &lecturetask.Fact{Category: "other", Name: "原料关联", Text: "商品使用原料A", Quote: "商品使用原料A"}, Title: "原料产地", Purpose: "说明来源", QuestionTerms: []string{"产地"}, Angles: []lecturetask.Angle{{Title: "来源", Goal: "解释产地"}}}
	facts := []model.LiveAgentPlanFactCandidate{{Category: "other", Key: "原料产地", Value: "四川南充", SourceQuote: "四川南充"}}
	id, added, err := s.SaveLectureSubmission(ctx, 14, 4, 8, "四川南充", `{"passed":true,"issues":[]}`, task, facts)
	if err != nil || id == 0 || added != 1 {
		t.Fatal(id, added, err)
	}
	if rev, err := s.ContentSourceRevision(ctx, 14, 4); err != nil || rev != 1 {
		t.Fatal("only level-one root should change ordinary source revision", rev, err)
	}
	lib, err := s.LectureLibrary(ctx, 14, 4, true)
	if err != nil || len(lib.Tasks) != 1 || !lib.Tasks[0].Valid || lib.Tasks[0].Source != "四川南充" {
		t.Fatal(lib, err)
	}
	frozen, err := s.LectureLibrary(ctx, 14, 4, false)
	ordinary, ordinaryErr := s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if ordinaryErr != nil || len(ordinary) != 1 || ordinary[0].Value != "商品使用原料A" || lib.Tasks[0].Facts[0].Storage != "lecture" {
		t.Fatal("level-two facts leaked", ordinary, lib, ordinaryErr)
	}
	if q := lecturetask.QuestionEvidence(lib, "产地"); strings.Contains(q, "四川南充") || !strings.Contains(q, "商品使用原料A") {
		t.Fatal("ordinary question loaded level-two", q)
	}
	if err != nil || frozen.Tasks[0].Source != "" || frozen.Fingerprint != lib.Fingerprint {
		t.Fatal("source leaked into snapshot", err)
	}
	other, err := s.LectureLibrary(ctx, 15, 5, true)
	if err != nil || len(other.Tasks) != 0 {
		t.Fatal("cross tenant library", err)
	}
	same, n, err := s.SaveLectureSubmission(ctx, 14, 4, 8, "四川南充", "review", task, facts)
	if err != nil || same != id || n != 0 {
		t.Fatal("duplicate not idempotent", err)
	}
	conflict := []model.LiveAgentPlanFactCandidate{{Category: "other", Key: "临时资料", Value: "先写这条", SourceQuote: "先写这条"}}
	badTask := task
	badTask.RootFact = &lecturetask.Fact{Category: "other", Name: "原料关联", Text: "冲突", Quote: "冲突"}
	if _, _, err = s.SaveLectureSubmission(ctx, 14, 4, 8, "另一段素材", "review", badTask, conflict); !errors.Is(err, ErrSpeechMinerFactConflict) {
		t.Fatal("missing conflict", err)
	}
	var count int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_agent_plan_facts WHERE tenant_id=14 AND plan_id=4`).Scan(&count); err != nil || count != 1 {
		t.Fatal("partial facts import", count, err)
	}
	before, _ := s.LectureRevision(ctx, 14, 4)
	if err = s.MutateLecture(ctx, 14, 4, id, 1, "delete"); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateLectureSnapshotTx(ctx, tx, 14, 4, map[string]any{"lecture_revision": before}); !errors.Is(err, ErrLectureChanged) {
		t.Fatal("stale saved snapshot", err)
	}
	tx.Rollback()
	restored, n, err := s.SaveLectureSubmission(ctx, 14, 4, 8, "四川南充", "review", task, facts)
	if err != nil || restored != id || n != 0 {
		t.Fatal("deleted duplicate not restored", err)
	}
	if err = s.MutateLecture(ctx, 14, 4, id, 1, "disable"); !errors.Is(err, ErrLectureChanged) {
		t.Fatal("stale revision accepted", err)
	}
	exec(`UPDATE live_agent_plan_facts SET fact_value='四川成都',version_no=version_no+1 WHERE tenant_id=14 AND plan_id=4`)
	lib, err = s.LectureLibrary(ctx, 14, 4, false)
	if err != nil || lib.Tasks[0].Valid {
		t.Fatal("changed dependency still valid", err)
	}
	if rev, _ := s.ContentSourceRevision(ctx, 14, 4); rev != 2 {
		t.Fatal("root edit did not invalidate ordinary source", rev)
	}
	exec(`UPDATE live_agent_plan_facts SET source_type='manual' WHERE tenant_id=14 AND plan_id=4`)
	if rev, _ := s.ContentSourceRevision(ctx, 14, 4); rev != 3 {
		t.Fatal("ordinary transition did not invalidate pools", rev)
	}
	// Legacy flattened imports retain evidence under their program-bound root.
	exec(`INSERT INTO live_agent_plan_facts(tenant_id,plan_id,category,fact_key,fact_value,source_quote,source_type,source_ref,status,version_no) VALUES(14,4,'other','旧原料关联','商品使用原料B','商品使用原料B','story_lecture','old','active',1),(14,4,'other','旧二级资料','原料B可以用于煮汤','原料B可以用于煮汤','story_lecture','old','active',1)`)
	exec(`INSERT INTO live_agent_plan_facts(tenant_id,plan_id,category,fact_key,fact_value,source_quote,source_type,source_ref,status,version_no) VALUES(14,4,'other','另存的原料资料','原料B来自丘陵','原料B来自丘陵','speech_miner','original-source','active',1)`)
	var rootID, childID, extraID int64
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_facts WHERE fact_key='旧原料关联'`).Scan(&rootID); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_facts WHERE fact_key='旧二级资料'`).Scan(&childID); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_facts WHERE fact_key='另存的原料资料'`).Scan(&extraID); err != nil {
		t.Fatal(err)
	}
	factData, _ := json.Marshal([]lecturetask.Fact{{ID: rootID, Version: 1, Name: "旧原料关联", Text: "商品使用原料B"}, {ID: childID, Version: 1, Name: "旧二级资料", Text: "原料B可以用于煮汤"}})
	exec(`INSERT INTO live_plan_lecture_tasks(tenant_id,plan_id,source_sha,source_text,title,purpose,angles_json,facts_json,links_json,review_json,created_by) VALUES(14,4,REPEAT('a',64),'旧原文','原料B','讲原料','{"angles":[{"title":"用途","goal":"讲原料用途"}],"question_terms":["原料B"]}',?,'[]','{}',8)`, string(factData))
	var oldTaskID int64
	if err = s.db.QueryRowContext(ctx, `SELECT id FROM live_plan_lecture_tasks WHERE source_sha=REPEAT('a',64)`).Scan(&oldTaskID); err != nil {
		t.Fatal(err)
	}
	if err = s.AttachLectureSubject(ctx, 15, 5, 8, oldTaskID, 1, rootID, "原料B"); err == nil {
		t.Fatal("cross-tenant binding accepted")
	}
	if err = s.AttachLectureSubject(ctx, 14, 4, 8, oldTaskID, 1, rootID, "原料B", extraID, extraID); err != nil {
		t.Fatal(err)
	}
	ordinary, err = s.ListLiveAgentPlanFacts(ctx, 14, 4)
	if err != nil || len(ordinary) != 2 {
		t.Fatal("legacy detail visible in ordinary facts", ordinary, err)
	}
	lib, err = s.LectureLibrary(ctx, 14, 4, true)
	if err != nil || !lib.Tasks[1].Valid || lib.Tasks[1].RootFact.ID != rootID || len(lib.Tasks[1].Facts) != 2 || lib.Tasks[1].Facts[0].ID != childID || lib.Tasks[1].Facts[1].ID != extraID || lib.Tasks[1].Facts[1].Text != "原料B来自丘陵" {
		t.Fatal("legacy subject binding lost evidence", lib, err)
	}
	var sourceRef string
	var version int
	if err = s.db.QueryRowContext(ctx, `SELECT source_ref,version_no FROM live_agent_plan_facts WHERE id=?`, extraID).Scan(&sourceRef, &version); err != nil || sourceRef != "original-source" || version != 2 {
		t.Fatal("original provenance lost", sourceRef, version, err)
	}
	if err = s.AttachLectureSubject(ctx, 14, 4, 8, oldTaskID, 1, rootID, "原料B", extraID); err == nil {
		t.Fatal("stale reattachment accepted")
	}
	if err = s.UpdateLectureDetail(ctx, 14, 4, 8, oldTaskID, 2, childID, "", "用途", "原料B用于制作汤底", "lecture-detail-edit:fixture"); err != nil {
		t.Fatal(err)
	}
	lib, err = s.LectureLibrary(ctx, 14, 4, true)
	if err != nil || !lib.Tasks[1].Valid || lib.Tasks[1].Revision != 3 || lib.Tasks[1].Facts[0].Text != "原料B用于制作汤底" || lib.Tasks[1].Facts[0].Quote != "" {
		t.Fatal("legacy detail edit lost scope/version", lib, err)
	}
	var quote, ref string
	if err = s.db.QueryRowContext(ctx, `SELECT source_quote,source_ref FROM live_agent_plan_facts WHERE id=?`, childID).Scan(&quote, &ref); err != nil || quote != "原料B可以用于煮汤" || ref != "lecture-detail-edit:fixture" {
		t.Fatal("legacy original archive lost", quote, ref, err)
	}
	modern := lecturetask.Task{Subject: "材料C", RootFact: &lecturetask.Fact{Category: "other", Name: "新独立材料关联", Text: "新商品使用材料C", Quote: "新商品使用材料C"}, Title: "材料C", Purpose: "来源", QuestionTerms: []string{"材料C"}, Angles: []lecturetask.Angle{{Title: "产地", Goal: "地区C"}}}
	modernID, _, err := s.SaveLectureSubmission(ctx, 14, 4, 8, "新商品使用材料C，材料C来自地区C", `{"passed":true,"issues":[]}`, modern, []model.LiveAgentPlanFactCandidate{{Category: "other", Key: "产地", Value: "材料C来自地区C", SourceQuote: "材料C来自地区C"}})
	if err != nil {
		t.Fatal(err)
	}
	lib, err = s.LectureLibrary(ctx, 14, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	var original lecturetask.Task
	for _, v := range lib.Tasks {
		if v.ID == modernID {
			original = v
		}
	}
	if len(original.Facts) != 1 || original.Facts[0].Storage != "lecture" {
		t.Fatal("fixture not level-two storage", original)
	}
	detail := original.Facts[0]
	for _, bad := range []struct {
		tenant, plan, revision, detail int64
		storage                        string
	}{{15, 5, original.Revision, detail.ID, "lecture"}, {14, 4, 99, detail.ID, "lecture"}, {14, 4, original.Revision, detail.ID + 9999, "lecture"}, {14, 4, original.Revision, detail.ID, ""}} {
		if e := s.UpdateLectureDetail(ctx, bad.tenant, bad.plan, 8, modernID, bad.revision, bad.detail, bad.storage, "不应保存", "越界输入", "fixture"); e == nil {
			t.Fatal("cross-scope/stale/detail collision accepted", bad)
		}
	}
	if err = s.UpdateLectureDetail(ctx, 14, 4, 8, modernID, original.Revision, detail.ID, "lecture", "新的产地说明", "材料C来自地区D", "lecture-detail-edit:modern"); err != nil {
		t.Fatal(err)
	}
	lib, err = s.LectureLibrary(ctx, 14, 4, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range lib.Tasks {
		if v.ID == modernID {
			if !v.Valid || v.Revision != original.Revision+1 || v.RootFact.Version != original.RootFact.Version || v.Facts[0].Version != detail.Version+1 || v.Facts[0].Text != "材料C来自地区D" || v.Facts[0].Quote != "材料C来自地区C" {
				t.Fatal("modern detail not saved independently", v)
			}
		}
	}
}
