package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

func (s *Store) createScopedVoiceAsset(ctx context.Context, in model.CreateMediaAssetInput) (model.MediaAsset, error) {
	meta := map[string]any{}
	for k, v := range in.Metadata {
		meta[k] = v
	}
	in.Metadata = meta
	captured, known := voiceasset.MetadataStamp(in.TenantID, in.AssetType, meta)
	if st, ok := voiceasset.FromContext(ctx); ok {
		if st.Scope.Tenant != in.TenantID || st.Scope.Plan != captured.Scope.Plan || st.Scope.Category != captured.Scope.Category {
			return model.MediaAsset{}, voiceasset.ErrCleared
		}
		captured = st
		st.Metadata(meta)
	}
	if !known {
		return model.MediaAsset{}, errors.New("声音归属无法确认")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return model.MediaAsset{}, e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, captured.Scope); e != nil {
		return model.MediaAsset{}, e
	}
	current, e := voiceStamp(ctx, tx, captured.Scope, true)
	if e != nil {
		return model.MediaAsset{}, e
	}
	status := "active"
	if current != captured {
		status = "quarantined"
	}
	if e = lockVoiceObjectKeys(ctx, tx, model.MediaAsset{StorageDriver: in.StorageDriver, StorageBucket: in.StorageBucket, ObjectKey: in.ObjectKey, Metadata: meta}); e != nil {
		return model.MediaAsset{}, e
	}
	b, e := json.Marshal(meta)
	if e != nil {
		return model.MediaAsset{}, e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO media_assets(tenant_id,agent_id,asset_type,original_name,storage_driver,storage_bucket,object_key,mime_type,size_bytes,duration_ms,checksum_sha256,status,metadata_json,created_by_user_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,CAST(? AS JSON),?)`, in.TenantID, in.AgentID, in.AssetType, strings.TrimSpace(in.OriginalName), in.StorageDriver, in.StorageBucket, in.ObjectKey, in.MIMEType, in.SizeBytes, in.DurationMS, in.ChecksumSHA256, status, b, in.CreatedByUserID)
	if e != nil {
		return model.MediaAsset{}, e
	}
	id, e := result.LastInsertId()
	if e != nil {
		return model.MediaAsset{}, e
	}
	if status == "quarantined" {
		if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO voice_cleanup_items(job_id,asset_id,state) SELECT id,?,'pending' FROM voice_cleanup_jobs WHERE tenant_id=? AND plan_id=? AND category=? AND (voice_key='' OR voice_key=?) AND (CAST(JSON_EXTRACT(after_json,'$.plan_epoch') AS UNSIGNED)>? OR (CAST(JSON_EXTRACT(after_json,'$.plan_epoch') AS UNSIGNED)=? AND voice_key<>'' AND CAST(JSON_EXTRACT(after_json,'$.voice_epoch') AS UNSIGNED)>?))`, id, captured.Scope.Tenant, captured.Scope.Plan, captured.Scope.Category, captured.Scope.Voice, captured.PlanEpoch, captured.PlanEpoch, captured.VoiceEpoch); e != nil {
			return model.MediaAsset{}, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE voice_cleanup_jobs j JOIN voice_cleanup_items i ON i.job_id=j.id SET j.state='processing' WHERE i.asset_id=? AND j.tenant_id=?`, id, captured.Scope.Tenant); e != nil {
			return model.MediaAsset{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return model.MediaAsset{}, e
	}
	return s.RawVoiceAsset(ctx, in.TenantID, id)
}

func (s *Store) ensureVoiceAssetCleanupSchema(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS voice_cache_epochs (tenant_id BIGINT NOT NULL,plan_id BIGINT NOT NULL,category VARCHAR(16) NOT NULL,voice_key VARCHAR(64) NOT NULL DEFAULT '',epoch BIGINT UNSIGNED NOT NULL DEFAULT 0,updated_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),PRIMARY KEY(tenant_id,plan_id,category,voice_key)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_cleanup_jobs (id CHAR(64) NOT NULL,tenant_id BIGINT NOT NULL,plan_id BIGINT NOT NULL,category VARCHAR(16) NOT NULL,voice_key VARCHAR(64) NOT NULL DEFAULT '',before_json TEXT NOT NULL,after_json TEXT NOT NULL,actor_user_id BIGINT NOT NULL,mode VARCHAR(24) NOT NULL DEFAULT 'history',next_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),last_error VARCHAR(64) NOT NULL DEFAULT '',state VARCHAR(32) NOT NULL DEFAULT 'scanning',scan_after BIGINT NOT NULL DEFAULT 0,scan_done BOOLEAN NOT NULL DEFAULT FALSE,created_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),PRIMARY KEY(id),KEY idx_voice_cleanup_plan(tenant_id,plan_id,created_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_cleanup_items (job_id CHAR(64) NOT NULL,asset_id BIGINT NOT NULL,state VARCHAR(32) NOT NULL DEFAULT 'pending',attempts INT NOT NULL DEFAULT 0,next_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),last_error VARCHAR(255) NOT NULL DEFAULT '',confirmed_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,PRIMARY KEY(job_id,asset_id),KEY idx_voice_cleanup_due(state,next_at)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_asset_leases (tenant_id BIGINT NOT NULL,asset_id BIGINT NOT NULL,room_id BIGINT NOT NULL,session_id BIGINT NOT NULL,program_id VARCHAR(128) NOT NULL DEFAULT '',track_id VARCHAR(160) NOT NULL,state VARCHAR(24) NOT NULL DEFAULT 'pending',created_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),updated_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),PRIMARY KEY(tenant_id,asset_id,session_id,track_id),KEY idx_voice_lease_asset(asset_id,state)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_local_sources (tenant_id BIGINT NOT NULL,plan_id BIGINT NOT NULL,category VARCHAR(16) NOT NULL,voice_key VARCHAR(64) NOT NULL DEFAULT '',path_hash CHAR(64) NOT NULL,path_json TEXT NOT NULL,stamp_json TEXT NOT NULL,asset_id BIGINT NOT NULL DEFAULT 0,created_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),state VARCHAR(24) NOT NULL DEFAULT 'active',PRIMARY KEY(tenant_id,path_hash),KEY idx_voice_local_scope(tenant_id,plan_id,category,state)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, e := s.db.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return s.ensureVoiceAssetRetirementSchema(ctx)
}

type epochQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func voiceStamp(ctx context.Context, q epochQuery, scope voiceasset.Scope, locking bool) (voiceasset.Stamp, error) {
	st := voiceasset.Stamp{Scope: scope}
	if e := scope.Validate(); e != nil {
		return st, e
	}
	suffix := ""
	if locking {
		suffix = " FOR UPDATE"
	}
	get := func(key string) (uint64, error) {
		var epoch uint64
		e := q.QueryRowContext(ctx, `SELECT epoch FROM voice_cache_epochs WHERE tenant_id=? AND plan_id=? AND category=? AND voice_key=?`+suffix, scope.Tenant, scope.Plan, scope.Category, key).Scan(&epoch)
		if errors.Is(e, sql.ErrNoRows) {
			e = nil
		}
		return epoch, e
	}
	var e error
	st.PlanEpoch, e = get("")
	if e != nil {
		return st, e
	}
	if scope.Voice != "" {
		st.VoiceEpoch, e = get(scope.Voice)
	}
	return st, e
}
func (s *Store) VoiceCacheStamp(ctx context.Context, scope voiceasset.Scope) (voiceasset.Stamp, error) {
	return voiceStamp(ctx, s.db, scope, false)
}
func (s *Store) CheckVoiceCacheStamp(ctx context.Context, st voiceasset.Stamp) error {
	current, e := s.VoiceCacheStamp(ctx, st.Scope)
	if e != nil {
		return e
	}
	if !st.Equal(current) {
		return voiceasset.ErrCleared
	}
	return nil
}
func lockVoiceScope(ctx context.Context, tx *sql.Tx, scope voiceasset.Scope) error {
	if e := scope.Validate(); e != nil {
		return e
	}
	keys := []string{""}
	if scope.Voice != "" {
		keys = append(keys, scope.Voice)
	}
	for _, key := range keys {
		if _, e := tx.ExecContext(ctx, `INSERT INTO voice_cache_epochs(tenant_id,plan_id,category,voice_key,epoch) VALUES(?,?,?,?,0) ON DUPLICATE KEY UPDATE epoch=epoch`, scope.Tenant, scope.Plan, scope.Category, key); e != nil {
			return e
		}
	}
	_, e := voiceStamp(ctx, tx, scope, true)
	return e
}
func (s *Store) VoiceAssetAllowed(ctx context.Context, a model.MediaAsset) error {
	if a.Status != "active" {
		return voiceasset.ErrCleared
	}
	st, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
	if !ok {
		if _, tagged := a.Metadata["voice_cache_stamp"]; tagged {
			return voiceasset.ErrCleared
		}
		if a.AssetType == "generated_voice" {
			if plan, e := strconv.ParseInt(fmt.Sprint(a.Metadata["plan_id"]), 10, 64); e == nil && plan > 0 {
				var n int
				if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_cache_epochs WHERE tenant_id=? AND plan_id=? AND epoch>0`, a.TenantID, plan).Scan(&n); e != nil {
					return e
				}
				if n > 0 {
					return voiceasset.ErrCleared
				}
			}
		}
		return nil
	}
	if key, _ := a.Metadata["paragraph_key"].(string); key != "" {
		if e := s.CheckVoiceParagraph(ctx, a.TenantID, st.Scope.Plan, key); e != nil {
			return e
		}
	}
	return s.CheckVoiceCacheStamp(ctx, st)
}
func (s *Store) ClearVoiceAssets(ctx context.Context, r voiceasset.ClearRequest) (voiceasset.Job, error) {
	if e := r.Validate(); e != nil {
		return voiceasset.Job{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return voiceasset.Job{}, e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, r.Scope); e != nil {
		return voiceasset.Job{}, e
	}
	// An identical retry returns its original job even if its epoch has advanced.
	if j, e := readVoiceJob(ctx, tx, r.Scope.Tenant, r.RequestID); e == nil {
		if j.Scope != r.Scope || (r.Mode != "" && j.Mode != r.Mode) {
			return voiceasset.Job{}, voiceasset.ErrConflict
		}
		return j, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return voiceasset.Job{}, e
	}
	before, e := voiceStamp(ctx, tx, r.Scope, true)
	if e != nil {
		return voiceasset.Job{}, e
	}
	if before != r.Expected {
		return voiceasset.Job{}, voiceasset.ErrConflict
	}
	after := before
	mode := r.Mode
	if mode == "" {
		mode = "history"
	}
	if mode == "history" {
		if (r.Scope.Voice == "" && before.PlanEpoch == math.MaxUint64) || (r.Scope.Voice != "" && before.VoiceEpoch == math.MaxUint64) {
			return voiceasset.Job{}, voiceasset.ErrConflict
		}
		if r.Scope.Voice == "" {
			after.PlanEpoch++
		} else {
			after.VoiceEpoch++
		}
	}
	value := after.PlanEpoch
	if r.Scope.Voice != "" {
		value = after.VoiceEpoch
	}
	if _, e = tx.ExecContext(ctx, `UPDATE voice_cache_epochs SET epoch=?,updated_at=UTC_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND category=? AND voice_key=?`, value, r.Scope.Tenant, r.Scope.Plan, r.Scope.Category, r.Scope.Voice); e != nil {
		return voiceasset.Job{}, e
	}
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	if _, e = tx.ExecContext(ctx, `INSERT INTO voice_cleanup_jobs(id,tenant_id,plan_id,category,voice_key,before_json,after_json,actor_user_id,mode) VALUES(?,?,?,?,?,?,?,?,?)`, r.RequestID, r.Scope.Tenant, r.Scope.Plan, r.Scope.Category, r.Scope.Voice, b, a, r.Actor, mode); e != nil {
		return voiceasset.Job{}, e
	}
	if e = tx.Commit(); e != nil {
		return voiceasset.Job{}, e
	}
	return s.VoiceCleanupJob(ctx, r.Scope.Tenant, r.RequestID)
}

type jobQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readVoiceJob(ctx context.Context, q jobQuery, tenant int64, id string) (voiceasset.Job, error) {
	var j voiceasset.Job
	var b, a []byte
	e := q.QueryRowContext(ctx, `SELECT id,tenant_id,plan_id,category,voice_key,before_json,after_json,state,scan_after,scan_done,created_at,mode FROM voice_cleanup_jobs WHERE tenant_id=? AND id=?`, tenant, id).Scan(&j.ID, &j.Scope.Tenant, &j.Scope.Plan, &j.Scope.Category, &j.Scope.Voice, &b, &a, &j.State, &j.ScanAfter, &j.ScanDone, &j.Created, &j.Mode)
	if e != nil {
		return j, e
	}
	if e = json.Unmarshal(b, &j.Before); e != nil {
		return j, e
	}
	e = json.Unmarshal(a, &j.After)
	return j, e
}
func (s *Store) VoiceCleanupJob(ctx context.Context, tenant int64, id string) (voiceasset.Job, error) {
	j, e := readVoiceJob(ctx, s.db, tenant, id)
	if e != nil {
		return j, e
	}
	j.Counts = map[string]int64{}
	rows, e := s.db.QueryContext(ctx, `SELECT state,COUNT(*),COALESCE(SUM(confirmed_bytes),0) FROM voice_cleanup_items WHERE job_id=? GROUP BY state`, id)
	if e != nil {
		return j, e
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n, bytes int64
		if e = rows.Scan(&state, &n, &bytes); e != nil {
			return j, e
		}
		j.Counts[state] = n
		j.Counts["confirmed_bytes"] += bytes
	}
	return j, rows.Err()
}
func (s *Store) ListVoiceCleanupJobs(ctx context.Context, tenant, plan int64) ([]voiceasset.Job, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM voice_cleanup_jobs WHERE tenant_id=? AND plan_id=? ORDER BY created_at DESC LIMIT 30`, tenant, plan)
	if e != nil {
		return nil, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var out []voiceasset.Job
	for _, id := range ids {
		j, e := s.VoiceCleanupJob(ctx, tenant, id)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, nil
}

const voiceAssetColumns = `id,tenant_id,agent_id,asset_type,original_name,storage_driver,storage_bucket,object_key,mime_type,size_bytes,duration_ms,checksum_sha256,status,CAST(metadata_json AS CHAR),created_by_user_id,created_at,updated_at`

func (s *Store) VoiceCleanupAssets(ctx context.Context, scope voiceasset.Scope, after int64, limit int) ([]model.MediaAsset, error) {
	if e := scope.Validate(); e != nil {
		return nil, e
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, e := s.db.QueryContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE tenant_id=? AND asset_type='generated_voice' AND id>? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.plan_id'))=CAST(? AS CHAR) ORDER BY id LIMIT ?`, scope.Tenant, after, scope.Plan, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []model.MediaAsset
	for rows.Next() {
		a, e := scanMediaAsset(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) ScanVoiceCleanupJob(ctx context.Context, j voiceasset.Job) error {
	if j.ScanDone || j.Mode == "retired" {
		return nil
	}
	assets, e := s.VoiceCleanupAssets(ctx, j.Scope, j.ScanAfter, 200)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, j.Scope); e != nil {
		return e
	}
	last := j.ScanAfter
	for _, a := range assets {
		last = a.ID
		st, known := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
		state := "review_required"
		if j.Mode == "local_only" {
			continue
		}
		if known {
			if st.Scope.Category != j.Scope.Category {
				continue
			}
			if j.Scope.Voice != "" && st.Scope.Voice != j.Scope.Voice {
				if st.Scope.Voice != "" {
					continue
				}
			} else {
				// Compare against this operation's cutoff, not a later clear. New epoch
				// assets created after this clear must never be swept by an older job.
				old := st.PlanEpoch < j.After.PlanEpoch || (st.PlanEpoch == j.After.PlanEpoch && j.Scope.Voice != "" && st.VoiceEpoch < j.After.VoiceEpoch)
				if !old {
					continue
				}
				state = "pending"
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO voice_cleanup_items(job_id,asset_id,state) VALUES(?,?,?)`, j.ID, a.ID, state); e != nil {
			return e
		}
		if state == "pending" {
			if _, e = tx.ExecContext(ctx, `UPDATE media_assets SET status='quarantined' WHERE id=? AND tenant_id=? AND status='active'`, a.ID, a.TenantID); e != nil {
				return e
			}
		}
	}
	done := len(assets) < 200
	if _, e = tx.ExecContext(ctx, `UPDATE voice_cleanup_jobs SET scan_after=GREATEST(scan_after,?),scan_done=?,state=? WHERE id=? AND scan_after=?`, last, done, "processing", j.ID, j.ScanAfter); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) DueVoiceCleanupJobs(ctx context.Context) ([]voiceasset.Job, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT tenant_id,id FROM voice_cleanup_jobs WHERE state NOT IN('completed','review_required','dry_run') AND next_at<=UTC_TIMESTAMP(3) ORDER BY next_at,created_at LIMIT 20`)
	if e != nil {
		return nil, e
	}
	type key struct {
		tenant int64
		id     string
	}
	var keys []key
	for rows.Next() {
		var k key
		if e = rows.Scan(&k.tenant, &k.id); e != nil {
			rows.Close()
			return nil, e
		}
		keys = append(keys, k)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var jobs []voiceasset.Job
	for _, k := range keys {
		j, e := s.VoiceCleanupJob(ctx, k.tenant, k.id)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}
func (s *Store) RawVoiceAsset(ctx context.Context, tenant, id int64) (model.MediaAsset, error) {
	return scanMediaAsset(s.db.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE tenant_id=? AND id=?`, tenant, id))
}
func (s *Store) DueVoiceCleanupItems(ctx context.Context, j voiceasset.Job) ([]voiceasset.Candidate, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT asset_id,state,attempts,next_at FROM voice_cleanup_items WHERE job_id=? AND state IN('pending','waiting_refs','retry','deleting') AND next_at<=UTC_TIMESTAMP(3) ORDER BY asset_id LIMIT 50`, j.ID)
	if e != nil {
		return nil, e
	}
	var out []voiceasset.Candidate
	for rows.Next() {
		var c voiceasset.Candidate
		if e = rows.Scan(&c.Asset, &c.State, &c.Attempts, &c.Next); e != nil {
			rows.Close()
			return nil, e
		}
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for i := range out {
		a, e := s.RawVoiceAsset(ctx, j.Scope.Tenant, out[i].Asset)
		if e != nil {
			return nil, e
		}
		out[i].Driver = a.StorageDriver
		out[i].Bucket = a.StorageBucket
		out[i].Key = a.ObjectKey
		out[i].Bytes = a.SizeBytes
		out[i].Stamp, _ = voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
	}
	return out, nil
}
func (s *Store) SetVoiceCleanupItem(ctx context.Context, j voiceasset.Job, c voiceasset.Candidate, state, reason string, bytes uint64) error {
	// Reasons are bounded codes, not raw SDK errors (which can contain URLs).
	if len(reason) > 200 {
		reason = "storage_error"
	}
	delay := voiceasset.RetryDelay(c.Attempts)
	_, e := s.db.ExecContext(ctx, `UPDATE voice_cleanup_items SET state=?,attempts=attempts+1,next_at=?,last_error=?,confirmed_bytes=? WHERE job_id=? AND asset_id=?`, state, time.Now().UTC().Add(delay), reason, bytes, j.ID, c.Asset)
	return e
}
func (s *Store) FinishVoiceCleanupJob(ctx context.Context, j voiceasset.Job) error {
	local, e := s.ClearedVoiceLocalSources(ctx, j)
	if e != nil {
		return e
	}
	if len(local) > 0 {
		return nil
	}
	_, e = s.db.ExecContext(ctx, `UPDATE voice_cleanup_jobs SET state=CASE WHEN EXISTS(SELECT 1 FROM voice_cleanup_items WHERE job_id=? AND state='review_required') THEN 'review_required' ELSE 'completed' END WHERE id=? AND scan_done=TRUE AND NOT EXISTS(SELECT 1 FROM voice_cleanup_items WHERE job_id=? AND state NOT IN('deleted','shared_retained','review_required'))`, j.ID, j.ID, j.ID)
	return e
}

// Register local paid staging before any provider call. The full path is
// private evidence; only its digest appears in APIs/logs.
func (s *Store) RegisterVoiceLocalSource(ctx context.Context, st voiceasset.Stamp, path string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, st.Scope); e != nil {
		return e
	}
	now, e := voiceStamp(ctx, tx, st.Scope, true)
	if e != nil {
		return e
	}
	if now != st {
		return voiceasset.ErrCleared
	}
	p, _ := json.Marshal(path)
	b, _ := json.Marshal(st)
	_, e = tx.ExecContext(ctx, `INSERT INTO voice_local_sources(tenant_id,plan_id,category,voice_key,path_hash,path_json,stamp_json) VALUES(?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE path_hash=path_hash`, st.Scope.Tenant, st.Scope.Plan, st.Scope.Category, st.Scope.Voice, voiceasset.Digest([]any{st.Scope, path}), p, b)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) VoiceAssetObjectReferences(ctx context.Context, a model.MediaAsset) ([]model.MediaAsset, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE storage_driver=? AND storage_bucket=? AND object_key=? AND status<>'deleted'`, a.StorageDriver, a.StorageBucket, a.ObjectKey)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []model.MediaAsset
	for rows.Next() {
		v, e := scanMediaAsset(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) LeaseVoiceAsset(ctx context.Context, tenant, asset, room, session int64, program, track string) error {
	if track == "" || room <= 0 || session <= 0 {
		return fmt.Errorf("声音播放引用无效")
	}
	a, e := s.RawVoiceAsset(ctx, tenant, asset)
	if e != nil {
		return e
	}
	st, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
	if !ok {
		return nil
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, st.Scope); e != nil {
		return e
	}
	now, e := voiceStamp(ctx, tx, st.Scope, true)
	if e != nil {
		return e
	}
	var state string
	e = tx.QueryRowContext(ctx, `SELECT state FROM voice_asset_leases WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=? FOR UPDATE`, tenant, asset, session, track).Scan(&state)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if e == nil && state != "released" {
		return tx.Commit()
	}
	if a.Status != "active" || st != now {
		return voiceasset.ErrCleared
	}
	// Retirement may change status while this caller waits for the scope lock.
	fresh, e := scanMediaAsset(tx.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, asset))
	if e != nil {
		return e
	}
	if fresh.Status != "active" {
		return voiceasset.ErrCleared
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO voice_asset_leases(tenant_id,asset_id,room_id,session_id,program_id,track_id) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE updated_at=updated_at`, tenant, asset, room, session, program, track)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) ConfirmVoiceAssetLease(ctx context.Context, tenant, asset, session int64, track, program string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE voice_asset_leases SET state='accepted',updated_at=UTC_TIMESTAMP(3),program_id=? WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=? AND state<>'released'`, program, tenant, asset, session, track)
	return e
}
func (s *Store) VoiceLeasedAsset(ctx context.Context, tenant, asset, session int64, track string) (model.MediaAsset, error) {
	var n int
	e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_asset_leases WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=? AND state<>'released'`, tenant, asset, session, track).Scan(&n)
	if e != nil {
		return model.MediaAsset{}, e
	}
	a, e := s.RawVoiceAsset(ctx, tenant, asset)
	if e != nil {
		return a, e
	}
	if n == 0 {
		if e = s.VoiceAssetAllowed(ctx, a); e != nil {
			return model.MediaAsset{}, e
		}
	}
	return a, nil
}

type VoiceAssetLease struct {
	Tenant, Asset, Room, Session int64
	Program, Track, State        string
}

func (s *Store) OutstandingVoiceAssetLeases(ctx context.Context) ([]VoiceAssetLease, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT tenant_id,asset_id,room_id,session_id,program_id,track_id,state FROM voice_asset_leases WHERE state<>'released' ORDER BY updated_at LIMIT 1000`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []VoiceAssetLease
	for rows.Next() {
		var l VoiceAssetLease
		if e = rows.Scan(&l.Tenant, &l.Asset, &l.Room, &l.Session, &l.Program, &l.Track, &l.State); e != nil {
			return nil, e
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
func (s *Store) ReleaseVoiceAssetLease(ctx context.Context, l VoiceAssetLease) error {
	_, e := s.db.ExecContext(ctx, `UPDATE voice_asset_leases SET state='released' WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=?`, l.Tenant, l.Asset, l.Session, l.Track)
	return e
}

// The scope lock serializes invalidation/new leases; the object-key range lock
// serializes shared references. Keep both through the bounded physical delete.
func (s *Store) DeleteVoiceCleanupObject(ctx context.Context, j voiceasset.Job, c voiceasset.Candidate, remove func(context.Context, model.MediaAsset) (uint64, error)) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, c.Stamp.Scope); e != nil {
		return e
	}
	initial, e := scanMediaAsset(tx.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE tenant_id=? AND id=?`, j.Scope.Tenant, c.Asset))
	if e != nil {
		return e
	}
	if e = lockVoiceObjectKeys(ctx, tx, initial); e != nil {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE storage_driver=? AND storage_bucket=? AND object_key=? FOR UPDATE`, c.Driver, c.Bucket, c.Key)
	if e != nil {
		return e
	}
	var assets []model.MediaAsset
	for rows.Next() {
		a, e := scanMediaAsset(rows)
		if e != nil {
			rows.Close()
			return e
		}
		assets = append(assets, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	var target model.MediaAsset
	shared := false
	for _, a := range assets {
		if a.ID == c.Asset {
			target = a
		}
		if a.Status == "deleted" {
			continue
		}
		st, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
		if !ok {
			shared = true
			continue
		}
		current, e := voiceStamp(ctx, tx, st.Scope, false)
		if e != nil {
			return e
		}
		if a.Status == "active" && st == current {
			shared = true
		}
	}
	if target.ID == 0 {
		return sql.ErrNoRows
	}
	st, ok := voiceasset.MetadataStamp(target.TenantID, target.AssetType, target.Metadata)
	if !ok || st != c.Stamp || target.TenantID != j.Scope.Tenant || st.Scope.Plan != j.Scope.Plan || st.Scope.Category != j.Scope.Category || (j.Scope.Voice != "" && st.Scope.Voice != j.Scope.Voice) {
		return voiceasset.ErrConflict
	}
	if j.Mode != "retired" && !(st.PlanEpoch < j.After.PlanEpoch || (st.PlanEpoch == j.After.PlanEpoch && j.Scope.Voice != "" && st.VoiceEpoch < j.After.VoiceEpoch)) {
		return voiceasset.ErrConflict
	}
	if j.Mode == "retired" {
		var n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_asset_retirements WHERE tenant_id=? AND plan_id=? AND asset_id=? AND eligible_at<=UTC_TIMESTAMP(3)`, j.Scope.Tenant, j.Scope.Plan, c.Asset).Scan(&n); e != nil {
			return e
		}
		if n == 0 {
			return voiceasset.ErrConflict
		}
	}
	if shared {
		// Only the cleared logical record is retired; the shared physical object
		// remains reachable through its other valid owner.
		if _, e = tx.ExecContext(ctx, `UPDATE media_assets SET status='deleted' WHERE id=? AND status='quarantined'`, target.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE voice_cleanup_items SET state='shared_retained',last_error='',confirmed_bytes=0 WHERE job_id=? AND asset_id=?`, j.ID, c.Asset); e != nil {
			return e
		}
		return tx.Commit()
	}
	var leases int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_asset_leases l JOIN media_assets a ON a.id=l.asset_id WHERE a.storage_driver=? AND a.storage_bucket=? AND a.object_key=? AND l.state<>'released'`, c.Driver, c.Bucket, c.Key).Scan(&leases); e != nil {
		return e
	}
	if leases > 0 {
		return voiceasset.ErrBusy
	}
	var currentState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM voice_cleanup_items WHERE job_id=? AND asset_id=? FOR UPDATE`, j.ID, c.Asset).Scan(&currentState); e != nil {
		return e
	}
	if currentState == "deleted" || currentState == "shared_retained" {
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE voice_cleanup_items SET state='deleting' WHERE job_id=? AND asset_id=?`, j.ID, c.Asset); e != nil {
		return e
	}
	metadata := map[string]any{}
	for key, value := range target.Metadata {
		metadata[key] = value
	}
	target.Metadata = metadata
	for _, field := range voiceasset.SidecarFields {
		key, _ := target.Metadata[field].(string)
		if key == "" {
			continue
		}
		var refs int
		q := `SELECT COUNT(*) FROM media_assets WHERE id<>? AND status<>'deleted' AND storage_driver=? AND storage_bucket=? AND (object_key=? OR JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.subtitle_object_key'))=? OR JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.timeline_object_key'))=? OR JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.safe_points_object_key'))=? OR JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.manifest_object_key'))=?)`
		if e = tx.QueryRowContext(ctx, q, target.ID, target.StorageDriver, target.StorageBucket, key, key, key, key, key).Scan(&refs); e != nil {
			return e
		}
		if refs > 0 {
			delete(target.Metadata, field)
		}
	}
	bytes, e := remove(ctx, target)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_assets SET status='deleted' WHERE id=?`, target.ID); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE voice_cleanup_items SET state='deleted',last_error='',confirmed_bytes=? WHERE job_id=? AND asset_id=?`, bytes, j.ID, c.Asset); e != nil {
		return e
	}
	return tx.Commit()
}

type VoiceLocalSource struct {
	Path, Hash string
	Stamp      voiceasset.Stamp
}

func (s *Store) ClearedVoiceLocalSources(ctx context.Context, j voiceasset.Job) ([]VoiceLocalSource, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT path_hash,path_json,stamp_json FROM voice_local_sources WHERE tenant_id=? AND plan_id=? AND category=? AND state='active' AND (?<>'local_only' OR created_at<=?) AND (?='' OR voice_key=?) AND ((?='local_only' AND CAST(JSON_EXTRACT(stamp_json,'$.plan_epoch') AS UNSIGNED)<=?) OR CAST(JSON_EXTRACT(stamp_json,'$.plan_epoch') AS UNSIGNED)<? OR (CAST(JSON_EXTRACT(stamp_json,'$.plan_epoch') AS UNSIGNED)=? AND ?<>'' AND CAST(JSON_EXTRACT(stamp_json,'$.voice_epoch') AS UNSIGNED)<?) OR (?='retired' AND asset_id IN(SELECT asset_id FROM voice_cleanup_items WHERE job_id=?))) ORDER BY path_hash LIMIT 500`, j.Scope.Tenant, j.Scope.Plan, j.Scope.Category, j.Mode, j.Created, j.Scope.Voice, j.Scope.Voice, j.Mode, j.Before.PlanEpoch, j.After.PlanEpoch, j.After.PlanEpoch, j.Scope.Voice, j.After.VoiceEpoch, j.Mode, j.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []VoiceLocalSource
	for rows.Next() {
		var x VoiceLocalSource
		var p, b []byte
		if e = rows.Scan(&x.Hash, &p, &b); e != nil {
			return nil, e
		}
		if json.Unmarshal(p, &x.Path) != nil || json.Unmarshal(b, &x.Stamp) != nil {
			return nil, errors.New("local_source_metadata_invalid")
		}
		if j.Scope.Voice != "" && x.Stamp.Scope.Voice != j.Scope.Voice {
			continue
		}
		old := x.Stamp.PlanEpoch < j.After.PlanEpoch || (x.Stamp.PlanEpoch == j.After.PlanEpoch && j.Scope.Voice != "" && x.Stamp.VoiceEpoch < j.After.VoiceEpoch)
		if old || j.Mode == "retired" || (j.Mode == "local_only" && x.Stamp.PlanEpoch <= j.Before.PlanEpoch) {
			out = append(out, x)
		}
	}
	return out, rows.Err()
}
func (s *Store) MarkVoiceLocalDeleted(ctx context.Context, tenant int64, hash string) error {
	_, e := s.db.ExecContext(ctx, `UPDATE voice_local_sources SET state='deleted' WHERE tenant_id=? AND path_hash=?`, tenant, hash)
	return e
}

// Prepared interaction audio is disposable execution state. A clear changes
// this revision before any new event can dispatch that older preparation.
func (s *Store) VoiceCacheRoomRevision(ctx context.Context, tenant, room int64) (string, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT e.voice_key,e.epoch FROM live_agent_room_plan_selections r JOIN voice_cache_epochs e ON e.tenant_id=r.tenant_id AND e.plan_id=r.plan_id WHERE r.tenant_id=? AND r.room_id=? AND e.category='interaction' AND e.epoch>0 ORDER BY e.voice_key`, tenant, room)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	var revisions []any
	for rows.Next() {
		var key string
		var epoch uint64
		if e = rows.Scan(&key, &epoch); e != nil {
			return "", e
		}
		revisions = append(revisions, []any{key, epoch})
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	if len(revisions) == 0 {
		return "", nil
	}
	return voiceasset.Digest(revisions), nil
}
