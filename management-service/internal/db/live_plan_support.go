package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLivePlanSupportRoomUnavailable = errors.New("live plan support room unavailable")

type LivePlanSupportRequest struct {
	ID                 int64      `json:"id"`
	TenantID           int64      `json:"tenant_id"`
	PlanID             int64      `json:"plan_id"`
	PlanName           string     `json:"plan_name"`
	StaffUserID        int64      `json:"staff_user_id"`
	AuthorizedRoomID   int64      `json:"authorized_room_id,omitempty"`
	AuthorizedRoomName string     `json:"authorized_room_name,omitempty"`
	Status             string     `json:"status"`
	RequestedAt        time.Time  `json:"requested_at"`
	DecidedAt          *time.Time `json:"decided_at,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

const livePlanSupportSelect = `
	SELECT r.id, r.tenant_id, r.plan_id, p.name, r.staff_user_id, r.status,
	       COALESCE(r.authorized_room_id,0), COALESCE(NULLIF(room.name,''), NULLIF(room.external_room_id,''), CONCAT('直播间 #',room.id), ''),
	       r.requested_at, r.decided_at, r.updated_at
	FROM live_plan_support_requests r
	JOIN live_agent_plans p ON p.id=r.plan_id AND p.tenant_id=r.tenant_id AND p.status='active'
	LEFT JOIN core_rooms room ON room.id=r.authorized_room_id AND room.tenant_id=r.tenant_id
	    AND NOT EXISTS (SELECT 1 FROM mgmt_room_deletions d WHERE d.tenant_id=room.tenant_id AND d.room_id=room.id)
`

func scanLivePlanSupportRequest(row interface{ Scan(...any) error }) (LivePlanSupportRequest, error) {
	var item LivePlanSupportRequest
	err := row.Scan(&item.ID, &item.TenantID, &item.PlanID, &item.PlanName, &item.StaffUserID, &item.Status,
		&item.AuthorizedRoomID, &item.AuthorizedRoomName, &item.RequestedAt, &item.DecidedAt, &item.UpdatedAt)
	return item, err
}

func (s *Store) listLivePlanSupportRequests(ctx context.Context, filter string, args ...any) ([]LivePlanSupportRequest, error) {
	rows, err := s.db.QueryContext(ctx, livePlanSupportSelect+filter+`
		ORDER BY r.updated_at DESC, r.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LivePlanSupportRequest{}
	for rows.Next() {
		item, err := scanLivePlanSupportRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListLivePlanSupportForPlan(ctx context.Context, tenantID, planID int64) ([]LivePlanSupportRequest, error) {
	return s.listLivePlanSupportRequests(ctx, ` WHERE r.tenant_id=? AND r.plan_id=? AND r.open_slot=1`, tenantID, planID)
}

func (s *Store) ListLivePlanSupportForStaff(ctx context.Context, staffID int64) ([]LivePlanSupportRequest, error) {
	return s.listLivePlanSupportRequests(ctx, ` WHERE r.staff_user_id=? AND r.open_slot=1`, staffID)
}

func (s *Store) CreateLivePlanSupport(ctx context.Context, tenantID, planID, staffID, requesterID, roomID int64) (LivePlanSupportRequest, error) {
	if _, err := s.validateLiveSupportRequestCapabilities(ctx, staffID, []string{model.LiveSupportCapabilityL3Policy}); err != nil {
		return LivePlanSupportRequest{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	defer tx.Rollback()
	if roomID != 0 {
		if err := lockUsableRoomTx(ctx, tx, tenantID, roomID); err != nil {
			if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errRoomDeleting) {
				return LivePlanSupportRequest{}, ErrLivePlanSupportRoomUnavailable
			}
			return LivePlanSupportRequest{}, err
		}
	}
	// Concurrent submits for the same plan and employee return the one open
	// request. A new request after rejection or revocation gets a new ID.
	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_plan_support_requests (tenant_id, plan_id, staff_user_id, requested_by_user_id, authorized_room_id)
		SELECT p.tenant_id, p.id, ?, ?, NULLIF(?,0) FROM live_agent_plans p
		WHERE p.tenant_id=? AND p.id=? AND p.status='active'
		ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(live_plan_support_requests.id)
	`, staffID, requesterID, roomID, tenantID, planID)
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	if id <= 0 {
		return LivePlanSupportRequest{}, ErrLiveAgentPlanNotFound
	}
	item, err := scanLivePlanSupportRequest(tx.QueryRowContext(ctx, livePlanSupportSelect+" WHERE r.id=?", id))
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	if item.AuthorizedRoomID != roomID {
		return LivePlanSupportRequest{}, ErrLiveSupportRequestState
	}
	return item, s.commitInboxTx(ctx, tx, "support")
}

func (s *Store) DecideLivePlanSupport(ctx context.Context, requestID, staffID int64, accept bool) (LivePlanSupportRequest, error) {
	if _, err := s.validateLiveSupportRequestCapabilities(ctx, staffID, []string{model.LiveSupportCapabilityL3Policy}); err != nil {
		return LivePlanSupportRequest{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	defer tx.Rollback()
	item, err := scanLivePlanSupportRequest(tx.QueryRowContext(ctx,
		livePlanSupportSelect+" WHERE r.id=? AND r.staff_user_id=? FOR UPDATE OF r, p", requestID, staffID))
	if errors.Is(err, sql.ErrNoRows) {
		return LivePlanSupportRequest{}, ErrLiveSupportRequestAccess
	}
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	if item.Status != "pending" {
		return LivePlanSupportRequest{}, ErrLiveSupportRequestState
	}
	status := "rejected"
	if accept {
		status = "accepted"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE live_plan_support_requests SET status=?, decided_at=CURRENT_TIMESTAMP(3) WHERE id=?
	`, status, requestID); err != nil {
		return LivePlanSupportRequest{}, err
	}
	item, err = scanLivePlanSupportRequest(tx.QueryRowContext(ctx, livePlanSupportSelect+" WHERE r.id=?", requestID))
	if err != nil {
		return LivePlanSupportRequest{}, err
	}
	return item, s.commitInboxTx(ctx, tx, "support")
}

func (s *Store) RevokeLivePlanSupport(ctx context.Context, tenantID, planID, requestID, userID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
		UPDATE live_plan_support_requests SET status='revoked', revoked_by_user_id=?
		WHERE id=? AND tenant_id=? AND plan_id=? AND open_slot=1
	`, userID, requestID, tenantID, planID)
	if err != nil {
		return err
	}
	if count, err := res.RowsAffected(); err != nil {
		return err
	} else if count != 1 {
		return ErrLiveSupportRequestState
	}
	return s.commitInboxTx(ctx, tx, "support")
}

// This predicate is shared by plan editing and delegated room binding.
const activeLivePlanSupportGrant = `
	r.open_slot=1 AND r.status='accepted'
	AND EXISTS (
		SELECT 1 FROM staff_employees e
		JOIN staff_groups g ON g.id=e.primary_group_id
		JOIN mgmt_users u ON u.id=e.user_id
		WHERE e.user_id=r.staff_user_id AND e.employment_status='active'
		  AND g.code='live_operations' AND g.status='active' AND u.status='active'
	)
`

const livePlanSupportGrantFrom = `
	FROM live_plan_support_requests r
	JOIN live_agent_plans p ON p.id=r.plan_id AND p.tenant_id=r.tenant_id AND p.status='active'
`

func (s *Store) AuthorizedLivePlanSupportTenant(ctx context.Context, planID, staffID int64) (int64, error) {
	var tenantID int64
	err := s.db.QueryRowContext(ctx, `SELECT r.tenant_id`+livePlanSupportGrantFrom+`
		WHERE r.plan_id=? AND r.staff_user_id=? AND `+activeLivePlanSupportGrant,
		planID, staffID,
	).Scan(&tenantID)
	return tenantID, err
}

type AuthorizedRoomPlan struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Selected bool   `json:"selected"`
}

func (s *Store) ListAuthorizedRoomPlans(ctx context.Context, tenantID, roomID, staffID int64) ([]AuthorizedRoomPlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name, COALESCE(selection.plan_id=p.id,0)
	`+livePlanSupportGrantFrom+`
		LEFT JOIN live_agent_room_plan_selections selection
		  ON selection.tenant_id=r.tenant_id AND selection.room_id=r.authorized_room_id
		WHERE r.tenant_id=? AND r.staff_user_id=? AND r.authorized_room_id=? AND `+activeLivePlanSupportGrant+`
		  AND EXISTS (SELECT 1 FROM core_rooms room WHERE room.tenant_id=r.tenant_id AND room.id=r.authorized_room_id)
		  AND NOT EXISTS (SELECT 1 FROM mgmt_room_deletions d WHERE d.tenant_id=r.tenant_id AND d.room_id=r.authorized_room_id)
		ORDER BY p.name, p.id
	`, tenantID, staffID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]AuthorizedRoomPlan, 0)
	for rows.Next() {
		var item AuthorizedRoomPlan
		if err := rows.Scan(&item.ID, &item.Name, &item.Selected); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
