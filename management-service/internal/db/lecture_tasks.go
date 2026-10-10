package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/lecturetask"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
	"time"
)

var ErrLectureChanged = errors.New("讲解任务已变化，请刷新后重试")

type lectureQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Program-owned concurrency fingerprint. This never becomes model context.
func lectureRevision(ctx context.Context, q lectureQueryer, tenant, plan int64) (string, error) {
	values := []any{}
	rows, err := q.QueryContext(ctx, `SELECT id,revision,status,enabled FROM live_plan_lecture_tasks WHERE tenant_id=? AND plan_id=? ORDER BY id`, tenant, plan)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var id, revision int64
		var status string
		var enabled bool
		if err = rows.Scan(&id, &revision, &status, &enabled); err != nil {
			rows.Close()
			return "", err
		}
		values = append(values, []any{id, revision, status, enabled})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	rows, err = q.QueryContext(ctx, `SELECT id,version_no,status,fact_value FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? ORDER BY id`, tenant, plan)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var id, revision int64
		var status, value string
		if err = rows.Scan(&id, &revision, &status, &value); err != nil {
			rows.Close()
			return "", err
		}
		values = append(values, []any{id, revision, status, value})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	var policy string
	err = q.QueryRowContext(ctx, `SELECT policy_json FROM live_plan_lecture_policies WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&policy)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	values = append(values, policy)
	raw, _ := json.Marshal(values)
	return lectureSHA(string(raw)), nil
}
func (s *Store) LectureRevision(ctx context.Context, tenant, plan int64) (string, error) {
	return lectureRevision(ctx, s.db, tenant, plan)
}
func validateLectureSnapshotTx(ctx context.Context, tx *sql.Tx, tenant, plan int64, data map[string]any) error {
	expected, _ := data["lecture_revision"].(string)
	if expected == "" {
		return nil
	}
	current, err := lectureRevision(ctx, tx, tenant, plan)
	if err != nil {
		return err
	}
	if current != expected {
		return ErrLectureChanged
	}
	return nil
}

func (s *Store) MigrateLectureTasks(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS live_plan_lecture_tasks (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, tenant_id BIGINT UNSIGNED NOT NULL, plan_id BIGINT UNSIGNED NOT NULL, source_sha CHAR(64) NOT NULL, source_text MEDIUMTEXT NOT NULL, title VARCHAR(160) NOT NULL, purpose TEXT NOT NULL, angles_json MEDIUMTEXT NOT NULL, facts_json MEDIUMTEXT NOT NULL, links_json TEXT NOT NULL, enabled BOOLEAN NOT NULL DEFAULT TRUE, status VARCHAR(20) NOT NULL DEFAULT 'active', revision BIGINT NOT NULL DEFAULT 1, review_json MEDIUMTEXT NOT NULL, created_by BIGINT UNSIGNED NOT NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3), PRIMARY KEY(id), UNIQUE KEY uq_lecture_source(tenant_id,plan_id,source_sha), KEY idx_lecture_plan(tenant_id,plan_id,status)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS live_plan_lecture_policies (tenant_id BIGINT UNSIGNED NOT NULL, plan_id BIGINT UNSIGNED NOT NULL, policy_json TEXT NOT NULL, PRIMARY KEY(tenant_id,plan_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	// Story facts belong to the independent library. Ordinary fact mutations
	// still invalidate content pools, including transitions into/out of stories.
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		name := "xl_pool_source_0_" + op
		var body string
		err := s.db.QueryRowContext(ctx, `SELECT ACTION_STATEMENT FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, name).Scan(&body)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if strings.Contains(body, "story_lecture") {
			continue
		}
		row, condition := "NEW", "NEW.source_type <> 'story_lecture'"
		if op == "DELETE" {
			row, condition = "OLD", "OLD.source_type <> 'story_lecture'"
		}
		if op == "UPDATE" {
			condition = "NEW.source_type <> 'story_lecture' OR OLD.source_type <> 'story_lecture'"
		}
		if _, err = s.db.ExecContext(ctx, "DROP TRIGGER "+name); err != nil {
			return err
		}
		q := fmt.Sprintf("CREATE TRIGGER %s AFTER %s ON live_agent_plan_facts FOR EACH ROW BEGIN IF %s THEN INSERT INTO live_plan_content_revisions(tenant_id,plan_id,revision) VALUES(%s.tenant_id,%s.plan_id,1) ON DUPLICATE KEY UPDATE revision=revision+1; END IF; END", name, op, condition, row, row)
		if _, err = s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("story fact revision trigger %s: %w", name, err)
		}
	}
	return nil
}

func lectureSHA(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

// The plan row is the common mutation lock used by both fact editors and intake.
// Facts and the lecture asset commit together; conflicts never publish a half import.
func (s *Store) SaveLectureSubmission(ctx context.Context, tenant, plan, actor int64, source, review string, task lecturetask.Task, candidates []model.LiveAgentPlanFactCandidate) (int64, int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists); err != nil {
		return 0, 0, err
	}
	linksForKey := append([]string(nil), task.Links...)
	sort.Strings(linksForKey)
	keyJSON, _ := json.Marshal(struct {
		Source string
		Links  []string
	}{source, linksForKey})
	sourceSHA := lectureSHA(string(keyJSON))
	var oldID int64
	var oldStatus string
	err = tx.QueryRowContext(ctx, `SELECT id,status FROM live_plan_lecture_tasks WHERE tenant_id=? AND plan_id=? AND source_sha=? FOR UPDATE`, tenant, plan, sourceSHA).Scan(&oldID, &oldStatus)
	if err == nil && oldStatus == "active" {
		return oldID, 0, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, err
	}
	facts := make([]lecturetask.Fact, 0, len(candidates))
	added := 0
	if task.RootFact != nil {
		f := task.RootFact
		var id, version int64
		var value, status, sourceType string
		err = tx.QueryRowContext(ctx, `SELECT id,version_no,fact_value,status,source_type FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? AND category=? AND fact_key=? FOR UPDATE`, tenant, plan, f.Category, f.Name).Scan(&id, &version, &value, &status, &sourceType)
		if err == nil {
			if status != "active" || strings.TrimSpace(value) != f.Text {
				return 0, 0, fmt.Errorf("%w：%s；请先解决原事实冲突", ErrSpeechMinerFactConflict, f.Name)
			}
			if sourceType != "story_subject" {
				if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_facts SET source_type='story_subject',version_no=version_no+1,updated_by_user_id=? WHERE tenant_id=? AND plan_id=? AND id=?`, actor, tenant, plan, id); err != nil {
					return 0, 0, err
				}
				version++
				if err = saveLectureRootRevision(ctx, tx, tenant, plan, id, actor); err != nil {
					return 0, 0, err
				}
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			res, e := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_facts (tenant_id,plan_id,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,status,version_no,created_by_user_id,updated_by_user_id) VALUES (?,?,?,?,?,'','',?,'adoptable','lecture_review_passed','story_subject',?,'active',1,?,?)`, tenant, plan, f.Category, f.Name, f.Text, f.Quote, sourceSHA, actor, actor)
			if e != nil {
				return 0, 0, e
			}
			id, e = res.LastInsertId()
			if e != nil {
				return 0, 0, e
			}
			version = 1
			_, e = tx.ExecContext(ctx, `INSERT INTO live_agent_plan_fact_revisions (tenant_id,plan_id,fact_id,version_no,action,category,fact_key,fact_value,forbidden_wording,safe_rewrite,source_quote,source_review_bucket,source_review_reason,source_type,source_ref,actor_user_id) VALUES (?,?,?,1,'adopt',?,?,?,'','',?,'adoptable','lecture_review_passed','story_subject',?,?)`, tenant, plan, id, f.Category, f.Name, f.Text, f.Quote, sourceSHA, actor)
			if e != nil {
				return 0, 0, e
			}
			added++
		} else {
			return 0, 0, err
		}
		task.RootFact.ID, task.RootFact.Version = id, version
	}
	for i, f := range candidates {
		facts = append(facts, lecturetask.Fact{Storage: "lecture", ID: int64(i + 1), Version: 1, Category: f.Category, Name: f.Key, Text: f.Value, Quote: f.SourceQuote})
	}
	angles, _ := json.Marshal(map[string]any{"angles": task.Angles, "question_terms": task.QuestionTerms, "subject": task.Subject, "root_fact": task.RootFact})
	factJSON, _ := json.Marshal(facts)
	links, _ := json.Marshal(task.Links)
	if oldID > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE live_plan_lecture_tasks SET source_text=?,title=?,purpose=?,angles_json=?,facts_json=?,links_json=?,review_json=?,status='active',enabled=TRUE,revision=revision+1 WHERE tenant_id=? AND plan_id=? AND id=?`, source, task.Title, task.Purpose, string(angles), string(factJSON), string(links), review, tenant, plan, oldID)
		if err != nil {
			return 0, 0, err
		}
		if err = tx.Commit(); err != nil {
			return 0, 0, err
		}
		return oldID, added, nil
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO live_plan_lecture_tasks (tenant_id,plan_id,source_sha,source_text,title,purpose,angles_json,facts_json,links_json,review_json,created_by) VALUES (?,?,?,?,?,?,?,?,?,?,?)`, tenant, plan, sourceSHA, source, task.Title, task.Purpose, string(angles), string(factJSON), string(links), review, actor)
	if err != nil {
		return 0, 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return id, added, nil
}

func (s *Store) LectureLibrary(ctx context.Context, tenant, plan int64, includeSource bool) (lecturetask.Library, error) {
	return s.lectureLibrary(ctx, tenant, plan, includeSource, false)
}
func (s *Store) LectureQuestionLibrary(ctx context.Context, tenant, plan int64) (lecturetask.Library, error) {
	return s.lectureLibrary(ctx, tenant, plan, false, true)
}
func (s *Store) lectureLibrary(ctx context.Context, tenant, plan int64, includeSource, includeDeleted bool) (lecturetask.Library, error) {
	lib := lecturetask.Library{Tasks: []lecturetask.Task{}, Policy: lecturetask.DefaultPolicy()}
	var cfg string
	err := s.db.QueryRowContext(ctx, `SELECT policy_json FROM live_plan_lecture_policies WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&cfg)
	if err == nil {
		if err = json.Unmarshal([]byte(cfg), &lib.Policy); err != nil {
			return lib, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return lib, err
	}
	statusClause := " AND status='active'"
	if includeDeleted {
		statusClause = ""
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,revision,title,purpose,angles_json,facts_json,links_json,enabled,source_text,updated_at FROM live_plan_lecture_tasks WHERE tenant_id=? AND plan_id=?`+statusClause+` ORDER BY id`, tenant, plan)
	if err != nil {
		return lib, err
	}
	for rows.Next() {
		var t lecturetask.Task
		var angles, facts, links, source string
		var at time.Time
		if err = rows.Scan(&t.ID, &t.Revision, &t.Title, &t.Purpose, &angles, &facts, &links, &t.Enabled, &source, &at); err != nil {
			rows.Close()
			return lib, err
		}
		var angleAsset struct {
			Subject       string              `json:"subject"`
			RootFact      *lecturetask.Fact   `json:"root_fact"`
			Angles        []lecturetask.Angle `json:"angles"`
			QuestionTerms []string            `json:"question_terms"`
		}
		if err = json.Unmarshal([]byte(angles), &angleAsset); err != nil {
			rows.Close()
			return lib, err
		}
		t.Angles, t.QuestionTerms = angleAsset.Angles, angleAsset.QuestionTerms
		t.Subject, t.RootFact = angleAsset.Subject, angleAsset.RootFact
		for _, v := range []struct {
			s string
			p any
		}{{facts, &t.Facts}, {links, &t.Links}} {
			if err = json.Unmarshal([]byte(v.s), v.p); err != nil {
				rows.Close()
				return lib, err
			}
		}
		if includeSource {
			t.Source = source
		}
		t.UpdatedAt = at.UTC().Format(time.RFC3339Nano)
		lib.Tasks = append(lib.Tasks, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return lib, err
	}
	// Compatibility dependencies are private story records; they are excluded
	// from the ordinary fact API but retain their original IDs and revisions.
	currentRows, err := s.db.QueryContext(ctx, liveAgentPlanFactSelect+` WHERE tenant_id=? AND plan_id=? AND status='active'`, tenant, plan)
	if err != nil {
		return lib, err
	}
	byID := map[int64]model.LiveAgentPlanFact{}
	for currentRows.Next() {
		f, e := scanLiveAgentPlanFact(currentRows)
		if e != nil {
			currentRows.Close()
			return lib, e
		}
		byID[f.ID] = f
	}
	err = currentRows.Err()
	currentRows.Close()
	if err != nil {
		return lib, err
	}
	for i := range lib.Tasks {
		t := &lib.Tasks[i]
		t.Valid = len(t.Facts) > 0 && len(t.Angles) > 0
		if t.RootFact != nil {
			f := t.RootFact
			v, ok := byID[f.ID]
			if !ok || v.VersionNo != f.Version || v.Value != f.Text || v.Status != "active" {
				t.Valid = false
			}
		}
		for _, f := range t.Facts {
			if f.Storage == "lecture" {
				continue
			}
			v, ok := byID[f.ID]
			if !ok || v.VersionNo != f.Version || v.Value != f.Text || v.Status != "active" {
				t.Valid = false
			}
		}
	}
	// Source prose is deliberately excluded from run snapshots and fingerprints.
	clean := lib
	clean.Tasks = append([]lecturetask.Task(nil), lib.Tasks...)
	for i := range clean.Tasks {
		clean.Tasks[i].Source = ""
	}
	raw, _ := json.Marshal(clean)
	lib.Fingerprint = lectureSHA(string(raw))
	return lib, nil
}

func (s *Store) MutateLecture(ctx context.Context, tenant, plan, id, revision int64, action string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists); err != nil {
		return err
	}
	var res sql.Result
	switch action {
	case "enable", "disable":
		res, err = tx.ExecContext(ctx, `UPDATE live_plan_lecture_tasks SET enabled=?,revision=revision+1 WHERE tenant_id=? AND plan_id=? AND id=? AND revision=? AND status='active'`, action == "enable", tenant, plan, id, revision)
	case "delete":
		res, err = tx.ExecContext(ctx, `UPDATE live_plan_lecture_tasks SET status='deleted',revision=revision+1 WHERE tenant_id=? AND plan_id=? AND id=? AND revision=? AND status='active'`, tenant, plan, id, revision)
	default:
		return errors.New("讲解操作无效")
	}
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrLectureChanged
	}
	return tx.Commit()
}
func (s *Store) SaveLecturePolicy(ctx context.Context, tenant, plan int64, p lecturetask.Policy) error {
	if err := p.Validate(); err != nil {
		return err
	}
	raw, _ := json.Marshal(p)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&exists); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_plan_lecture_policies(tenant_id,plan_id,policy_json) VALUES(?,?,?) ON DUPLICATE KEY UPDATE policy_json=VALUES(policy_json)`, tenant, plan, string(raw))
	if err != nil {
		return err
	}
	return tx.Commit()
}
