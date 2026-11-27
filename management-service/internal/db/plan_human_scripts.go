package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"livecompanion/management/internal/voiceasset"
)

// A separate scope keeps recordings in this library out of per-piece playback.
var PlanHumanScriptScope = voiceasset.Digest([]string{"plan-human-scripts-v1"})

var ErrPlanHumanScriptOrder = errors.New("录音列表已变化，请刷新后重新排序")

func (s *Store) ReorderPlanHumanScripts(ctx context.Context, tenant, plan int64, order []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var active int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE", plan, tenant).Scan(&active); err != nil {
		return err
	}
	prefix := ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope) + "%"
	rows, err := tx.QueryContext(ctx, "SELECT id FROM media_assets WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ?", tenant, voiceasset.ScriptHumanAsset, prefix)
	if err != nil {
		return err
	}
	remaining := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		remaining[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(remaining) != len(order) {
		return ErrPlanHumanScriptOrder
	}
	for index, id := range order {
		if !remaining[id] {
			return ErrPlanHumanScriptOrder
		}
		delete(remaining, id)
		if _, err = tx.ExecContext(ctx, "UPDATE media_assets SET metadata_json=JSON_SET(metadata_json,'$.script_order',?) WHERE id=?", index+1, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SavePlanHumanScriptTranscript(ctx context.Context, tenant, plan, id int64, text string) error {
	result, err := s.db.ExecContext(ctx, "UPDATE media_assets SET metadata_json=JSON_REMOVE(JSON_SET(metadata_json,'$.script_transcript',?,'$.script_asr_status','ready'),'$.script_asr_token','$.script_asr_expires') WHERE id=? AND tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ?",
		text, id, tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%")
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != 0 {
		return err
	}
	_, err = s.ScriptHumanVoice(ctx, tenant, plan, PlanHumanScriptScope, id)
	return err
}

type PlanHumanScriptASRJob struct {
	TenantID, PlanID, AssetID int64
}

func (s *Store) NextPlanHumanScriptASR(ctx context.Context) (PlanHumanScriptASRJob, error) {
	var job PlanHumanScriptASRJob
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id, JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.plan_id')), id
		FROM media_assets WHERE asset_type=? AND status='active' AND object_key LIKE ?
		AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_status'))='queued'
		ORDER BY id LIMIT 1`, voiceasset.ScriptHumanAsset, "%/"+PlanHumanScriptScope+"/%").Scan(&job.TenantID, &job.PlanID, &job.AssetID)
	return job, err
}

func (s *Store) QueuePlanHumanScriptASR(ctx context.Context, tenant, plan, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media_assets SET metadata_json=JSON_REMOVE(
		JSON_SET(metadata_json,'$.script_asr_status','queued'),'$.script_asr_token','$.script_asr_expires')
		WHERE id=? AND tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ?
		AND (COALESCE(JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_status')),'') IN ('','failed')
		OR (JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_status'))='processing'
		    AND COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_expires')) AS UNSIGNED),0)<=UNIX_TIMESTAMP()))`,
		id, tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%")
	return err
}

func (s *Store) ClaimPlanHumanScriptASR(ctx context.Context, tenant, plan, id int64, token string, until time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, "UPDATE media_assets SET metadata_json=JSON_SET(metadata_json,'$.script_asr_status','processing','$.script_asr_token',?,'$.script_asr_expires',?) WHERE id=? AND tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_status'))='queued'",
		token, until.Unix(), id, tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%")
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) FinishPlanHumanScriptASR(ctx context.Context, tenant, plan, id int64, token, text string) error {
	update := "JSON_SET(metadata_json,'$.script_asr_status','failed')"
	args := []any{}
	if text != "" {
		update = "JSON_SET(metadata_json,'$.script_asr_status','ready','$.script_transcript',?)"
		args = append(args, text)
	}
	args = append(args, id, tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, PlanHumanScriptScope)+"%", token)
	_, err := s.db.ExecContext(ctx, "UPDATE media_assets SET metadata_json=JSON_REMOVE("+update+",'$.script_asr_token','$.script_asr_expires') WHERE id=? AND tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.script_asr_token'))=?", args...)
	return err
}

func retirePlanHumanScripts(ctx context.Context, tx *sql.Tx, tenant, plan, actor int64) error {
	// Lock the plan before checking its assets, as upload completion does.
	var locked int64
	if err := tx.QueryRowContext(ctx, "SELECT id FROM live_agent_plans WHERE id=? AND tenant_id=? FOR UPDATE", plan, tenant).Scan(&locked); err != nil {
		return err
	}
	scope := voiceasset.Scope{Tenant: tenant, Plan: plan, Category: voiceasset.ScriptHumanCategory, Voice: PlanHumanScriptScope}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM media_assets WHERE tenant_id=? AND asset_type=? AND status='active' AND object_key LIKE ? FOR UPDATE", tenant, voiceasset.ScriptHumanAsset, ScriptHumanVoicePrefix(tenant, plan, scope.Voice)+"%")
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
	if len(ids) == 0 {
		return nil
	}
	if err := lockVoiceScope(ctx, tx, scope); err != nil {
		return err
	}
	for _, id := range ids {
		if err := retireScriptHumanVoice(ctx, tx, scope, id, actor); err != nil {
			return err
		}
	}
	return nil
}
