package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
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
	tables := []string{"live_agent_plans", "live_agent_plan_facts", "live_agent_plan_fact_revisions", "live_plan_content_revisions", "live_plan_lecture_tasks", "live_plan_lecture_policies"}
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
	task := lecturetask.Task{Title: "原料产地", Purpose: "说明来源", QuestionTerms: []string{"产地"}, Angles: []lecturetask.Angle{{Title: "来源", Goal: "解释产地"}}}
	facts := []model.LiveAgentPlanFactCandidate{{Category: "other", Key: "原料产地", Value: "四川南充", SourceQuote: "四川南充"}}
	id, added, err := s.SaveLectureSubmission(ctx, 14, 4, 8, "四川南充", `{"passed":true,"issues":[]}`, task, facts)
	if err != nil || id == 0 || added != 1 {
		t.Fatal(id, added, err)
	}
	if rev, err := s.ContentSourceRevision(ctx, 14, 4); err != nil || rev != 0 {
		t.Fatal("story polluted ordinary source revision", rev, err)
	}
	lib, err := s.LectureLibrary(ctx, 14, 4, true)
	if err != nil || len(lib.Tasks) != 1 || !lib.Tasks[0].Valid || lib.Tasks[0].Source != "四川南充" {
		t.Fatal(lib, err)
	}
	frozen, err := s.LectureLibrary(ctx, 14, 4, false)
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
	conflict := append([]model.LiveAgentPlanFactCandidate{{Category: "other", Key: "临时事实", Value: "先写这条", SourceQuote: "先写这条"}}, model.LiveAgentPlanFactCandidate{Category: "other", Key: "原料产地", Value: "冲突", SourceQuote: "冲突"})
	if _, _, err = s.SaveLectureSubmission(ctx, 14, 4, 8, "另一段素材", "review", task, conflict); !errors.Is(err, ErrSpeechMinerFactConflict) {
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
	if rev, _ := s.ContentSourceRevision(ctx, 14, 4); rev != 0 {
		t.Fatal("story edit invalidated core", rev)
	}
	exec(`UPDATE live_agent_plan_facts SET source_type='manual' WHERE tenant_id=14 AND plan_id=4`)
	if rev, _ := s.ContentSourceRevision(ctx, 14, 4); rev != 1 {
		t.Fatal("ordinary transition did not invalidate pools", rev)
	}
}
