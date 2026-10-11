package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"livecompanion/management/internal/contentpools"
)

var ErrPoolSourceChanged = errors.New("资料在生成期间发生变化，请更新受影响内容；已保存方案保持可用")
var ErrPoolJobRunning = errors.New("当前方案正在更新内容，请等待完成")

func validatePoolSnapshotTx(ctx context.Context, tx *sql.Tx, tenant, plan int64, data map[string]any) error {
	raw, err := json.Marshal(data["content_pools"])
	if err != nil {
		return err
	}
	if string(raw) == "null" {
		return nil
	}
	var ref struct {
		ID        int64  `json:"set_id"`
		SHA       string `json:"source_sha"`
		Revision  int64  `json:"source_revision"`
		VoiceOnly bool   `json:"voice_only"`
	}
	if err = json.Unmarshal(raw, &ref); err != nil {
		return err
	}
	var sha string
	if err = tx.QueryRowContext(ctx, `SELECT source_sha FROM live_plan_content_sets WHERE tenant_id=? AND plan_id=? AND id=?`, tenant, plan, ref.ID).Scan(&sha); err != nil {
		return err
	}
	if sha != ref.SHA {
		return ErrPoolSourceChanged
	}
	if ref.VoiceOnly {
		return nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO live_plan_content_revisions VALUES(?,?,0)`, tenant, plan); err != nil {
		return err
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&revision); err != nil {
		return err
	}
	if revision != ref.Revision {
		return ErrPoolSourceChanged
	}
	return nil
}

type PoolJob struct {
	ID        int64  `json:"id"`
	Status    string `json:"status"`
	Stage     string `json:"stage"`
	Error     string `json:"error,omitempty"`
	SourceSHA string `json:"source_sha"`
	Revision  int64  `json:"revision"`
	SetID     int64  `json:"set_id"`
}
type BenefitScope struct {
	BenefitID int64   `json:"benefit_id"`
	All       bool    `json:"all"`
	Products  []int64 `json:"products"`
	VersionNo int64   `json:"version_no"`
}

// Source triggers record revisions only. They never invoke a provider or adopt
// content. Their writes share the source transaction, including rollback.
func (s *Store) MigrateContentPools(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS live_plan_content_changes (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,source_revision BIGINT NOT NULL,changes_json JSON NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),KEY idx_content_change(tenant_id,plan_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_revisions (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,revision BIGINT NOT NULL DEFAULT 0,PRIMARY KEY(tenant_id,plan_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_observations (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,baseline_json JSON NOT NULL,current_json JSON NOT NULL,changes_json JSON NOT NULL,current_sha CHAR(64) NOT NULL,updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),PRIMARY KEY(tenant_id,plan_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_sets (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,source_sha CHAR(64) NOT NULL,data_json JSON NOT NULL,created_by BIGINT UNSIGNED NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),KEY idx_pool_set(tenant_id,plan_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_jobs (id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,source_sha CHAR(64) NOT NULL,source_revision BIGINT NOT NULL,status VARCHAR(24) NOT NULL,stage VARCHAR(255) NOT NULL DEFAULT '',last_error TEXT NULL,result_set_id BIGINT UNSIGNED NOT NULL DEFAULT 0,lease_until DATETIME(3) NULL,created_by BIGINT UNSIGNED NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),KEY idx_pool_job(tenant_id,plan_id,id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_pool_records (set_id BIGINT UNSIGNED NOT NULL,pool_key VARCHAR(96) NOT NULL,product_link_id BIGINT UNSIGNED NULL,source_sha CHAR(64) NOT NULL,PRIMARY KEY(set_id,pool_key),FOREIGN KEY(set_id) REFERENCES live_plan_content_sets(id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_tasks (set_id BIGINT UNSIGNED NOT NULL,task_id VARCHAR(64) NOT NULL,pool_key VARCHAR(96) NOT NULL,selling_point TEXT NOT NULL,angle TEXT NOT NULL,source_ids JSON NOT NULL,PRIMARY KEY(set_id,task_id),FOREIGN KEY(set_id,pool_key) REFERENCES live_plan_content_pool_records(set_id,pool_key)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_content_task_products (set_id BIGINT UNSIGNED NOT NULL,task_id VARCHAR(64) NOT NULL,product_link_id BIGINT UNSIGNED NOT NULL,PRIMARY KEY(set_id,task_id,product_link_id),FOREIGN KEY(set_id,task_id) REFERENCES live_plan_content_tasks(set_id,task_id),FOREIGN KEY(product_link_id) REFERENCES live_agent_plan_product_links(id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_benefit_scopes (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,benefit_id BIGINT UNSIGNED NOT NULL,apply_all BOOLEAN NOT NULL DEFAULT 0,version_no BIGINT NOT NULL DEFAULT 1,PRIMARY KEY(benefit_id),KEY idx_benefit_scope(tenant_id,plan_id),FOREIGN KEY(benefit_id) REFERENCES live_agent_plan_benefits(id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS live_plan_benefit_scope_products (tenant_id BIGINT UNSIGNED NOT NULL,plan_id BIGINT UNSIGNED NOT NULL,benefit_id BIGINT UNSIGNED NOT NULL,product_link_id BIGINT UNSIGNED NOT NULL,PRIMARY KEY(benefit_id,product_link_id),FOREIGN KEY(benefit_id) REFERENCES live_plan_benefit_scopes(benefit_id),FOREIGN KEY(product_link_id) REFERENCES live_agent_plan_product_links(id)) ENGINE=InnoDB`,
	}
	for _, q := range statements {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	tables := []string{"live_agent_plan_facts", "live_agent_plan_product_links", "live_agent_plan_product_attributes", "live_agent_plan_benefits", "live_plan_benefit_scopes", "live_plan_benefit_scope_products", "live_selling_point_wheels"}
	for i, table := range tables {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			name := fmt.Sprintf("xl_pool_source_%d_%s", i, op)
			var count int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE() AND TRIGGER_NAME=?`, name).Scan(&count); err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			row := "NEW"
			if op == "DELETE" {
				row = "OLD"
			}
			body := fmt.Sprintf(`INSERT INTO live_plan_content_revisions(tenant_id,plan_id,revision) VALUES(%s.tenant_id,%s.plan_id,1) ON DUPLICATE KEY UPDATE revision=revision+1`, row, row)
			q := fmt.Sprintf("CREATE TRIGGER %s AFTER %s ON %s FOR EACH ROW %s", name, op, table, body)
			if _, err := s.db.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("create content source revision trigger %s: %w", name, err)
			}
		}
	}
	return nil
}
func (s *Store) ContentSourceRevision(ctx context.Context, tenant, plan int64) (int64, error) {
	var rev int64
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=?`, tenant, plan).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}
func (s *Store) ObserveContentSources(ctx context.Context, tenant, plan int64, current contentpools.Sources) (contentpools.Sources, error) {
	// Seed outside the transaction to avoid concurrent shared-lock upgrades.
	if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO live_plan_content_revisions VALUES(?,?,0)`, tenant, plan); err != nil {
		return current, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return current, err
	}
	defer tx.Rollback()
	var rev int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&rev); err != nil {
		return current, err
	}
	if rev != current.Revision {
		return current, ErrPoolSourceChanged
	}
	raw, _ := json.Marshal(current)
	if _, err = tx.ExecContext(ctx, `INSERT IGNORE INTO live_plan_content_observations(tenant_id,plan_id,baseline_json,current_json,changes_json,current_sha) VALUES(?,?,?,?,'[]',?)`, tenant, plan, raw, raw, current.SHA()); err != nil {
		return current, err
	}
	var baseRaw, oldRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT CAST(baseline_json AS CHAR),CAST(current_json AS CHAR) FROM live_plan_content_observations WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&baseRaw, &oldRaw); err != nil {
		return current, err
	}
	var baseline, old contentpools.Sources
	if err = json.Unmarshal(baseRaw, &baseline); err != nil {
		return current, err
	}
	if err = json.Unmarshal(oldRaw, &old); err != nil {
		return current, err
	}
	if old.ObservedAt <= current.ObservedAt {
		changes, _ := json.Marshal(contentpools.Changes(baseline, current))
		delta := contentpools.Changes(old, current)
		if len(delta) > 0 {
			history, _ := json.Marshal(delta)
			if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_content_changes(tenant_id,plan_id,source_revision,changes_json) VALUES(?,?,?,?)`, tenant, plan, current.Revision, history); err != nil {
				return current, err
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_plan_content_observations SET current_json=?,changes_json=?,current_sha=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=?`, raw, changes, current.SHA(), tenant, plan); err != nil {
			return current, err
		}
	}
	return baseline, tx.Commit()
}

type ContentChangeHistory struct {
	ID        int64                 `json:"id"`
	CreatedAt string                `json:"created_at"`
	Changes   []contentpools.Change `json:"changes"`
}

func (s *Store) ContentChangeHistory(ctx context.Context, tenant, plan int64) ([]ContentChangeHistory, error) {
	r, err := s.db.QueryContext(ctx, `SELECT id,DATE_FORMAT(created_at,'%Y-%m-%d %H:%i:%s'),CAST(changes_json AS CHAR) FROM live_plan_content_changes WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 50`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	out := []ContentChangeHistory{}
	for r.Next() {
		var row ContentChangeHistory
		var raw []byte
		if err = r.Scan(&row.ID, &row.CreatedAt, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &row.Changes); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, r.Err()
}
func scanPoolJob(row interface{ Scan(...any) error }) (PoolJob, error) {
	var j PoolJob
	err := row.Scan(&j.ID, &j.Status, &j.Stage, &j.Error, &j.SourceSHA, &j.Revision, &j.SetID)
	return j, err
}

const poolJobSelect = `SELECT id,status,stage,COALESCE(last_error,''),source_sha,source_revision,result_set_id FROM live_plan_content_jobs`

func (s *Store) LatestPoolJob(ctx context.Context, tenant, plan int64) (PoolJob, error) {
	_, err := s.db.ExecContext(ctx, `UPDATE live_plan_content_jobs SET status='failed',last_error='更新进程已结束或超时，可点击重试',stage='等待重试' WHERE tenant_id=? AND plan_id=? AND status='running' AND lease_until<CURRENT_TIMESTAMP(3)`, tenant, plan)
	if err != nil {
		return PoolJob{}, err
	}
	j, err := scanPoolJob(s.db.QueryRowContext(ctx, poolJobSelect+` WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 1`, tenant, plan))
	if errors.Is(err, sql.ErrNoRows) {
		return j, nil
	}
	return j, err
}
func (s *Store) ClaimPoolJob(ctx context.Context, tenant, plan, actor int64, source contentpools.Sources) (PoolJob, error) {
	// Seed outside the transaction to avoid concurrent shared-lock upgrades.
	if _, err := s.db.ExecContext(ctx, `INSERT IGNORE INTO live_plan_content_revisions VALUES(?,?,0)`, tenant, plan); err != nil {
		return PoolJob{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PoolJob{}, err
	}
	defer tx.Rollback()
	var rev int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&rev); err != nil {
		return PoolJob{}, err
	}
	if rev != source.Revision {
		return PoolJob{}, ErrPoolSourceChanged
	}
	var active int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM live_plan_content_jobs WHERE tenant_id=? AND plan_id=? AND status='running' AND lease_until>CURRENT_TIMESTAMP(3) LIMIT 1`, tenant, plan).Scan(&active)
	if err == nil {
		return PoolJob{}, ErrPoolJobRunning
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PoolJob{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_plan_content_jobs SET status='failed',last_error='更新已超时，可点击重试' WHERE tenant_id=? AND plan_id=? AND status='running'`, tenant, plan)
	if err != nil {
		return PoolJob{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO live_plan_content_jobs(tenant_id,plan_id,source_sha,source_revision,status,stage,lease_until,created_by) VALUES(?,?,?,?,'running','正在读取资料',DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 2 MINUTE),?)`, tenant, plan, source.SHA(), source.Revision, actor)
	if err != nil {
		return PoolJob{}, err
	}
	id, _ := r.LastInsertId()
	if err = tx.Commit(); err != nil {
		return PoolJob{}, err
	}
	return PoolJob{ID: id, Status: "running", SourceSHA: source.SHA(), Revision: source.Revision}, nil
}
func (s *Store) PoolJobStage(ctx context.Context, tenant, plan, id int64, stage string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_plan_content_jobs SET stage=?,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=? AND status='running'`, stage, tenant, plan, id)
	return err
}
func (s *Store) FailPoolJob(ctx context.Context, tenant, plan, id int64, reason string) {
	_, _ = s.db.ExecContext(ctx, `UPDATE live_plan_content_jobs SET status='failed',stage='更新未完成，可重试',last_error=?,lease_until=NULL,updated_at=CURRENT_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND id=? AND status='running'`, reason, tenant, plan, id)
}
func (s *Store) GetContentPoolSet(ctx context.Context, tenant, plan, id int64) (contentpools.Set, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT CAST(data_json AS CHAR) FROM live_plan_content_sets WHERE tenant_id=? AND plan_id=? AND id=?`, tenant, plan, id).Scan(&raw)
	if err != nil {
		return contentpools.Set{}, err
	}
	var set contentpools.Set
	err = json.Unmarshal(raw, &set)
	set.ID = id
	return set, err
}
func (s *Store) LatestContentPoolSet(ctx context.Context, tenant, plan int64) (contentpools.Set, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM live_plan_content_sets WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return contentpools.Set{}, nil
	}
	if err != nil {
		return contentpools.Set{}, err
	}
	return s.GetContentPoolSet(ctx, tenant, plan, id)
}
func (s *Store) CommitContentPoolSet(ctx context.Context, tenant, plan, actor int64, job PoolJob, set contentpools.Set) (contentpools.Set, error) {
	if err := contentpools.ValidateSet(set); err != nil {
		return set, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return set, err
	}
	defer tx.Rollback()
	var rev int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM live_plan_content_revisions WHERE tenant_id=? AND plan_id=? FOR UPDATE`, tenant, plan).Scan(&rev); err != nil {
		return set, err
	}
	if rev != job.Revision || set.SourceSHA != job.SourceSHA {
		return set, ErrPoolSourceChanged
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM live_plan_content_jobs WHERE id=? AND tenant_id=? AND plan_id=? AND lease_until>CURRENT_TIMESTAMP(3) FOR UPDATE`, job.ID, tenant, plan).Scan(&status); err != nil {
		return set, err
	}
	if status != "running" {
		return set, ErrPoolSourceChanged
	}
	set, err = insertContentPoolSetTx(ctx, tx, tenant, plan, actor, set)
	if err != nil {
		return set, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_plan_content_jobs SET status='ready',stage='更新完成，等待保存智能体',result_set_id=?,lease_until=NULL,updated_at=CURRENT_TIMESTAMP(3) WHERE id=?`, set.ID, job.ID); err != nil {
		return set, err
	}
	return set, tx.Commit()
}

func insertContentPoolSetTx(ctx context.Context, tx *sql.Tx, tenant, plan, actor int64, set contentpools.Set) (contentpools.Set, error) {
	// The row ID is authoritative when loading the immutable JSON snapshot.
	set.ID = 0
	raw, err := json.Marshal(set)
	if err != nil {
		return set, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO live_plan_content_sets(tenant_id,plan_id,source_sha,data_json,created_by) VALUES(?,?,?,?,?)`, tenant, plan, set.SourceSHA, raw, actor)
	if err != nil {
		return set, err
	}
	set.ID, err = r.LastInsertId()
	if err != nil {
		return set, err
	}
	for _, p := range set.Pools {
		var owner any
		if p.Owner > 0 {
			owner = p.Owner
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_content_pool_records VALUES(?,?,?,?)`, set.ID, p.Key, owner, p.Fingerprint); err != nil {
			return set, err
		}
		for _, t := range p.Tasks {
			refs, _ := json.Marshal(t.Sources)
			if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_content_tasks VALUES(?,?,?,?,?,?)`, set.ID, t.ID, p.Key, t.Point, t.Angle, refs); err != nil {
				return set, err
			}
			for _, product := range p.Products {
				if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_content_task_products VALUES(?,?,?)`, set.ID, t.ID, product); err != nil {
					return set, err
				}
			}
		}
	}
	return set, nil
}
func (s *Store) ListBenefitScopes(ctx context.Context, tenant, plan int64) (map[int64]BenefitScope, error) {
	r, err := s.db.QueryContext(ctx, `SELECT s.benefit_id,s.apply_all,s.version_no,p.product_link_id FROM live_plan_benefit_scopes s LEFT JOIN live_plan_benefit_scope_products p ON p.benefit_id=s.benefit_id WHERE s.tenant_id=? AND s.plan_id=? ORDER BY s.benefit_id,p.product_link_id`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	m := map[int64]BenefitScope{}
	for r.Next() {
		var id int64
		var all bool
		var version int64
		var product sql.NullInt64
		if err = r.Scan(&id, &all, &version, &product); err != nil {
			return nil, err
		}
		v := m[id]
		v.BenefitID, v.All, v.VersionNo = id, all, version
		if product.Valid {
			v.Products = append(v.Products, product.Int64)
		}
		m[id] = v
	}
	return m, r.Err()
}
func (s *Store) SaveBenefitScope(ctx context.Context, tenant, plan, benefit, expectedVersion int64, scope BenefitScope) error {
	scope.Products = contentpools.IDs(scope.Products)
	if !scope.All && len(scope.Products) == 0 {
		return errors.New("请选择适用商品或全部商品")
	}
	if len(scope.Products) > 1000 {
		return errors.New("适用商品数量超过1000")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int64
	if err = tx.QueryRowContext(ctx, `SELECT version_no FROM live_agent_plan_benefits WHERE id=? AND tenant_id=? AND plan_id=? FOR UPDATE`, benefit, tenant, plan).Scan(&version); err != nil {
		return err
	}
	if expectedVersion > 0 && version != expectedVersion {
		return ErrPoolSourceChanged
	}
	var scopeVersion int64
	err = tx.QueryRowContext(ctx, `SELECT version_no FROM live_plan_benefit_scopes WHERE tenant_id=? AND plan_id=? AND benefit_id=? FOR UPDATE`, tenant, plan, benefit).Scan(&scopeVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if scopeVersion != scope.VersionNo {
		return ErrPoolSourceChanged
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_benefit_scopes(tenant_id,plan_id,benefit_id,apply_all,version_no) VALUES(?,?,?,?,1) ON DUPLICATE KEY UPDATE apply_all=VALUES(apply_all),version_no=version_no+1`, tenant, plan, benefit, scope.All); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM live_plan_benefit_scope_products WHERE benefit_id=?`, benefit); err != nil {
		return err
	}
	if !scope.All {
		for _, id := range scope.Products {
			var exists int
			if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? AND id=? AND status='active'`, tenant, plan, id).Scan(&exists); err != nil {
				return err
			}
			if exists != 1 {
				return errors.New("福利适用商品不属于当前方案或已停用")
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO live_plan_benefit_scope_products VALUES(?,?,?,?)`, tenant, plan, benefit, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// A short lease is renewed only by the process executing an explicitly requested
// job. A dead process becomes retryable without cancelling work on another host.
func (s *Store) RenewPoolJob(ctx context.Context, tenant, plan, id int64) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE live_plan_content_jobs SET lease_until=DATE_ADD(CURRENT_TIMESTAMP(3),INTERVAL 2 MINUTE) WHERE tenant_id=? AND plan_id=? AND id=? AND status='running' AND lease_until>CURRENT_TIMESTAMP(3)`, tenant, plan, id)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}
