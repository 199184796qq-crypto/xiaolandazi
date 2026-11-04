package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"livecompanion/management/internal/voiceasset"
)

func (s *Store) ensureVoiceAssetRetirementSchema(ctx context.Context) error {
	_, e := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS voice_asset_retirements(tenant_id BIGINT NOT NULL,plan_id BIGINT NOT NULL,paragraph_key CHAR(64) NOT NULL,asset_id BIGINT NOT NULL,retired_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),eligible_at DATETIME(3) NOT NULL,PRIMARY KEY(tenant_id,plan_id,paragraph_key),KEY idx_retired_asset(asset_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	if e != nil {
		return e
	}
	return s.ensureVoiceInventorySchema(ctx)
}

func (s *Store) CheckVoiceParagraph(ctx context.Context, tenant, plan int64, key string) error {
	var n int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_asset_retirements WHERE tenant_id=? AND plan_id=? AND paragraph_key=?`, tenant, plan, key).Scan(&n); e != nil {
		return e
	}
	if n > 0 {
		return voiceasset.ErrCleared
	}
	return nil
}

// Register retirement before removing a pool member. A file-write failure may
// leave a conservative inactive member, but can never resurrect paid old audio.
func (s *Store) RetireVoiceAsset(ctx context.Context, tenant, plan, asset int64, key string) error {
	if !voiceasset.DigestValid(key) {
		return voiceasset.ErrConflict
	}
	a, e := s.RawVoiceAsset(ctx, tenant, asset)
	if e != nil {
		return e
	}
	st, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
	if !ok || st.Scope.Plan != plan || st.Scope.Category != "mainline" {
		return voiceasset.ErrConflict
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, st.Scope); e != nil {
		return e
	}
	job := voiceasset.Digest([]any{"retire", tenant, plan, asset, key})
	b, _ := json.Marshal(st)
	if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO voice_asset_retirements(tenant_id,plan_id,paragraph_key,asset_id,eligible_at) VALUES(?,?,?,?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 7 DAY))`, tenant, plan, key, asset); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_assets SET status='quarantined' WHERE tenant_id=? AND id=? AND status='active'`, tenant, asset); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO voice_cleanup_jobs(id,tenant_id,plan_id,category,voice_key,before_json,after_json,actor_user_id,mode,scan_done,state,next_at) VALUES(?,?,?,?,?,?,?,0,'retired',TRUE,'dry_run',DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 7 DAY))`, job, tenant, plan, st.Scope.Category, st.Scope.Voice, b, b); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT IGNORE INTO voice_cleanup_items(job_id,asset_id,next_at) VALUES(?,?,DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 7 DAY))`, job, asset); e != nil {
		return e
	}
	return tx.Commit()
}

// Job-level scheduling prevents a corrupt local directory or a review-only job
// from occupying every worker slot forever. Per-object retries remain separate.
func (s *Store) DeferVoiceCleanupJob(ctx context.Context, j voiceasset.Job, failed bool) error {
	delay := time.Minute
	reason := ""
	if failed {
		delay = 5 * time.Minute
		reason = "state_or_local_check_failed"
	}
	_, e := s.db.ExecContext(ctx, `UPDATE voice_cleanup_jobs SET next_at=?,last_error=? WHERE id=?`, time.Now().UTC().Add(delay), reason, j.ID)
	return e
}
func (s *Store) TouchVoiceAssetLease(ctx context.Context, l VoiceAssetLease) error {
	_, e := s.db.ExecContext(ctx, `UPDATE voice_asset_leases SET updated_at=UTC_TIMESTAMP(3) WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=?`, l.Tenant, l.Asset, l.Session, l.Track)
	return e
}

// Proven legacy owners may be registered after invalidation. This operation
// cannot activate an asset or change its original epoch.
func (s *Store) RegisterLegacyVoiceLocalSource(ctx context.Context, st voiceasset.Stamp, path string, asset int64, observed ...time.Time) error {
	if st.Scope.Validate() != nil || asset <= 0 {
		return voiceasset.ErrConflict
	}
	a, e := s.RawVoiceAsset(ctx, st.Scope.Tenant, asset)
	if e != nil {
		return e
	}
	actual, ok := voiceasset.MetadataStamp(a.TenantID, a.AssetType, a.Metadata)
	if !ok || actual != st {
		return voiceasset.ErrConflict
	}
	p, _ := json.Marshal(path)
	b, _ := json.Marshal(st)
	at := time.Now().UTC()
	if len(observed) > 0 {
		at = observed[0].UTC()
	}
	_, e = s.db.ExecContext(ctx, `INSERT INTO voice_local_sources(tenant_id,plan_id,category,voice_key,path_hash,path_json,stamp_json,asset_id,created_at) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE asset_id=VALUES(asset_id)`, st.Scope.Tenant, st.Scope.Plan, st.Scope.Category, st.Scope.Voice, voiceasset.Digest([]any{st.Scope, path}), p, b, asset, at)
	return e
}

// Keep every logical owner locked through removal. A second valid scope using
// the same directory retains the physical local copy, just like a shared OSS key.
func (s *Store) DeleteVoiceLocalSource(ctx context.Context, j voiceasset.Job, x VoiceLocalSource, remove func() error) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, x.Stamp.Scope); e != nil {
		return e
	}
	p, _ := json.Marshal(x.Path)
	rows, e := tx.QueryContext(ctx, `SELECT path_hash,stamp_json,asset_id FROM voice_local_sources WHERE path_json=? AND state='active' FOR UPDATE`, p)
	if e != nil {
		return e
	}
	type owner struct {
		hash  string
		st    voiceasset.Stamp
		asset int64
	}
	var owners []owner
	for rows.Next() {
		var v owner
		var b []byte
		if e = rows.Scan(&v.hash, &b, &v.asset); e != nil {
			rows.Close()
			return e
		}
		if json.Unmarshal(b, &v.st) != nil {
			rows.Close()
			return voiceasset.ErrConflict
		}
		owners = append(owners, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	shared := false
	for _, v := range owners {
		if v.asset > 0 {
			var n int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_asset_leases WHERE asset_id=? AND state<>'released'`, v.asset).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				return voiceasset.ErrBusy
			}
		}
		if v.hash == x.Hash {
			continue
		}
		current, e := voiceStamp(ctx, tx, v.st.Scope, false)
		if e != nil {
			return e
		}
		if current == v.st {
			shared = true
		}
	}
	if !shared {
		if e = remove(); e != nil {
			return e
		}
	}
	state := "deleted"
	if shared {
		state = "shared_retained"
	}
	_, e = tx.ExecContext(ctx, `UPDATE voice_local_sources SET state=? WHERE tenant_id=? AND path_hash=?`, state, j.Scope.Tenant, x.Hash)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func (s *Store) VoiceRetirementDue(ctx context.Context, tenant, asset int64) (bool, error) {
	var at time.Time
	e := s.db.QueryRowContext(ctx, `SELECT MIN(eligible_at) FROM voice_asset_retirements WHERE tenant_id=? AND asset_id=?`, tenant, asset).Scan(&at)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return !at.After(time.Now().UTC()), e
}

// The invalidation lock spans the actual Core handoff. A merely prepared lease
// cannot become permission to enqueue old audio after a clear. An already
// accepted exact instance may retry without buying or selecting a new instance.
func (s *Store) DispatchVoiceAsset(ctx context.Context, tenant, asset, session int64, track string, send func() (string, error)) error {
	a, e := s.RawVoiceAsset(ctx, tenant, asset)
	if e != nil {
		return e
	}
	st, known := voiceasset.MetadataStamp(tenant, a.AssetType, a.Metadata)
	if !known {
		_, e = send()
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockVoiceScope(ctx, tx, st.Scope); e != nil {
		return e
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM voice_asset_leases WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=? FOR UPDATE`, tenant, asset, session, track).Scan(&state); e != nil {
		return e
	}
	if state == "released" {
		return voiceasset.ErrCleared
	}
	if state != "accepted" {
		current, e := voiceStamp(ctx, tx, st.Scope, true)
		if e != nil {
			return e
		}
		fresh, e := scanMediaAsset(tx.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets WHERE tenant_id=? AND id=?`, tenant, asset))
		if e != nil {
			return e
		}
		if current != st || fresh.Status != "active" {
			return voiceasset.ErrCleared
		}
	}
	program, e := send()
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE voice_asset_leases SET state='accepted',program_id=?,updated_at=UTC_TIMESTAMP(3) WHERE tenant_id=? AND asset_id=? AND session_id=? AND track_id=?`, program, tenant, asset, session, track); e != nil {
		return e
	}
	return tx.Commit()
}
