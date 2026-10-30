package db

import (
	"context"
	"database/sql"
	"fmt"
	"livecompanion/management/internal/model"
)

// publishLatestPlanPointersTx 在「绑定房间」时把该方案当前每个内容模式下最新的有效版本
// 回填给这个房间，实现「绑定即生效、无需再发布」。方案级存档（room_id=0）与其它房间
// 保存的版本都一样是方案级资产，绑定后即可被直播间直接取用。
func publishLatestPlanPointersTx(ctx context.Context, tx *sql.Tx, tenant, plan, room, actor int64) error {
	// These rows may contain a complete long recording. Choose per-mode heads
	// by ID in memory instead of asking MySQL to sort full JSON rows.
	rows, err := tx.QueryContext(ctx, liveAgentPlanVersionSelect+`
		WHERE tenant_id=? AND plan_id=? AND lifecycle_status IN ('draft','published','superseded')
	`, tenant, plan)
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
		if previous, exists := heads[mode]; !exists || v.ID > previous.ID {
			heads[mode] = v
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for mode, v := range heads {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_agent_room_plan_publications (tenant_id,room_id,plan_id,version_id,published_by_user_id,published_at)
			VALUES (?,?,?,?,?,CURRENT_TIMESTAMP(3))
			ON DUPLICATE KEY UPDATE version_id=VALUES(version_id),published_by_user_id=VALUES(published_by_user_id),published_at=VALUES(published_at)
		`, tenant, room, plan, v.ID, actor); err != nil {
			return fmt.Errorf("publish latest %s plan pointers: %w", mode, err)
		}
	}
	return nil
}

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
	rows, err := tx.QueryContext(ctx, liveAgentPlanVersionSelect+` WHERE tenant_id=? AND plan_id=? AND lifecycle_status IN ('draft','published','superseded')`, tenant, plan)
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
		if previous, exists := heads[mode]; !exists || v.ID > previous.ID {
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
