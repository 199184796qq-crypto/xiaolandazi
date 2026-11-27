package db

import (
	"context"
	"database/sql"
	"errors"
)

const upsertLiveAgentRoomPlanSelection = `
		INSERT INTO live_agent_room_plan_selections (
			tenant_id, room_id, plan_id, selected_by_user_id, selected_at
		) VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP(3))
		ON DUPLICATE KEY UPDATE
			plan_id=VALUES(plan_id), selected_by_user_id=VALUES(selected_by_user_id),
			selected_at=CURRENT_TIMESTAMP(3), updated_at=CURRENT_TIMESTAMP(3)
	`

type LiveAgentRoomPlanChange struct {
	PlanID              int64 // The target plan, or the expected current plan when clearing.
	ActorID             int64
	Clear               bool
	RequireSupportGrant bool
}

// ChangeLiveAgentRoomPlan is shared by customer and delegated room actions.
func (s *Store) ChangeLiveAgentRoomPlan(ctx context.Context, tenantID, roomID int64, change LiveAgentRoomPlanChange) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if err := lockUsableRoomTx(ctx, tx, tenantID, roomID); err != nil {
		if errors.Is(err, errRoomDeleting) {
			err = sql.ErrNoRows
		}
		return "", err
	}

	session, err := lockActiveSessionByRoom(ctx, tx, tenantID, roomID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if session.Status == "running" {
		return "", ErrLiveAgentPlanInUse
	}

	var planName string
	if change.RequireSupportGrant {
		// Hold the exact room grant until committing the selection. A concurrent
		// revoke cannot turn authorization into a check-then-write race.
		err := tx.QueryRowContext(ctx, `SELECT p.name`+livePlanSupportGrantFrom+`
			WHERE r.tenant_id=? AND r.plan_id=? AND r.staff_user_id=? AND r.authorized_room_id=? AND `+activeLivePlanSupportGrant+`
			FOR UPDATE OF r, p`, tenantID, change.PlanID, change.ActorID, roomID).Scan(&planName)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrLiveSupportRequestAccess
		}
		if err != nil {
			return "", err
		}
	} else if !change.Clear {
		err := tx.QueryRowContext(ctx, `SELECT name FROM live_agent_plans
			WHERE tenant_id=? AND id=? AND status='active' FOR UPDATE`, tenantID, change.PlanID).Scan(&planName)
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrLiveAgentPlanNotFound
		}
		if err != nil {
			return "", err
		}
	}

	var currentPlanID int64
	err = tx.QueryRowContext(ctx, `SELECT plan_id FROM live_agent_room_plan_selections
		WHERE tenant_id=? AND room_id=? FOR UPDATE`, tenantID, roomID).Scan(&currentPlanID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	targetPlanID := change.PlanID
	if change.Clear {
		if currentPlanID > 0 && currentPlanID != change.PlanID {
			return "", ErrLiveAgentPlanSelectionChanged
		}
		// Record the explicit empty state. A missing row could be repopulated
		// from an old binding by the startup migration.
		targetPlanID, planName = 0, ""
	} else {
		published, readErr := hasPublishedLiveAgentPlan(ctx, tx, tenantID, targetPlanID)
		if readErr != nil {
			return "", readErr
		}
		if !published {
			return "", ErrLiveAgentPlanNotPublished
		}
	}

	if err == nil && targetPlanID == currentPlanID {
		return planName, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, upsertLiveAgentRoomPlanSelection, tenantID, roomID, targetPlanID, change.ActorID); err != nil {
		return "", err
	}
	if session.Status == "paused" {
		// A resume based on the old paused plan must reload the selection.
		if _, err := tx.ExecContext(ctx, `UPDATE live_runtime_sessions SET version=version+1 WHERE id=?`, session.ID); err != nil {
			return "", err
		}
	}
	return planName, tx.Commit()
}
