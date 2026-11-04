package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

func (s *Store) ensureVoiceInventorySchema(ctx context.Context) error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS voice_object_locks(object_hash CHAR(64) PRIMARY KEY) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_upload_journal(object_hash CHAR(64) PRIMARY KEY,tenant_id BIGINT NOT NULL,plan_id BIGINT NOT NULL,storage_driver VARCHAR(24) NOT NULL,storage_bucket VARCHAR(255) NOT NULL,object_key VARCHAR(1024) NOT NULL,created_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),last_absent_scan DATETIME(3) NULL,absent_scans INT NOT NULL DEFAULT 0,KEY idx_voice_upload_scope(tenant_id,plan_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
		`CREATE TABLE IF NOT EXISTS voice_asset_usage(tenant_id BIGINT NOT NULL,asset_id BIGINT NOT NULL,last_pcm_at DATETIME(3) NOT NULL,PRIMARY KEY(tenant_id,asset_id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`,
	} {
		if _, e := s.db.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return nil
}
func lockVoiceObjectKeys(ctx context.Context, tx *sql.Tx, a model.MediaAsset) error {
	keys := []string{a.ObjectKey}
	for _, field := range voiceasset.SidecarFields {
		if key, _ := a.Metadata[field].(string); key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		hash := voiceasset.Digest([]string{a.StorageDriver, a.StorageBucket, key})
		if _, e := tx.ExecContext(ctx, `INSERT INTO voice_object_locks(object_hash) VALUES(?) ON DUPLICATE KEY UPDATE object_hash=object_hash`, hash); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) RegisterVoiceUpload(ctx context.Context, tenant, plan int64, driver, bucket, key string) error {
	if tenant <= 0 || plan <= 0 || !strings.HasPrefix(key, "tenants/") {
		return voiceasset.ErrConflict
	}
	hash := voiceasset.Digest([]string{driver, bucket, key})
	_, e := s.db.ExecContext(ctx, `INSERT IGNORE INTO voice_upload_journal(object_hash,tenant_id,plan_id,storage_driver,storage_bucket,object_key) VALUES(?,?,?,?,?,?)`, hash, tenant, plan, driver, bucket, key)
	return e
}
func (s *Store) RecordVoiceAssetPCM(ctx context.Context, tenant, asset int64, at string) error {
	_, e := s.db.ExecContext(ctx, `INSERT INTO voice_asset_usage(tenant_id,asset_id,last_pcm_at) VALUES(?,?,?) ON DUPLICATE KEY UPDATE last_pcm_at=GREATEST(last_pcm_at,VALUES(last_pcm_at))`, tenant, asset, at)
	return e
}
func (s *Store) VoiceAssetLastPCM(ctx context.Context, tenant, asset int64) (string, error) {
	var at string
	e := s.db.QueryRowContext(ctx, `SELECT CAST(last_pcm_at AS CHAR) FROM voice_asset_usage WHERE tenant_id=? AND asset_id=?`, tenant, asset).Scan(&at)
	if e == sql.ErrNoRows {
		return "", nil
	}
	return at, e
}

// Journal-backed uploads have proven scope. Legacy bucket objects lacking this
// evidence are reported separately by inventory, never guessed into a plan.
func (s *Store) ScanVoiceUploadOrphans(ctx context.Context, tenant, plan int64) (map[string]int64, error) {
	q := `NOT EXISTS(SELECT 1 FROM media_assets a WHERE a.storage_driver=u.storage_driver AND a.storage_bucket=u.storage_bucket AND (a.object_key=u.object_key OR JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.subtitle_object_key'))=u.object_key OR JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.timeline_object_key'))=u.object_key OR JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.safe_points_object_key'))=u.object_key OR JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.manifest_object_key'))=u.object_key))`
	_, e := s.db.ExecContext(ctx, `UPDATE voice_upload_journal u SET absent_scans=absent_scans+1,last_absent_scan=UTC_TIMESTAMP(3) WHERE tenant_id=? AND plan_id=? AND created_at<DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 24 HOUR) AND (last_absent_scan IS NULL OR last_absent_scan<DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 15 MINUTE)) AND `+q, tenant, plan)
	if e != nil {
		return nil, e
	}
	var n int64
	e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM voice_upload_journal u WHERE tenant_id=? AND plan_id=? AND absent_scans>=2 AND `+q, tenant, plan).Scan(&n)
	return map[string]int64{"orphan_upload_candidates": n}, e
}
func (s *Store) VoiceCleanupVoices(ctx context.Context, scope voiceasset.Scope) ([]map[string]string, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT DISTINCT CAST(metadata_json AS CHAR) FROM media_assets WHERE tenant_id=? AND asset_type='generated_voice' AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.plan_id'))=CAST(? AS CHAR)`, scope.Tenant, scope.Plan)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]string{}
	seen := map[string]bool{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var m map[string]any
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		st, ok := voiceasset.MetadataStamp(scope.Tenant, "generated_voice", m)
		if !ok || st.Scope.Category != scope.Category || st.Scope.Voice == "" || seen[st.Scope.Voice] {
			continue
		}
		seen[st.Scope.Voice] = true
		label, _ := m["voice_id"].(string)
		model, _ := m["tts_model"].(string)
		out = append(out, map[string]string{"key": st.Scope.Voice, "label": label + " · " + model + " · " + st.Scope.Voice[:12]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["key"] < out[j]["key"] })
	return out, rows.Err()
}
