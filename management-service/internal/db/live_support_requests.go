package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

var (
	ErrLiveSupportRequestEmpty  = errors.New("live support request capabilities empty")
	ErrLiveSupportRequestState  = errors.New("live support request state invalid")
	ErrLiveSupportRequestAccess = errors.New("live support request access denied")
)

func scanLiveSupportRequest(scanner interface{ Scan(...any) error }) (model.LiveSupportRequest, error) {
	var item model.LiveSupportRequest
	var capabilitiesRaw []byte
	var decidedBy sql.NullInt64
	var decidedAt sql.NullTime
	err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&item.RoomID,
		&item.StaffUserID,
		&item.StaffUsername,
		&item.StaffDisplayName,
		&item.RequestedByUserID,
		&capabilitiesRaw,
		&item.Status,
		&decidedBy,
		&item.DecisionNote,
		&item.RequestedAt,
		&decidedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	item.Capabilities = []string{}
	_ = json.Unmarshal(capabilitiesRaw, &item.Capabilities)
	if decidedBy.Valid {
		value := decidedBy.Int64
		item.DecidedByUserID = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time
		item.DecidedAt = &value
	}
	return item, nil
}

func (s *Store) validateLiveSupportRequestCapabilities(
	ctx context.Context,
	staffUserID int64,
	capabilities []string,
) ([]string, error) {
	normalized, err := normalizeLiveSupportCapabilities(capabilities)
	if err != nil {
		return nil, err
	}
	if len(normalized) == 0 {
		return nil, ErrLiveSupportRequestEmpty
	}
	eligible, err := s.IsEligibleLiveSupportStaff(ctx, staffUserID)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, ErrLiveSupportRequestAccess
	}
	access, err := s.GetStaffAccess(ctx, staffUserID)
	if err != nil {
		return nil, err
	}
	for _, capability := range normalized {
		if !access.CanUseLiveSupportCapability(capability) {
			return nil, ErrLiveSupportRequestAccess
		}
		if capability == model.LiveSupportCapabilityL3Policy && !access.CanDelegateLivePolicyL3() {
			return nil, ErrLiveSupportL3Ineligible
		}
	}
	return normalized, nil
}

func (s *Store) CreateLiveSupportRequest(
	ctx context.Context,
	tenantID, roomID, staffUserID, requestedByUserID int64,
	capabilities []string,
) (model.LiveSupportRequest, error) {
	normalized, err := s.validateLiveSupportRequestCapabilities(ctx, staffUserID, capabilities)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_support_requests
		SET status='cancelled',
		    decision_note='客户提交了新的协助申请',
		    decided_by_user_id=?,
		    decided_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND status='pending'
	`, requestedByUserID, tenantID, roomID, staffUserID); err != nil {
		return model.LiveSupportRequest{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO live_support_requests (
			tenant_id, room_id, staff_user_id, requested_by_user_id,
			capabilities_json, status
		)
		VALUES (?, ?, ?, ?, CAST(? AS JSON), 'pending')
	`, tenantID, roomID, staffUserID, requestedByUserID, string(raw))
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	requestID, err := result.LastInsertId()
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	for _, capability := range normalized {
		if err := insertLiveSupportEventTx(
			ctx, tx, "request.created", tenantID, roomID, staffUserID,
			requestedByUserID, capability,
			map[string]any{"request_id": requestID, "status": "pending"},
		); err != nil {
			return model.LiveSupportRequest{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.LiveSupportRequest{}, err
	}
	return s.GetLiveSupportRequest(ctx, requestID)
}

func (s *Store) GetLiveSupportRequest(ctx context.Context, requestID int64) (model.LiveSupportRequest, error) {
	return scanLiveSupportRequest(s.db.QueryRowContext(ctx, `
		SELECT
			r.id, r.tenant_id, r.room_id, r.staff_user_id,
			COALESCE(u.username,''), COALESCE(u.display_name,''),
			r.requested_by_user_id, r.capabilities_json, r.status,
			r.decided_by_user_id, r.decision_note,
			r.requested_at, r.decided_at, r.updated_at
		FROM live_support_requests r
		LEFT JOIN mgmt_users u ON u.id=r.staff_user_id
		WHERE r.id=?
	`, requestID))
}

func (s *Store) ListLiveSupportRequestsForRoom(
	ctx context.Context,
	tenantID, roomID int64,
) ([]model.LiveSupportRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id, r.tenant_id, r.room_id, r.staff_user_id,
			COALESCE(u.username,''), COALESCE(u.display_name,''),
			r.requested_by_user_id, r.capabilities_json, r.status,
			r.decided_by_user_id, r.decision_note,
			r.requested_at, r.decided_at, r.updated_at
		FROM live_support_requests r
		LEFT JOIN mgmt_users u ON u.id=r.staff_user_id
		WHERE r.tenant_id=? AND r.room_id=?
		ORDER BY r.requested_at DESC, r.id DESC
		LIMIT 100
	`, tenantID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveSupportRequest, 0)
	for rows.Next() {
		item, err := scanLiveSupportRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListLiveSupportRequestsForStaff(
	ctx context.Context,
	staffUserID int64,
) ([]model.LiveSupportRequest, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id, r.tenant_id, r.room_id, r.staff_user_id,
			COALESCE(u.username,''), COALESCE(u.display_name,''),
			r.requested_by_user_id, r.capabilities_json, r.status,
			r.decided_by_user_id, r.decision_note,
			r.requested_at, r.decided_at, r.updated_at
		FROM live_support_requests r
		LEFT JOIN mgmt_users u ON u.id=r.staff_user_id
		WHERE r.staff_user_id=?
		ORDER BY
			CASE r.status WHEN 'pending' THEN 0 ELSE 1 END,
			r.requested_at DESC,
			r.id DESC
		LIMIT 200
	`, staffUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveSupportRequest, 0)
	for rows.Next() {
		item, err := scanLiveSupportRequest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AcceptLiveSupportRequest(
	ctx context.Context,
	requestID, staffUserID int64,
	note string,
) (model.LiveSupportRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	defer tx.Rollback()

	var tenantID, roomID, targetStaffID, requestedBy int64
	var capabilitiesRaw []byte
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, room_id, staff_user_id, requested_by_user_id,
		       capabilities_json, status
		FROM live_support_requests
		WHERE id=?
		FOR UPDATE
	`, requestID).Scan(
		&tenantID,
		&roomID,
		&targetStaffID,
		&requestedBy,
		&capabilitiesRaw,
		&status,
	); err != nil {
		return model.LiveSupportRequest{}, err
	}
	if targetStaffID != staffUserID {
		return model.LiveSupportRequest{}, ErrLiveSupportRequestAccess
	}
	if status != "pending" {
		return model.LiveSupportRequest{}, ErrLiveSupportRequestState
	}
	var capabilities []string
	if err := json.Unmarshal(capabilitiesRaw, &capabilities); err != nil {
		return model.LiveSupportRequest{}, err
	}
	normalized, err := s.validateLiveSupportRequestCapabilities(ctx, staffUserID, capabilities)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}

	for _, capability := range normalized {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_support_authorizations (
				tenant_id, room_id, staff_user_id, capability, status,
				granted_by_user_id, granted_at, revoked_by_user_id, revoked_at
			)
			VALUES (?, ?, ?, ?, 'active', ?, CURRENT_TIMESTAMP(3), NULL, NULL)
			ON DUPLICATE KEY UPDATE
				status='active',
				granted_by_user_id=VALUES(granted_by_user_id),
				granted_at=CURRENT_TIMESTAMP(3),
				revoked_by_user_id=NULL,
				revoked_at=NULL,
				updated_at=CURRENT_TIMESTAMP(3)
		`, tenantID, roomID, staffUserID, capability, requestedBy); err != nil {
			return model.LiveSupportRequest{}, err
		}
		if err := insertLiveSupportEventTx(
			ctx, tx, "request.accept", tenantID, roomID, staffUserID,
			staffUserID, capability,
			map[string]any{"request_id": requestID, "requested_by_user_id": requestedBy},
		); err != nil {
			return model.LiveSupportRequest{}, err
		}
		if err := insertLiveSupportEventTx(
			ctx, tx, "authorization.grant", tenantID, roomID, staffUserID,
			requestedBy, capability,
			map[string]any{"request_id": requestID, "accepted_by_staff_user_id": staffUserID},
		); err != nil {
			return model.LiveSupportRequest{}, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_support_requests
		SET status='accepted',
		    decided_by_user_id=?,
		    decision_note=?,
		    decided_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, staffUserID, strings.TrimSpace(note), requestID); err != nil {
		return model.LiveSupportRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveSupportRequest{}, err
	}
	return s.GetLiveSupportRequest(ctx, requestID)
}

func (s *Store) RejectLiveSupportRequest(
	ctx context.Context,
	requestID, staffUserID int64,
	note string,
) (model.LiveSupportRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveSupportRequest{}, err
	}
	defer tx.Rollback()

	var tenantID, roomID, targetStaffID int64
	var capabilitiesRaw []byte
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, room_id, staff_user_id, capabilities_json, status
		FROM live_support_requests
		WHERE id=?
		FOR UPDATE
	`, requestID).Scan(&tenantID, &roomID, &targetStaffID, &capabilitiesRaw, &status); err != nil {
		return model.LiveSupportRequest{}, err
	}
	if targetStaffID != staffUserID {
		return model.LiveSupportRequest{}, ErrLiveSupportRequestAccess
	}
	if status != "pending" {
		return model.LiveSupportRequest{}, ErrLiveSupportRequestState
	}
	var capabilities []string
	_ = json.Unmarshal(capabilitiesRaw, &capabilities)

	if _, err := tx.ExecContext(ctx, `
		UPDATE live_support_requests
		SET status='rejected',
		    decided_by_user_id=?,
		    decision_note=?,
		    decided_at=CURRENT_TIMESTAMP(3),
		    updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, staffUserID, strings.TrimSpace(note), requestID); err != nil {
		return model.LiveSupportRequest{}, err
	}
	for _, capability := range capabilities {
		if !validLiveSupportCapability(capability) {
			continue
		}
		if err := insertLiveSupportEventTx(
			ctx, tx, "request.reject", tenantID, roomID, staffUserID,
			staffUserID, capability,
			map[string]any{"request_id": requestID, "note": strings.TrimSpace(note)},
		); err != nil {
			return model.LiveSupportRequest{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.LiveSupportRequest{}, err
	}
	return s.GetLiveSupportRequest(ctx, requestID)
}

func (s *Store) HasPendingLiveSupportRequest(
	ctx context.Context,
	tenantID, roomID, staffUserID int64,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM live_support_requests
		WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND status='pending'
	`, tenantID, roomID, staffUserID).Scan(&count)
	return count > 0, err
}

func LiveSupportRequestStatusLabel(status string) string {
	switch status {
	case "pending":
		return "pending"
	case "accepted":
		return "accepted"
	case "rejected":
		return "rejected"
	case "cancelled":
		return "cancelled"
	default:
		return fmt.Sprintf("%s", status)
	}
}
