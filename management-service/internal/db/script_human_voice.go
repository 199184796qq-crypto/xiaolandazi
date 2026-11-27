package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
)

const MaxScriptHumanVoices = 20

var ErrScriptHumanVoiceLimit = errors.New("每个话术片最多添加 20 条真人录音")

type ScriptHumanVoiceSummary struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Duration         uint64 `json:"duration_ms"`
	TranscriptStatus string `json:"transcript_status,omitempty"`
	FissionParts     int    `json:"fission_parts,omitempty"`
	FissionPending   bool   `json:"fission_pending,omitempty"`
	FissionClearable bool   `json:"fission_clearable,omitempty"`
	FissionBusy      bool   `json:"fission_busy,omitempty"`
}

func scriptHumanPlanPrefix(tenant, plan int64) string {
	return fmt.Sprintf("tenants/%d/%s/%d/", tenant, voiceasset.ScriptHumanAsset, plan)
}

func ScriptHumanVoicePrefix(tenant, plan int64, piece string) string {
	return scriptHumanPlanPrefix(tenant, plan) + piece + "/"
}

func (s *Store) ListPlanScriptHumanVoices(ctx context.Context, tenant, plan int64) (map[string][]ScriptHumanVoiceSummary, error) {
	prefix := scriptHumanPlanPrefix(tenant, plan)
	rows, err := s.db.QueryContext(ctx, `SELECT object_key,id,original_name,duration_ms FROM media_assets
		WHERE tenant_id=? AND asset_type=? AND status='active' AND duration_ms>0 AND object_key LIKE ?
		AND JSON_EXTRACT(metadata_json,'$.fission_source_id') IS NULL ORDER BY id`,
		tenant, voiceasset.ScriptHumanAsset, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[string][]ScriptHumanVoiceSummary)
	for rows.Next() {
		var key string
		var item ScriptHumanVoiceSummary
		if err = rows.Scan(&key, &item.ID, &item.Name, &item.Duration); err != nil {
			return nil, err
		}
		piece, _, linked := strings.Cut(strings.TrimPrefix(key, prefix), "/")
		if strings.HasPrefix(key, prefix) && linked && piece != PlanHumanScriptScope && voiceasset.DigestValid(piece) {
			items[piece] = append(items[piece], item)
		}
	}
	return items, rows.Err()
}

func (s *Store) ListPlanHumanScriptSources(ctx context.Context, tenant, plan int64) ([]ScriptHumanVoiceSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,original_name,duration_ms FROM media_assets
		WHERE tenant_id=? AND asset_type=? AND status='active' AND duration_ms>0 AND object_key LIKE ?
		ORDER BY COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_order')) AS UNSIGNED),18446744073709551615), id`,
		tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []ScriptHumanVoiceSummary
	for rows.Next() {
		var source ScriptHumanVoiceSummary
		if err := rows.Scan(&source.ID, &source.Name, &source.Duration); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, rows.Err()
}

func (s *Store) ListScriptHumanVoices(ctx context.Context, tenant, plan int64, piece string) ([]model.MediaAsset, error) {
	order := "id"
	if piece == PlanHumanScriptScope {
		order = "COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_order')) AS UNSIGNED), 18446744073709551615), id"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets
		WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ? ORDER BY `+order,
		tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, piece)+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.MediaAsset{}
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ScriptHumanVoice(ctx context.Context, tenant, plan int64, piece string, asset int64) (model.MediaAsset, error) {
	return scanMediaAsset(s.db.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets
		WHERE tenant_id=? AND id=? AND asset_type=? AND status='active' AND object_key LIKE ?`,
		tenant, asset, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, piece)+"%"))
}

func (s *Store) ScriptHumanVoiceByObject(ctx context.Context, tenant, plan int64, piece, object string) (model.MediaAsset, error) {
	if !strings.HasPrefix(object, ScriptHumanVoicePrefix(tenant, plan, piece)) {
		return model.MediaAsset{}, sql.ErrNoRows
	}
	return scanMediaAsset(s.db.QueryRowContext(ctx, `SELECT `+voiceAssetColumns+` FROM media_assets
		WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key=?`, tenant, voiceasset.ScriptHumanAsset, object))
}

func (s *Store) RenameScriptHumanVoice(ctx context.Context, tenant, plan int64, piece string, asset int64, name string) error {
	if !voiceasset.DigestValid(piece) {
		return voiceasset.ErrConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE media_assets SET original_name=?
		WHERE tenant_id=? AND id=? AND asset_type=? AND status='active' AND object_key LIKE ?`,
		name, tenant, asset, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, piece)+"%")
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed > 0 {
		return err
	}
	// An identical name is a successful retry with MySQL's default row count.
	_, err = s.ScriptHumanVoice(ctx, tenant, plan, piece, asset)
	return err
}

// ChangeScriptHumanVoice adds an immutable object or atomically replaces/retires
// a prior object. Cleanup respects accepted Core playback leases.
func (s *Store) ChangeScriptHumanVoice(ctx context.Context, tenant, plan int64, piece string, oldID, actor int64, input *model.CreateMediaAssetInput) error {
	scope := voiceasset.Scope{Tenant: tenant, Plan: plan, Category: voiceasset.ScriptHumanCategory, Voice: piece}
	if scope.Validate() != nil || !voiceasset.DigestValid(piece) {
		return voiceasset.ErrConflict
	}
	prefix := ScriptHumanVoicePrefix(tenant, plan, piece)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var activePlan int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, plan, tenant).Scan(&activePlan)
	if err != nil {
		return err
	}
	if err = lockVoiceScope(ctx, tx, scope); err != nil {
		return err
	}
	stamp := voiceasset.Stamp{Scope: scope}
	if oldID > 0 {
		var linkedID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM media_assets
			WHERE tenant_id=? AND id=? AND asset_type=? AND status='active' AND object_key LIKE ? FOR UPDATE`,
			tenant, oldID, voiceasset.ScriptHumanAsset, prefix+"%").Scan(&linkedID)
		if err != nil {
			return err
		}
		if err := retireScriptHumanVoice(ctx, tx, scope, linkedID, actor); err != nil {
			return err
		}
	} else if input != nil {
		var active int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_assets WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ?`,
			tenant, voiceasset.ScriptHumanAsset, prefix+"%").Scan(&active); err != nil {
			return err
		}
		if active >= MaxScriptHumanVoices {
			return ErrScriptHumanVoiceLimit
		}
	}
	if input != nil {
		if input.StorageDriver != "oss" || !strings.HasPrefix(input.ObjectKey, prefix) {
			return voiceasset.ErrConflict
		}
		meta := map[string]any{"plan_id": plan, "purpose": voiceasset.ScriptHumanAsset}
		stamp.Metadata(meta)
		if piece == PlanHumanScriptScope {
			meta["script_asr_status"] = "queued"
		}
		data, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO media_assets(tenant_id,asset_type,original_name,storage_driver,storage_bucket,object_key,
			mime_type,size_bytes,duration_ms,checksum_sha256,status,metadata_json,created_by_user_id)
			VALUES(?,?,?,?,?,?,'audio/wav',?,?,?,'active',CAST(? AS JSON),?)`, tenant, voiceasset.ScriptHumanAsset,
			input.OriginalName, input.StorageDriver, input.StorageBucket, input.ObjectKey, input.SizeBytes, input.DurationMS,
			input.ChecksumSHA256, data, actor)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ScriptHumanVoiceObjectRegistered(ctx context.Context, tenant int64, objectKey string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM media_assets WHERE tenant_id=? AND asset_type=? AND object_key=?)`,
		tenant, voiceasset.ScriptHumanAsset, objectKey).Scan(&exists)
	return exists, err
}

func retireScriptHumanVoice(ctx context.Context, tx *sql.Tx, scope voiceasset.Scope, id, actor int64) error {
	if scope.Voice == PlanHumanScriptScope {
		if err := retireHumanFission(ctx, tx, scope, id, actor); err != nil {
			return err
		}
	}
	stamp := voiceasset.Stamp{Scope: scope}
	job := voiceasset.Digest([]any{"human-recording-retirement", scope.Tenant, scope.Plan, id})
	data, err := json.Marshal(stamp)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE media_assets SET status='quarantined' WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO voice_asset_retirements(tenant_id,plan_id,paragraph_key,asset_id,eligible_at)
		VALUES(?,?,?,?,UTC_TIMESTAMP(3))`, scope.Tenant, scope.Plan, job, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO voice_cleanup_jobs(id,tenant_id,plan_id,category,voice_key,before_json,after_json,actor_user_id,mode,scan_done,state)
		VALUES(?,?,?,?,?,?,?,?,'retired',TRUE,'processing')`, job, scope.Tenant, scope.Plan, scope.Category, scope.Voice, data, data, actor); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO voice_cleanup_items(job_id,asset_id) VALUES(?,?)`, job, id); err != nil {
		return err
	}
	return nil
}
