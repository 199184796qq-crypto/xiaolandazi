package db

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLiveContentRefreshObsolete = errors.New("直播内容更新任务已失效")
var ErrLiveContentRefreshLeaseLost = errors.New("直播内容更新任务租约已失效")

const maxRefreshGenerationAttempts = 5

func (s *Store) MigrateLiveContentRefresh(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_content_refresh_jobs (
	 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
	 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL,
	 plan_id BIGINT UNSIGNED NOT NULL, base_version_id BIGINT UNSIGNED NOT NULL,
	 runtime_session_id BIGINT UNSIGNED NOT NULL, execution_realm VARCHAR(96) NOT NULL,
	 dedupe_key CHAR(64) NOT NULL, status VARCHAR(24) NOT NULL DEFAULT 'pending',
	 policy_json JSON NOT NULL, policy_fingerprint CHAR(64) NOT NULL, source_fingerprint CHAR(64) NOT NULL,
	 scheduled_at DATETIME(3) NOT NULL, publish_after DATETIME(3) NOT NULL,
	 next_attempt_at DATETIME(3) NOT NULL, lease_owner VARCHAR(128) NOT NULL DEFAULT '',
	 lease_token VARCHAR(64) NOT NULL DEFAULT '', lease_until DATETIME(3) NULL,
	 attempts INT UNSIGNED NOT NULL DEFAULT 0, result_version_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
	 draft_json JSON NULL, generated_assets_json JSON NULL,
	 last_error VARCHAR(1024) NOT NULL DEFAULT '',
	 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
	 completed_at DATETIME(3) NULL, cleanup_completed_at DATETIME(3) NULL,
	 PRIMARY KEY(id), UNIQUE KEY uk_content_refresh_dedupe(dedupe_key),
	 KEY idx_content_refresh_claim(execution_realm,status,next_attempt_at,lease_until),
	 KEY idx_content_refresh_room(tenant_id,room_id,status)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS live_content_refresh_events (
	 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, job_id BIGINT UNSIGNED NOT NULL,
	 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL,
	 action VARCHAR(64) NOT NULL, payload_json JSON NOT NULL,
	 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
	 PRIMARY KEY(id), KEY idx_refresh_events_room(tenant_id,room_id,id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`)
	return err
}

func (s *Store) ListLiveContentRefreshCandidates(ctx context.Context, realm string, limit int) ([]model.LiveContentRefreshCandidate, error) {
	return s.ListLiveContentRefreshCandidatesAfter(ctx, realm, 0, limit)
}

// Iterate using the last RuntimeSessionID, reset the cursor to zero on an empty
// page. Completed/disabled old rooms cannot starve later rooms behind LIMIT.
func (s *Store) ListLiveContentRefreshCandidatesAfter(ctx context.Context, realm string, afterRuntimeID int64, limit int) ([]model.LiveContentRefreshCandidate, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.tenant_id,r.room_id,v.plan_id,v.id,r.id,r.execution_realm,r.started_at,COALESCE(v.published_at,v.created_at)
	 FROM live_runtime_sessions r JOIN core_rooms room ON room.id=r.room_id AND room.tenant_id=r.tenant_id
	 JOIN live_room_content_access access ON access.tenant_id=r.tenant_id AND access.room_id=r.room_id
	 LEFT JOIN live_content_policies room_policy ON room_policy.tenant_id=r.tenant_id AND room_policy.room_id=r.room_id
	 LEFT JOIN live_content_policies global_policy ON global_policy.tenant_id=0 AND global_policy.room_id=0
	 LEFT JOIN live_agent_room_plan_selections selected ON selected.tenant_id=r.tenant_id AND selected.room_id=r.room_id
	 LEFT JOIN live_agent_room_plan_publications pub ON pub.tenant_id=r.tenant_id AND pub.room_id=r.room_id
	 JOIN live_agent_plan_versions v ON v.tenant_id=r.tenant_id AND v.room_id=r.room_id AND v.plan_id=COALESCE(selected.plan_id,pub.plan_id)
	 WHERE r.status='running' AND r.execution_realm=? AND r.id>? AND access.dynamic_authorized=1 AND access.selected_mode='ai_dynamic'
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(room_policy.policy_json,'$.auto_refresh_enabled')),JSON_UNQUOTE(JSON_EXTRACT(global_policy.policy_json,'$.auto_refresh_enabled')),'true')='true'
	 AND v.lifecycle_status='published' AND v.id=(SELECT v2.id FROM live_agent_plan_versions v2 WHERE v2.tenant_id=v.tenant_id AND v2.room_id=v.room_id AND v2.plan_id=v.plan_id AND v2.lifecycle_status='published' ORDER BY v2.version_no DESC,v2.id DESC LIMIT 1)
	 AND NOT EXISTS(SELECT 1 FROM mgmt_room_deletions d WHERE d.tenant_id=r.tenant_id AND d.room_id=r.room_id)
	 ORDER BY r.id LIMIT ?`, model.NormalizeExecutionRealm(realm), afterRuntimeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.LiveContentRefreshCandidate{}
	for rows.Next() {
		var c model.LiveContentRefreshCandidate
		if err := rows.Scan(&c.TenantID, &c.RoomID, &c.PlanID, &c.BaseVersionID, &c.RuntimeSessionID, &c.ExecutionRealm, &c.StartedAt, &c.PublishedAt); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func refreshHash(value any) string {
	raw, _ := json.Marshal(value)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type refreshQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func refreshSourceFingerprint(ctx context.Context, q refreshQuerier, tenantID, roomID, planID int64, now time.Time, lock bool) (string, error) {
	queries := []struct {
		sql  string
		args []any
	}{
		{`SELECT CAST(JSON_OBJECT('id',id,'v',version_no,'state',status,'value',fact_value,'key',fact_key) AS CHAR) FROM live_agent_plan_facts WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'v',version_no,'state',status,'updated',updated_at) AS CHAR) FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'v',version_no,'state',status,'updated',updated_at,'window',CASE WHEN starts_at>? THEN 'future' WHEN ends_at<=? THEN 'expired' ELSE 'current' END) AS CHAR) FROM live_agent_plan_benefits WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{now, now, tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'state',status,'analysis',SHA2(analysis_json,256),'text',SHA2(readable_text,256)) AS CHAR) FROM live_agent_plan_scripts WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'v',version_no,'state',status,'updated',updated_at) AS CHAR) FROM live_agent_plan_script_references WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'state',status,'text',canonical_text,'note',note,'updated',updated_at) AS CHAR) FROM live_agent_plan_terms WHERE tenant_id=? AND plan_id=? ORDER BY id`, []any{tenantID, planID}},
		{`SELECT CAST(JSON_OBJECT('id',id,'version',current_version_id,'state',status,'updated',updated_at) AS CHAR) FROM live_policy_scopes WHERE (tenant_id IS NULL OR tenant_id=0 OR tenant_id=?) AND (room_id IS NULL OR room_id=0 OR room_id=?) ORDER BY id`, []any{tenantID, roomID}},
	}
	h := sha256.New()
	for i, query := range queries {
		if lock {
			query.sql += " FOR UPDATE"
		}
		rows, err := q.QueryContext(ctx, query.sql, query.args...)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "table:%d\n", i)
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return "", err
			}
			fmt.Fprintf(h, "%d:%s\n", len(value), value)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", err
		}
		if err := rows.Close(); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Store) GetLiveContentRefreshSourceFingerprint(ctx context.Context, tenantID, roomID, planID int64) (string, error) {
	return refreshSourceFingerprint(ctx, s.db, tenantID, roomID, planID, time.Now().UTC(), false)
}

func readRefreshPolicyTx(ctx context.Context, tx *sql.Tx, tenantID, roomID int64) (model.LiveContentPolicy, string, error) {
	p := model.DefaultLiveContentPolicy()
	var revisions [2]int64
	for i, scope := range [][2]int64{{0, 0}, {tenantID, roomID}} {
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT policy_json,revision FROM live_content_policies WHERE tenant_id=? AND room_id=? FOR UPDATE`, scope[0], scope[1]).Scan(&raw, &revisions[i])
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return p, "", err
		}
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return p, "", err
		}
	}
	var mode string
	var authorized bool
	var accessUpdated time.Time
	err := tx.QueryRowContext(ctx, `SELECT selected_mode,dynamic_authorized,updated_at FROM live_room_content_access WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&mode, &authorized, &accessUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		return p, "", ErrLiveContentRefreshObsolete
	}
	if err != nil {
		return p, "", err
	}
	if !authorized || mode != model.LiveContentAIDynamic {
		return p, "", ErrLiveContentRefreshObsolete
	}
	p.ContentMode = mode
	if err := p.Validate(); err != nil {
		return p, "", err
	}
	if !p.AutoRefreshEnabled {
		return p, "", ErrLiveContentRefreshObsolete
	}
	return p, refreshHash(struct {
		P             model.LiveContentPolicy
		R             [2]int64
		AccessUpdated time.Time
	}{p, revisions, accessUpdated}), nil
}

func refreshTimes(c model.LiveContentRefreshCandidate, p model.LiveContentPolicy) (time.Time, time.Time) {
	anchor := c.PublishedAt
	if c.StartedAt.After(anchor) {
		anchor = c.StartedAt
	}
	return p.ContentExpiry("mainline", anchor)
}

func (s *Store) EnsureLiveContentRefreshJob(ctx context.Context, c model.LiveContentRefreshCandidate, now time.Time) (model.LiveContentRefreshJob, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	defer tx.Rollback()
	if err := lockUsableRoomTx(ctx, tx, c.TenantID, c.RoomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.LiveContentRefreshJob{}, false, nil
		}
		return model.LiveContentRefreshJob{}, false, err
	}
	p, policyHash, err := readRefreshPolicyTx(ctx, tx, c.TenantID, c.RoomID)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return model.LiveContentRefreshJob{}, false, nil
	}
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	if err := validateRefreshRuntimeTx(ctx, tx, c.TenantID, c.RoomID, c.RuntimeSessionID, c.ExecutionRealm); err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	if err := validateRefreshVersionTx(ctx, tx, c.TenantID, c.RoomID, c.PlanID, c.BaseVersionID); err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	fingerprint, err := refreshSourceFingerprint(ctx, tx, c.TenantID, c.RoomID, c.PlanID, now, true)
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	// Invalidate an old in-flight generation if facts or configuration changed.
	if _, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='cancelled',last_error='source_or_policy_changed',lease_token='',lease_until=NULL,next_attempt_at=? WHERE tenant_id=? AND room_id=? AND status IN ('pending','running','retry') AND (runtime_session_id<>? OR base_version_id<>? OR source_fingerprint<>? OR policy_fingerprint<>?)`, now, c.TenantID, c.RoomID, c.RuntimeSessionID, c.BaseVersionID, fingerprint, policyHash); err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='cancelled',last_error='prepared_baseline_changed',lease_token='',lease_until=NULL,next_attempt_at=? WHERE tenant_id=? AND room_id=? AND status='ready' AND (runtime_session_id<>? OR base_version_id<>? OR source_fingerprint<>? OR policy_fingerprint<>?)`, now, c.TenantID, c.RoomID, c.RuntimeSessionID, c.BaseVersionID, fingerprint, policyHash); err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	var active int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM live_content_refresh_jobs WHERE tenant_id=? AND room_id=? AND (status IN ('pending','running','retry','ready') OR (status='cancelled' AND cleanup_completed_at IS NULL)) LIMIT 1 FOR UPDATE`, c.TenantID, c.RoomID).Scan(&active)
	if err == nil {
		return model.LiveContentRefreshJob{}, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.LiveContentRefreshJob{}, false, err
	}
	scheduled, publish := refreshTimes(c, p)
	raw, _ := json.Marshal(p)
	dedupe := refreshHash([]any{c.TenantID, c.RoomID, c.RuntimeSessionID, c.BaseVersionID, policyHash, fingerprint})
	result, err := tx.ExecContext(ctx, `INSERT IGNORE INTO live_content_refresh_jobs(tenant_id,room_id,plan_id,base_version_id,runtime_session_id,execution_realm,dedupe_key,policy_json,policy_fingerprint,source_fingerprint,scheduled_at,publish_after,next_attempt_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.TenantID, c.RoomID, c.PlanID, c.BaseVersionID, c.RuntimeSessionID, model.NormalizeExecutionRealm(c.ExecutionRealm), dedupe, string(raw), policyHash, fingerprint, scheduled, publish, scheduled)
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	return model.LiveContentRefreshJob{ID: id, TenantID: c.TenantID, RoomID: c.RoomID, PlanID: c.PlanID, BaseVersionID: c.BaseVersionID, RuntimeSessionID: c.RuntimeSessionID, ExecutionRealm: c.ExecutionRealm, Policy: p, PolicyFingerprint: policyHash, SourceFingerprint: fingerprint, ScheduledAt: scheduled, PublishAfter: publish, Status: "pending"}, n > 0, nil
}

const contentRefreshSelect = `SELECT id,tenant_id,room_id,plan_id,base_version_id,runtime_session_id,execution_realm,status,CAST(policy_json AS CHAR),policy_fingerprint,source_fingerprint,scheduled_at,publish_after,lease_token,lease_until,attempts,result_version_id,CAST(draft_json AS CHAR),CAST(generated_assets_json AS CHAR) FROM live_content_refresh_jobs`

func scanRefreshJob(scanner interface{ Scan(...any) error }) (model.LiveContentRefreshJob, error) {
	var j model.LiveContentRefreshJob
	var raw string
	var until sql.NullTime
	var draft, assets sql.NullString
	err := scanner.Scan(&j.ID, &j.TenantID, &j.RoomID, &j.PlanID, &j.BaseVersionID, &j.RuntimeSessionID, &j.ExecutionRealm, &j.Status, &raw, &j.PolicyFingerprint, &j.SourceFingerprint, &j.ScheduledAt, &j.PublishAfter, &j.LeaseToken, &until, &j.Attempts, &j.ResultVersionID, &draft, &assets)
	if err != nil {
		return j, err
	}
	if until.Valid {
		j.LeaseUntil = &until.Time
	}
	err = json.Unmarshal([]byte(raw), &j.Policy)
	if err == nil && draft.Valid && draft.String != "null" {
		err = json.Unmarshal([]byte(draft.String), &j.Draft)
	}
	if err == nil && assets.Valid && assets.String != "null" {
		err = json.Unmarshal([]byte(assets.String), &j.GeneratedAssets)
	}
	return j, err
}

func (s *Store) ClaimLiveContentRefreshJob(ctx context.Context, owner, realm string, now time.Time, lease time.Duration) (model.LiveContentRefreshJob, bool, error) {
	return s.claimLiveContentRefreshJob(ctx, owner, realm, now, lease, false)
}

func (s *Store) ClaimReadyLiveContentRefreshJob(ctx context.Context, owner, realm string, now time.Time, lease time.Duration) (model.LiveContentRefreshJob, bool, error) {
	return s.claimLiveContentRefreshJob(ctx, owner, realm, now, lease, true)
}

func (s *Store) claimLiveContentRefreshJob(ctx context.Context, owner, realm string, now time.Time, lease time.Duration, ready bool) (model.LiveContentRefreshJob, bool, error) {
	if strings.TrimSpace(owner) == "" || lease < time.Second {
		return model.LiveContentRefreshJob{}, false, fmt.Errorf("refresh lease owner/duration required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveContentRefreshJob{}, false, err
	}
	defer tx.Rollback()
	// Expired crashed workers also consume the generation budget. A persistent
	// cancellation outbox cleans their assets; an unchanged source is not retried forever.
	statuses := "('pending','running','retry')"
	if ready {
		statuses = "('ready')"
	} else {
		if _, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='cancelled',last_error='generation_attempts_exhausted',lease_token='',lease_until=NULL,next_attempt_at=?,updated_at=? WHERE execution_realm=? AND status IN ('pending','running','retry') AND result_version_id=0 AND attempts>=? AND (lease_until IS NULL OR lease_until<=?)`, now, now, model.NormalizeExecutionRealm(realm), maxRefreshGenerationAttempts, now); err != nil {
			return model.LiveContentRefreshJob{}, false, err
		}
	}
	j, err := scanRefreshJob(tx.QueryRowContext(ctx, contentRefreshSelect+` WHERE execution_realm=? AND status IN `+statuses+` AND next_attempt_at<=? AND (lease_until IS NULL OR lease_until<=?) ORDER BY next_attempt_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, model.NormalizeExecutionRealm(realm), now, now))
	if errors.Is(err, sql.ErrNoRows) {
		return j, false, tx.Commit()
	}
	if err != nil {
		return j, false, err
	}
	var token [24]byte
	if _, err = rand.Read(token[:]); err != nil {
		return j, false, err
	}
	j.LeaseToken = hex.EncodeToString(token[:])
	until := now.Add(lease)
	j.LeaseUntil = &until
	j.Attempts++
	if j.ResultVersionID > 0 {
		j.Status = "ready"
	} else {
		j.Status = "running"
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status=?,lease_owner=?,lease_token=?,lease_until=?,attempts=attempts+1,updated_at=? WHERE id=?`, j.Status, owner, j.LeaseToken, until, now, j.ID)
	if err != nil {
		return j, false, err
	}
	if err = tx.Commit(); err != nil {
		return j, false, err
	}
	return j, true, nil
}

func validateRefreshRuntimeTx(ctx context.Context, tx *sql.Tx, tenantID, roomID, sessionID int64, realm string) error {
	var status, execution string
	err := tx.QueryRowContext(ctx, `SELECT status,execution_realm FROM live_runtime_sessions WHERE id=? AND tenant_id=? AND room_id=? FOR UPDATE`, sessionID, tenantID, roomID).Scan(&status, &execution)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveContentRefreshObsolete
	}
	if err != nil {
		return err
	}
	if status != "running" || model.NormalizeExecutionRealm(execution) != model.NormalizeExecutionRealm(realm) {
		return ErrLiveContentRefreshObsolete
	}
	return nil
}

func validateRefreshVersionTx(ctx context.Context, tx *sql.Tx, tenantID, roomID, planID, expectedID int64) error {
	var selected int64
	err := tx.QueryRowContext(ctx, `SELECT plan_id FROM live_agent_room_plan_selections WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&selected)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT plan_id FROM live_agent_room_plan_publications WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&selected)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveContentRefreshObsolete
	}
	if err != nil {
		return err
	}
	if selected != planID {
		return ErrLiveContentRefreshObsolete
	}
	var binding int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_room_bindings WHERE tenant_id=? AND room_id=? AND plan_id=? AND status='active' LIMIT 1 FOR UPDATE`, tenantID, roomID, planID).Scan(&binding)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveContentRefreshObsolete
	}
	if err != nil {
		return err
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_versions WHERE tenant_id=? AND room_id=? AND plan_id=? AND lifecycle_status='published' ORDER BY version_no DESC,id DESC LIMIT 1 FOR UPDATE`, tenantID, roomID, planID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveContentRefreshObsolete
	}
	if err != nil {
		return err
	}
	if current != expectedID {
		return ErrLiveContentRefreshObsolete
	}
	return nil
}

func validateRefreshLease(j model.LiveContentRefreshJob, token string, now time.Time) error {
	if token == "" || j.LeaseToken != token || j.LeaseUntil == nil || !j.LeaseUntil.After(now) || (j.Status != "running" && j.Status != "ready") {
		return ErrLiveContentRefreshLeaseLost
	}
	return nil
}

func (s *Store) lockRefreshJobTx(ctx context.Context, tx *sql.Tx, job model.LiveContentRefreshJob, now time.Time) (model.LiveContentRefreshJob, error) {
	// All mutators take the room lock before the job lock. This agrees with
	// scheduling/deletion and prevents room->job versus job->room deadlocks.
	var roomMissing bool
	if err := lockUsableRoomTx(ctx, tx, job.TenantID, job.RoomID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || strings.Contains(err.Error(), "永久删除") {
			roomMissing = true
		} else {
			return model.LiveContentRefreshJob{}, err
		}
	}
	j, err := scanRefreshJob(tx.QueryRowContext(ctx, contentRefreshSelect+` WHERE id=? FOR UPDATE`, job.ID))
	if err != nil {
		return j, err
	}
	if err = validateRefreshLease(j, job.LeaseToken, now); err != nil {
		return j, err
	}
	if j.TenantID != job.TenantID || j.RoomID != job.RoomID {
		return j, ErrLiveContentRefreshLeaseLost
	}
	if roomMissing {
		return j, ErrLiveContentRefreshObsolete
	}
	if err = validateRefreshRuntimeTx(ctx, tx, j.TenantID, j.RoomID, j.RuntimeSessionID, j.ExecutionRealm); err != nil {
		return j, err
	}
	if err = validateRefreshVersionTx(ctx, tx, j.TenantID, j.RoomID, j.PlanID, j.BaseVersionID); err != nil {
		return j, err
	}
	_, policyHash, err := readRefreshPolicyTx(ctx, tx, j.TenantID, j.RoomID)
	if err != nil {
		return j, err
	}
	if policyHash != j.PolicyFingerprint {
		return j, ErrLiveContentRefreshObsolete
	}
	fingerprint, err := refreshSourceFingerprint(ctx, tx, j.TenantID, j.RoomID, j.PlanID, now, true)
	if err != nil {
		return j, err
	}
	if fingerprint != j.SourceFingerprint {
		return j, ErrLiveContentRefreshObsolete
	}
	return j, nil
}

func (s *Store) ValidateLiveContentRefreshJob(ctx context.Context, job model.LiveContentRefreshJob, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = s.lockRefreshJobTx(ctx, tx, job, now)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return cancelRefreshTx(ctx, tx, job.ID, "baseline_changed")
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func cancelRefreshTx(ctx context.Context, tx *sql.Tx, id int64, reason string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='cancelled',last_error=?,lease_token='',lease_until=NULL,next_attempt_at=CURRENT_TIMESTAMP(3) WHERE id=?`, reason, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrLiveContentRefreshObsolete
}

func (s *Store) RenewLiveContentRefreshLease(ctx context.Context, job model.LiveContentRefreshJob, now time.Time, lease time.Duration) error {
	if lease < time.Second {
		return fmt.Errorf("refresh lease too short")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = s.lockRefreshJobTx(ctx, tx, job, now)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return cancelRefreshTx(ctx, tx, job.ID, "baseline_changed")
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET lease_until=?,updated_at=? WHERE id=? AND lease_token=?`, now.Add(lease), now, job.ID, job.LeaseToken)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FailLiveContentRefreshJob(ctx context.Context, job model.LiveContentRefreshJob, reason string, now time.Time) error {
	delay := time.Duration(1<<min(max(job.Attempts, 0), 5)) * 15 * time.Second
	text := []rune(reason)
	if len(text) > 1000 {
		text = text[:1000]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status=CASE WHEN result_version_id>0 THEN 'ready' WHEN attempts>=? THEN 'cancelled' ELSE 'retry' END,last_error=?,next_attempt_at=GREATEST(?,CASE WHEN result_version_id>0 THEN publish_after ELSE scheduled_at END),lease_owner='',lease_token='',lease_until=NULL,updated_at=? WHERE id=? AND lease_token=? AND lease_until>? AND status IN ('running','ready')`, maxRefreshGenerationAttempts, string(text), now.Add(delay), now, job.ID, job.LeaseToken, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLiveContentRefreshLeaseLost
	}
	return nil
}

// Commit produces a durable ready version, never marks Core application complete.
// It releases the lease and schedules the ready retry no earlier than PublishAfter.
func (s *Store) CommitLiveContentRefresh(ctx context.Context, job model.LiveContentRefreshJob, input model.CreateLiveAgentPlanVersionInput, now time.Time) (model.LiveAgentPlanVersion, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	defer tx.Rollback()
	j, err := s.lockRefreshJobTx(ctx, tx, job, now)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return model.LiveAgentPlanVersion{}, cancelRefreshTx(ctx, tx, job.ID, "baseline_changed")
	}
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if j.ResultVersionID > 0 {
		return model.LiveAgentPlanVersion{}, fmt.Errorf("refresh already prepared")
	}
	if input.RoomID != j.RoomID || len(input.Variants) == 0 {
		return model.LiveAgentPlanVersion{}, fmt.Errorf("invalid refresh result scope")
	}
	formalCount := 0
	for _, v := range input.Variants {
		if v.IsFormal {
			formalCount++
		}
		if v.IsFormal && (v.AudioAssetID <= 0 || strings.TrimSpace(v.AudioURL) == "" || strings.TrimSpace(v.Text) == "") {
			return model.LiveAgentPlanVersion{}, fmt.Errorf("refresh formal audio is incomplete")
		}
	}
	if formalCount == 0 {
		return model.LiveAgentPlanVersion{}, fmt.Errorf("refresh has no formal audio")
	}
	if input.GenerationContext == nil {
		input.GenerationContext = map[string]any{}
	}
	input.GenerationContext["content_refresh_job_id"] = j.ID
	input.GenerationContext["content_mode"] = model.LiveContentAIDynamic
	input.GenerationContext["content_refresh_base_version_id"] = j.BaseVersionID
	voice, _ := json.Marshal(input.VoiceIdentity)
	variants, _ := json.Marshal(input.Variants)
	generation, err := json.Marshal(input.GenerationContext)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	var versionNo int64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_no),0)+1 FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND room_id=?`, j.TenantID, j.PlanID, j.RoomID).Scan(&versionNo)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO live_agent_plan_versions(tenant_id,plan_id,room_id,version_no,lifecycle_status,duration_minutes,round_minutes,voice_identity_json,variants_json,generation_context_json) VALUES(?,?,?,?,'prepared',?,?,?,?,?)`, j.TenantID, j.PlanID, j.RoomID, versionNo, input.DurationMinutes, input.RoundMinutes, string(voice), string(variants), string(generation))
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='ready',result_version_id=?,next_attempt_at=GREATEST(publish_after,?),lease_token='',lease_owner='',lease_until=NULL,last_error='',updated_at=? WHERE id=?`, id, now, now, j.ID)
	if err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if err = auditRefreshTx(ctx, tx, j, "prepared", map[string]any{"result_version_id": id, "base_version_id": j.BaseVersionID}, now); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAgentPlanVersion{}, err
	}
	return s.GetLiveAgentPlanVersion(ctx, j.TenantID, j.PlanID, id)
}

func (s *Store) CompleteLiveContentRefreshJob(ctx context.Context, job model.LiveContentRefreshJob, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := s.lockRefreshJobTx(ctx, tx, job, now)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return cancelRefreshTx(ctx, tx, job.ID, "baseline_changed")
	}
	if err != nil {
		return err
	}
	if j.Status != "ready" || j.ResultVersionID <= 0 || now.Before(j.PublishAfter) {
		return fmt.Errorf("refresh is not ready for application")
	}
	// Publishing is deferred until Core has applied the boundary update. Prepared
	// audio must not leak into UI/runtime selectors before its two-hour deadline.
	result, err := tx.ExecContext(ctx, `UPDATE live_agent_plan_versions SET lifecycle_status='published',published_at=? WHERE id=? AND tenant_id=? AND room_id=? AND plan_id=? AND lifecycle_status='prepared'`, now, j.ResultVersionID, j.TenantID, j.RoomID, j.PlanID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return cancelRefreshTx(ctx, tx, j.ID, "prepared_version_changed")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_versions SET lifecycle_status='superseded' WHERE tenant_id=? AND room_id=? AND plan_id=? AND lifecycle_status='published' AND id<>?`, j.TenantID, j.RoomID, j.PlanID, j.ResultVersionID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO live_agent_room_plan_publications(tenant_id,room_id,plan_id,version_id,published_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE plan_id=VALUES(plan_id),version_id=VALUES(version_id),published_at=VALUES(published_at)`, j.TenantID, j.RoomID, j.PlanID, j.ResultVersionID, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='completed',completed_at=?,lease_token='',lease_owner='',lease_until=NULL,last_error='',updated_at=? WHERE id=?`, now, now, j.ID)
	if err != nil {
		return err
	}
	if err = auditRefreshTx(ctx, tx, j, "applied", map[string]any{"result_version_id": j.ResultVersionID}, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelInactiveLiveContentRefreshJobs(ctx context.Context, realm string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE live_content_refresh_jobs job
	 LEFT JOIN core_rooms room ON room.id=job.room_id AND room.tenant_id=job.tenant_id
	 LEFT JOIN mgmt_room_deletions deleted ON deleted.room_id=job.room_id AND deleted.tenant_id=job.tenant_id
	 LEFT JOIN live_runtime_sessions runtime ON runtime.id=job.runtime_session_id
	 LEFT JOIN live_room_content_access access ON access.tenant_id=job.tenant_id AND access.room_id=job.room_id
	 LEFT JOIN live_content_policies room_policy ON room_policy.tenant_id=job.tenant_id AND room_policy.room_id=job.room_id
	 LEFT JOIN live_content_policies global_policy ON global_policy.tenant_id=0 AND global_policy.room_id=0
	 SET job.status='cancelled',job.last_error='room_stopped_deleted_or_entitlement_revoked',job.lease_token='',job.lease_until=NULL,job.next_attempt_at=?,job.updated_at=?
	 WHERE job.execution_realm=? AND job.status IN ('pending','running','retry','ready')
	 AND (room.id IS NULL OR deleted.room_id IS NOT NULL OR runtime.status<>'running' OR runtime.id IS NULL OR access.dynamic_authorized IS NULL OR access.dynamic_authorized=0 OR access.selected_mode<>'ai_dynamic' OR COALESCE(JSON_UNQUOTE(JSON_EXTRACT(room_policy.policy_json,'$.auto_refresh_enabled')),JSON_UNQUOTE(JSON_EXTRACT(global_policy.policy_json,'$.auto_refresh_enabled')),'true')<>'true')`, now, now, model.NormalizeExecutionRealm(realm))
	return err
}

func (s *Store) SaveLiveContentRefreshDraft(ctx context.Context, job model.LiveContentRefreshJob, input model.CreateLiveAgentPlanVersionInput, now time.Time) error {
	if input.RoomID != job.RoomID {
		return fmt.Errorf("refresh draft scope mismatch")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := s.lockRefreshJobTx(ctx, tx, job, now)
	if errors.Is(err, ErrLiveContentRefreshObsolete) {
		return cancelRefreshTx(ctx, tx, job.ID, "baseline_changed")
	}
	if err != nil {
		return err
	}
	if j.ResultVersionID > 0 {
		return fmt.Errorf("prepared refresh is immutable")
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET draft_json=?,updated_at=? WHERE id=?`, string(raw), now, job.ID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TrackLiveContentRefreshAsset(ctx context.Context, job model.LiveContentRefreshJob, assetID int64, now time.Time) error {
	if assetID <= 0 {
		return fmt.Errorf("invalid refresh asset")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET generated_assets_json=JSON_ARRAY_APPEND(COALESCE(generated_assets_json,JSON_ARRAY()),'$',CAST(? AS UNSIGNED)),updated_at=? WHERE id=? AND lease_token=? AND lease_until>? AND status='running'`, assetID, now, job.ID, job.LeaseToken, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLiveContentRefreshLeaseLost
	}
	return nil
}

func (s *Store) DeferLiveContentRefreshJob(ctx context.Context, job model.LiveContentRefreshJob, now time.Time, delay time.Duration) error {
	if delay < time.Second {
		delay = time.Second
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET next_attempt_at=CASE WHEN result_version_id>0 THEN GREATEST(publish_after,?) ELSE ? END,status=CASE WHEN result_version_id>0 THEN 'ready' ELSE 'retry' END,attempts=GREATEST(CAST(attempts AS SIGNED)-CASE WHEN result_version_id>0 THEN 0 ELSE 1 END,0),lease_token='',lease_owner='',lease_until=NULL,updated_at=? WHERE id=? AND lease_token=? AND lease_until>? AND status IN ('running','ready')`, now.Add(delay), now.Add(delay), now, job.ID, job.LeaseToken, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLiveContentRefreshLeaseLost
	}
	return nil
}

func (s *Store) CancelLiveContentRefreshJob(ctx context.Context, job model.LiveContentRefreshJob, reason string, now time.Time) error {
	text := []rune(reason)
	if len(text) > 1000 {
		text = text[:1000]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE live_content_refresh_jobs SET status='cancelled',last_error=?,lease_token='',lease_owner='',lease_until=NULL,next_attempt_at=?,updated_at=? WHERE id=? AND lease_token=? AND lease_until>? AND status IN ('running','ready')`, string(text), now, now, job.ID, job.LeaseToken, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrLiveContentRefreshLeaseLost
	}
	return nil
}

// The operator panel receives execution metadata only, never customer text,
// generated asset URLs or lease tokens. Consent must be checked by its handler.
func (s *Store) GetLatestLiveContentRefreshStatus(ctx context.Context, tenantID, roomID int64) (map[string]any, error) {
	var id, resultID int64
	var status, lastError string
	var attempts int
	var scheduled, publish, updated time.Time
	err := s.db.QueryRowContext(ctx, `SELECT id,status,scheduled_at,publish_after,attempts,last_error,result_version_id,updated_at FROM live_content_refresh_jobs WHERE tenant_id=? AND room_id=? ORDER BY id DESC LIMIT 1`, tenantID, roomID).Scan(&id, &status, &scheduled, &publish, &attempts, &lastError, &resultID, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "status": status, "scheduled_at": scheduled, "publish_after": publish, "attempts": attempts, "last_error": lastError, "result_version_id": resultID, "updated_at": updated}, nil
}
