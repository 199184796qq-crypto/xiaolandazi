package db

import (
	"context"
	"database/sql"
)

const savedPlanVersionsToKeep = 4 // Current snapshot plus three older snapshots.

// Each content mode keeps its own current configuration. A running/paused room
// protects all snapshots until stop: the pinned version is runtime-only state.
func pruneSavedPlanVersions(ctx context.Context, tx *sql.Tx, tenant, plan, room int64) error {
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_runtime_sessions WHERE tenant_id=? AND room_id=? AND (ended_at IS NULL OR status IN ('running','paused','starting','resuming'))`, tenant, room).Scan(&active); err != nil {
		return err
	}
	if active > 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(generation_context_json,'$.content_mode')),''),'ai_pregenerated') FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND room_id=? AND lifecycle_status IN ('draft','published','superseded') ORDER BY version_no DESC,id DESC FOR UPDATE`, tenant, plan, room)
	if err != nil {
		return err
	}
	counts := map[string]int{}
	var obsolete []int64
	for rows.Next() {
		var id int64
		var mode string
		if err := rows.Scan(&id, &mode); err != nil {
			rows.Close()
			return err
		}
		counts[mode]++
		if counts[mode] > savedPlanVersionsToKeep {
			obsolete = append(obsolete, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, id := range obsolete {
		// Preserve effective pointers and in-flight refreshes; do not delete
		// underlying shared media, source facts, or historical event records.
		if _, err := tx.ExecContext(ctx, `DELETE FROM live_agent_plan_versions WHERE tenant_id=? AND plan_id=? AND room_id=? AND id=? AND NOT EXISTS (SELECT 1 FROM live_agent_room_plan_publications p WHERE p.tenant_id=? AND p.room_id=? AND p.version_id=?) AND NOT EXISTS (SELECT 1 FROM live_content_refresh_jobs j WHERE j.tenant_id=? AND j.room_id=? AND (j.base_version_id=? OR j.result_version_id=?) AND j.status NOT IN ('completed','failed','obsolete','cancelled','abandoned'))`, tenant, plan, room, id, tenant, room, id, tenant, room, id, id); err != nil {
			return err
		}
	}
	return nil
}

// Saving while live defers cleanup until this guarded stop path.
func (s *Store) PruneSavedPlanVersionsForRoom(ctx context.Context, tenant, room int64) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT plan_id FROM live_agent_plan_versions WHERE tenant_id=? AND room_id=?`, tenant, room)
	if err != nil {
		return err
	}
	var plans []int64
	for rows.Next() {
		var plan int64
		if err := rows.Scan(&plan); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, plan := range plans {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		var locked int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plans WHERE tenant_id=? AND id=? FOR UPDATE`, tenant, plan).Scan(&locked)
		if err == nil {
			err = pruneSavedPlanVersions(ctx, tx, tenant, plan, room)
		}
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
