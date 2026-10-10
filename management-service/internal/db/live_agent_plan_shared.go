package db

import (
	"context"
	"database/sql"
	"livecompanion/management/internal/model"
)

// Rooms select a plan, not a private copy of its saved content. The legacy
// publication table is retained as a compatibility pointer; every room using
// this plan and content mode points to the same immutable version row.
func publishSharedPlanPointersTx(ctx context.Context, tx *sql.Tx, tenant, plan, version, actor int64, mode string) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_room_plan_publications (tenant_id,room_id,plan_id,version_id,published_by_user_id,published_at)
		SELECT binding.tenant_id,binding.room_id,binding.plan_id,?,?,CURRENT_TIMESTAMP(3)
		FROM live_agent_plan_room_bindings binding
		LEFT JOIN live_agent_room_plan_selections selection ON selection.tenant_id=binding.tenant_id AND selection.room_id=binding.room_id
		LEFT JOIN live_room_content_access access ON access.tenant_id=binding.tenant_id AND access.room_id=binding.room_id
		WHERE binding.tenant_id=? AND binding.plan_id=? AND binding.status='active'
		  AND (selection.plan_id IS NULL OR selection.plan_id=binding.plan_id)
		  AND (access.selected_mode IS NULL OR access.selected_mode=?)
		ON DUPLICATE KEY UPDATE plan_id=VALUES(plan_id),version_id=VALUES(version_id),published_by_user_id=VALUES(published_by_user_id),published_at=VALUES(published_at)
	`, version, actor, tenant, plan, mode)
	return err
}

// Explicit repair for a user's existing plan. Repoint rooms to existing saved
// rows; do not create copies, regenerate content, or change running sessions.
func (s *Store) UnifyLiveAgentPlanSavedVersions(ctx context.Context, tenant, plan int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var locked int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenant, plan).Scan(&locked); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, liveAgentPlanVersionSelect+` WHERE tenant_id=? AND plan_id=? AND lifecycle_status IN ('draft','published','superseded') ORDER BY id DESC`, tenant, plan)
	if err != nil {
		return err
	}
	heads := map[string]model.LiveAgentPlanVersion{}
	for rows.Next() {
		v, scanErr := scanLiveAgentPlanVersion(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		mode := model.LiveContentModeFromGenerationContext(v.GenerationContext)
		if _, exists := heads[mode]; !exists {
			heads[mode] = v
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for mode, v := range heads {
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_versions SET lifecycle_status='superseded' WHERE tenant_id=? AND plan_id=? AND id<>? AND lifecycle_status IN ('draft','published') AND COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(generation_context_json,'$.content_mode')),''),'ai_pregenerated')=?`, tenant, plan, v.ID, mode); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_versions SET lifecycle_status='published' WHERE tenant_id=? AND plan_id=? AND id=?`, tenant, plan, v.ID); err != nil {
			return err
		}
		actor := int64(0)
		if v.PublishedByUserID != nil {
			actor = *v.PublishedByUserID
		} else if v.CreatedByUserID != nil {
			actor = *v.CreatedByUserID
		}
		if err = publishSharedPlanPointersTx(ctx, tx, tenant, plan, v.ID, actor, mode); err != nil {
			return err
		}
	}
	return tx.Commit()
}
