package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/humanfission"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

type HumanFissionJob struct {
	Tenant, Plan, Source          int64
	Action, Status, Detail, Token string
	State                         humanfission.State
}

var ErrFissionUpdated = errors.New("录音或文稿已更新，请重新裂变")
var ErrFissionBusy = errors.New("本条录音正在处理，请完成后再修改语音片")
var ErrFissionClearBusy = errors.New("本条录音正在处理，完成后可清空语音片")
var ErrFissionClearChanged = errors.New("裂变结果已更新，请重新打开确认框")

func (s *Store) MigrateHumanFission(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS plan_human_fission (
	    source_id BIGINT NOT NULL PRIMARY KEY, tenant_id BIGINT NOT NULL, plan_id BIGINT NOT NULL,
	    action VARCHAR(16) NOT NULL, status VARCHAR(16) NOT NULL, detail VARCHAR(255) NOT NULL DEFAULT '',
	    token VARCHAR(64) NOT NULL DEFAULT '', available_at DATETIME(3) NOT NULL DEFAULT (UTC_TIMESTAMP(3)),
	    lease_until DATETIME(3) NULL, state_json JSON NOT NULL,
	    KEY idx_fission_queue(status,available_at,lease_until), KEY idx_fission_plan(tenant_id,plan_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	return err
}

const humanFissionColumns = "tenant_id,plan_id,source_id,action,status,detail,token,state_json"

func scanFission(row interface{ Scan(...any) error }) (HumanFissionJob, error) {
	var job HumanFissionJob
	var raw []byte
	if err := row.Scan(&job.Tenant, &job.Plan, &job.Source, &job.Action, &job.Status, &job.Detail, &job.Token, &raw); err != nil {
		return job, err
	}
	return job, json.Unmarshal(raw, &job.State)
}

func (s *Store) HumanFission(ctx context.Context, tenant, plan, source int64) (HumanFissionJob, error) {
	return scanFission(s.db.QueryRowContext(ctx, "SELECT "+humanFissionColumns+" FROM plan_human_fission WHERE tenant_id=? AND plan_id=? AND source_id=?", tenant, plan, source))
}

type HumanFissionPartsStatus struct {
	Input   string
	Count   int
	Pending bool
	Busy    bool
}

func (s *Store) ListHumanFissionPartsStatus(ctx context.Context, tenant, plan int64) (map[int64]HumanFissionPartsStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_id, JSON_UNQUOTE(JSON_EXTRACT(state_json,'$.input')),
		CASE WHEN JSON_SEARCH(state_json,'one','',NULL,'$.parts[*].srt') IS NULL
			THEN COALESCE(JSON_LENGTH(state_json,'$.parts'),0) ELSE 0 END,
		status IN ('queued','running') AND action='plan', status IN ('queued','running')
		FROM plan_human_fission WHERE tenant_id=? AND plan_id=?`, tenant, plan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]HumanFissionPartsStatus)
	for rows.Next() {
		var id int64
		var item HumanFissionPartsStatus
		if err := rows.Scan(&id, &item.Input, &item.Count, &item.Pending, &item.Busy); err != nil {
			return nil, err
		}
		out[id] = item
	}
	return out, rows.Err()
}

func fissionOriginal(ctx context.Context, tx *sql.Tx, tenant, plan, source int64) (model.MediaAsset, error) {
	var planID int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE", plan, tenant).Scan(&planID); err != nil {
		return model.MediaAsset{}, err
	}
	return scanMediaAsset(tx.QueryRowContext(ctx, "SELECT "+voiceAssetColumns+" FROM media_assets WHERE tenant_id=? AND id=? AND asset_type=? AND status='active' AND object_key LIKE ? FOR UPDATE",
		tenant, source, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%"))
}

func HumanFissionInput(asset model.MediaAsset) string {
	text, _ := asset.Metadata["script_transcript"].(string)
	return humanfission.Digest([]any{asset.ObjectKey, text})
}

func (s *Store) QueueHumanFission(ctx context.Context, tenant, plan, source int64, action string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	asset, err := fissionOriginal(ctx, tx, tenant, plan, source)
	if err != nil {
		return err
	}
	transcript, _ := asset.Metadata["script_transcript"].(string)
	if _, err := humanfission.ReadSRT(transcript); err != nil {
		return err
	}
	input := HumanFissionInput(asset)
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+" FROM plan_human_fission WHERE source_id=? FOR UPDATE", source))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err != nil || input != job.State.Input {
		if action == "render" {
			return ErrFissionUpdated
		}
		job.State = humanfission.State{Input: input, Generation: rand.Text()}
	} else if job.Status == "queued" || job.Status == "running" || action == "plan" && job.Status == "ready" && (len(job.State.Recipes) > 0 || job.State.Exhausted) {
		return tx.Commit()
	} else if action == "render" && len(job.State.Recipes) == 0 {
		return errors.New("请先完成裂变规划")
	}
	job.State.Split, job.State.Merge, job.State.Exact = nil, nil, nil
	raw, err := json.Marshal(job.State)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO plan_human_fission(source_id,tenant_id,plan_id,action,status,state_json)
	    VALUES(?,?,?,?,'queued',?) ON DUPLICATE KEY UPDATE action=VALUES(action),status='queued',state_json=VALUES(state_json),
	    token='',detail='',available_at=UTC_TIMESTAMP(3),lease_until=NULL`, source, tenant, plan, action, raw)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (job HumanFissionJob) Busy() bool {
	return job.Status == "queued" || job.Status == "running"
}

func (job HumanFissionJob) ClearRevision() string {
	return humanfission.Digest([]any{job.Tenant, job.Plan, job.Source, job.Action, job.Status, job.Token, job.State})
}

func (job HumanFissionJob) ObjectKey(name string) string {
	prefix := ScriptHumanVoicePrefix(job.Tenant, job.Plan, HumanFissionScope(job.Source)) + job.State.Input
	if job.State.Generation != "" {
		prefix += "-" + job.State.Generation
	}
	return prefix + "-" + name + ".wav"
}

func (s *Store) ClearHumanFission(ctx context.Context, tenant, plan, source, actor int64, revision string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = fissionOriginal(ctx, tx, tenant, plan, source); err != nil {
		return err
	}
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+
		" FROM plan_human_fission WHERE source_id=? AND tenant_id=? AND plan_id=? FOR UPDATE", source, tenant, plan))
	if errors.Is(err, sql.ErrNoRows) {
		return tx.Commit() // An already cleared recording is an idempotent retry.
	}
	if err != nil {
		return err
	}
	if job.Busy() {
		return ErrFissionClearBusy
	}
	if revision != job.ClearRevision() {
		return ErrFissionClearChanged
	}
	scope := voiceasset.Scope{Tenant: tenant, Plan: plan, Category: voiceasset.ScriptHumanCategory, Voice: PlanHumanScriptScope}
	if err = retireHumanFission(ctx, tx, scope, source, actor); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) QueueHumanFissionSplit(ctx context.Context, tenant, plan, source int64, selection humanfission.SplitRequest) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	asset, err := fissionOriginal(ctx, tx, tenant, plan, source)
	if err != nil {
		return err
	}
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+" FROM plan_human_fission WHERE source_id=? AND tenant_id=? AND plan_id=? FOR UPDATE", source, tenant, plan))
	if err != nil {
		return err
	}
	if job.State.Input != HumanFissionInput(asset) || job.State.ReadyParts() == 0 ||
		selection.Number < 1 || selection.Number > len(job.State.Parts) {
		return ErrFissionUpdated
	}
	part := job.State.Parts[selection.Number-1]
	if part.Version() != selection.Version {
		return ErrFissionUpdated
	}
	if job.Status == "queued" || job.Status == "running" {
		if job.Action == "split" && job.State.Split != nil && *job.State.Split == selection {
			return tx.Commit()
		}
		return ErrFissionBusy
	}
	if _, err := humanfission.Selection(part.Text, selection.Start, selection.End); err != nil {
		return err
	}
	job.State.Split, job.State.Merge = &selection, nil
	return queueHumanFissionEdit(ctx, tx, job, "split")
}

func (s *Store) QueueHumanFissionMerge(ctx context.Context, tenant, plan, source int64, selection humanfission.MergeRequest) error {
	if !selection.Adjacent() {
		return humanfission.ErrMergeSelection
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	asset, err := fissionOriginal(ctx, tx, tenant, plan, source)
	if err != nil {
		return err
	}
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+
		" FROM plan_human_fission WHERE source_id=? AND tenant_id=? AND plan_id=? FOR UPDATE", source, tenant, plan))
	if err != nil {
		return err
	}
	if job.State.Input != HumanFissionInput(asset) || job.State.ReadyParts() == 0 || !selection.Matches(job.State.Parts) {
		return ErrFissionUpdated
	}
	if job.Busy() {
		if job.Action == "merge" && job.State.Merge != nil && *job.State.Merge == selection {
			return tx.Commit()
		}
		return ErrFissionBusy
	}
	job.State.Split, job.State.Merge = nil, &selection
	return queueHumanFissionEdit(ctx, tx, job, "merge")
}

func queueHumanFissionEdit(ctx context.Context, tx *sql.Tx, job HumanFissionJob, action string) error {
	raw, err := json.Marshal(job.State)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE plan_human_fission SET state_json=?, action=?, status='queued',
			detail='', token='', available_at=UTC_TIMESTAMP(3), lease_until=NULL WHERE source_id=?`, raw, action, job.Source); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClaimHumanFission(ctx context.Context, token string, until time.Time) (HumanFissionJob, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HumanFissionJob{}, err
	}
	defer tx.Rollback()
	job, err := scanFission(tx.QueryRowContext(ctx, "SELECT "+humanFissionColumns+` FROM plan_human_fission
	    WHERE (status='queued' AND available_at<=UTC_TIMESTAMP(3)) OR (status='running' AND lease_until<UTC_TIMESTAMP(3))
	    ORDER BY available_at,source_id LIMIT 1 FOR UPDATE SKIP LOCKED`))
	if err != nil {
		return job, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE plan_human_fission SET status='running',token=?,lease_until=?,detail='' WHERE source_id=?", token, until, job.Source)
	if err != nil {
		return job, err
	}
	job.Status, job.Token = "running", token
	return job, tx.Commit()
}

func (s *Store) UpdateHumanFissionProgress(ctx context.Context, job HumanFissionJob, detail string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE plan_human_fission SET detail=?
		WHERE source_id=? AND tenant_id=? AND plan_id=? AND token=? AND status='running'`,
		detail, job.Source, job.Tenant, job.Plan, job.Token)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected != 1 {
		return ErrFissionUpdated
	}
	return err
}

func (s *Store) SaveHumanFission(ctx context.Context, job HumanFissionJob, status, detail string, delay time.Duration) error {
	raw, err := json.Marshal(job.State)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE plan_human_fission SET state_json=?,status=?,detail=?,available_at=?
	    WHERE source_id=? AND tenant_id=? AND plan_id=? AND token=? AND status='running'`,
		raw, status, detail, time.Now().UTC().Add(delay), job.Source, job.Tenant, job.Plan, job.Token)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err == nil && affected != 1 && status != "running" {
		return ErrFissionUpdated
	}
	return err
}

func HumanFissionScope(source int64) string {
	return voiceasset.Digest([]any{"plan-human-fission", source})
}

func (s *Store) SaveHumanFissionMedia(ctx context.Context, job HumanFissionJob, asset model.CreateMediaAssetInput, put func() error) error {
	scope := voiceasset.Scope{Tenant: job.Tenant, Plan: job.Plan, Category: voiceasset.ScriptHumanCategory, Voice: HumanFissionScope(job.Source)}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	source, err := fissionOriginal(ctx, tx, job.Tenant, job.Plan, job.Source)
	if err != nil {
		return err
	}
	if HumanFissionInput(source) != job.State.Input {
		return ErrFissionUpdated
	}
	var current string
	if err = tx.QueryRowContext(ctx, "SELECT token FROM plan_human_fission WHERE source_id=? AND status='running' FOR UPDATE", job.Source).Scan(&current); err != nil {
		return err
	}
	if current != job.Token {
		return ErrFissionUpdated
	}
	if err = lockVoiceScope(ctx, tx, scope); err != nil {
		return err
	}
	if asset.StorageDriver != "oss" || !strings.HasPrefix(asset.ObjectKey, ScriptHumanVoicePrefix(job.Tenant, job.Plan, scope.Voice)) {
		return voiceasset.ErrConflict
	}
	meta := map[string]any{"plan_id": job.Plan, "purpose": voiceasset.ScriptHumanAsset, "fission_source_id": job.Source}
	voiceasset.Stamp{Scope: scope}.Metadata(meta)
	stamp, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	// The parent and source locks serialize registration with plan and source
	// retirement. The object is never published in the manifest before Put.
	_, err = tx.ExecContext(ctx, `INSERT INTO media_assets(tenant_id,asset_type,original_name,storage_driver,storage_bucket,
	    object_key,mime_type,size_bytes,duration_ms,checksum_sha256,status,metadata_json)
	    SELECT ?,?,?,?,?,?,'audio/wav',?,?,?,'active',CAST(? AS JSON)
	    WHERE NOT EXISTS (SELECT 1 FROM media_assets WHERE tenant_id=? AND object_key=?)`,
		job.Tenant, voiceasset.ScriptHumanAsset, asset.OriginalName, asset.StorageDriver, asset.StorageBucket, asset.ObjectKey,
		asset.SizeBytes, asset.DurationMS, asset.ChecksumSHA256, string(stamp), job.Tenant, asset.ObjectKey)
	if err != nil {
		return err
	}
	if err = put(); err != nil {
		return err
	}
	return tx.Commit()
}

func retireHumanFission(ctx context.Context, tx *sql.Tx, scope voiceasset.Scope, source, actor int64) error {
	derived := scope
	derived.Voice = HumanFissionScope(source)
	if err := lockVoiceScope(ctx, tx, derived); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM media_assets WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ? FOR UPDATE",
		scope.Tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(scope.Tenant, scope.Plan, derived.Voice)+"%")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = retireScriptHumanVoice(ctx, tx, derived, id, actor); err != nil {
			return fmt.Errorf("清理裂变资产: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM plan_human_fission WHERE source_id=? AND tenant_id=? AND plan_id=?", source, scope.Tenant, scope.Plan)
	return err
}
