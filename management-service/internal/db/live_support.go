package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveSupportL3Ineligible = errors.New("用户层仅能授权给具备行业层配置能力和用户层授权协助权限的运维员工")

func validLiveSupportCapability(capability string) bool {
	switch strings.TrimSpace(capability) {
	case model.LiveSupportCapabilityL3Policy,
		model.LiveSupportCapabilityAnchorTraining,
		model.LiveSupportCapabilityVoiceClone:
		return true
	default:
		return false
	}
}

func normalizeLiveSupportCapabilities(values []string) ([]string, error) {
	set := make(map[string]struct{})
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if !validLiveSupportCapability(value) {
			return nil, fmt.Errorf("invalid live support capability: %s", value)
		}
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func (s *Store) ListLiveSupportStaff(ctx context.Context) ([]model.LiveSupportStaff, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT u.id, u.username, u.display_name, COALESCE(u.avatar_url, '')
		FROM staff_employees e
		INNER JOIN mgmt_users u ON u.id=e.user_id
		INNER JOIN staff_groups g ON g.id=e.primary_group_id
		WHERE g.code='live_operations'
		  AND e.employment_status='active'
		  AND u.status='active'
		ORDER BY u.display_name ASC, u.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveSupportStaff, 0)
	for rows.Next() {
		var item model.LiveSupportStaff
		if err := rows.Scan(&item.UserID, &item.Username, &item.DisplayName, &item.AvatarURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range items {
		access, err := s.GetStaffAccess(ctx, items[index].UserID)
		if err != nil {
			return nil, err
		}
		items[index].AllowedCapabilities = []string{}
		items[index].SpecialtyIndustries = []string{}
		for _, capability := range []string{model.LiveSupportCapabilityL3Policy, model.LiveSupportCapabilityAnchorTraining, model.LiveSupportCapabilityVoiceClone} {
			if access.CanUseLiveSupportCapability(capability) {
				items[index].AllowedCapabilities = append(items[index].AllowedCapabilities, capability)
			}
		}
		if !access.CanDelegateLivePolicyL3() {
			items[index].L3RestrictionReason = "该员工缺少行业层配置能力或用户层授权协助权限，不能接受用户层代维护授权"
		}
	}
	return items, nil
}

func (s *Store) IsEligibleLiveSupportStaff(ctx context.Context, userID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM staff_employees e
		INNER JOIN mgmt_users u ON u.id=e.user_id
		INNER JOIN staff_groups g ON g.id=e.primary_group_id
		WHERE e.user_id=?
		  AND g.code='live_operations'
		  AND e.employment_status='active'
		  AND u.status='active'
	`, userID).Scan(&count)
	return count > 0, err
}

func scanLiveSupportAuthorization(scanner interface{ Scan(...any) error }) (model.LiveSupportAuthorization, error) {
	var item model.LiveSupportAuthorization
	var revokedBy sql.NullInt64
	var revokedAt sql.NullTime
	err := scanner.Scan(
		&item.ID,
		&item.TenantID,
		&item.RoomID,
		&item.StaffUserID,
		&item.StaffUsername,
		&item.StaffDisplayName,
		&item.Capability,
		&item.Status,
		&item.GrantedByUserID,
		&item.GrantedAt,
		&revokedBy,
		&revokedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.LiveSupportAuthorization{}, err
	}
	if revokedBy.Valid {
		value := revokedBy.Int64
		item.RevokedByUserID = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time
		item.RevokedAt = &value
	}
	return item, nil
}

func (s *Store) ListLiveSupportAuthorizationsForRoom(
	ctx context.Context,
	tenantID, roomID int64,
) ([]model.LiveSupportAuthorization, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			a.id, a.tenant_id, a.room_id, a.staff_user_id,
			COALESCE(u.username, ''), COALESCE(u.display_name, ''),
			a.capability, a.status, a.granted_by_user_id, a.granted_at,
			a.revoked_by_user_id, a.revoked_at, a.updated_at
		FROM live_support_authorizations a
		LEFT JOIN mgmt_users u ON u.id=a.staff_user_id
		WHERE a.tenant_id=? AND a.room_id=? AND a.status='active'
		ORDER BY u.display_name ASC, a.capability ASC
	`, tenantID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveSupportAuthorization, 0)
	for rows.Next() {
		item, err := scanLiveSupportAuthorization(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListLiveSupportAuthorizationsForStaff(
	ctx context.Context,
	staffUserID int64,
) ([]model.LiveSupportAuthorization, error) {
	access, err := s.GetStaffAccess(ctx, staffUserID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			a.id, a.tenant_id, a.room_id, a.staff_user_id,
			COALESCE(u.username, ''), COALESCE(u.display_name, ''),
			a.capability, a.status, a.granted_by_user_id, a.granted_at,
			a.revoked_by_user_id, a.revoked_at, a.updated_at
		FROM live_support_authorizations a
		LEFT JOIN mgmt_users u ON u.id=a.staff_user_id
		WHERE a.staff_user_id=? AND a.status='active'
		ORDER BY a.room_id ASC, a.capability ASC
	`, staffUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveSupportAuthorization, 0)
	for rows.Next() {
		item, err := scanLiveSupportAuthorization(rows)
		if err != nil {
			return nil, err
		}
		if !access.CanUseLiveSupportCapability(item.Capability) {
			continue
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) HasLiveSupportAuthorization(
	ctx context.Context,
	tenantID, roomID, staffUserID int64,
	capability string,
) (bool, error) {
	if capability == model.LiveSupportCapabilityL3Policy {
		access, err := s.GetStaffAccess(ctx, staffUserID)
		if err != nil {
			return false, err
		}
		if !access.CanDelegateLivePolicyL3() {
			return false, nil
		}
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM live_support_authorizations
		WHERE tenant_id=? AND room_id=? AND staff_user_id=?
		  AND capability=? AND status='active'
	`, tenantID, roomID, staffUserID, capability).Scan(&count)
	return count > 0, err
}

func (s *Store) GetLiveSupportAuthorizedTenant(
	ctx context.Context,
	roomID, staffUserID int64,
	capability string,
) (int64, error) {
	if capability == model.LiveSupportCapabilityL3Policy {
		access, err := s.GetStaffAccess(ctx, staffUserID)
		if err != nil {
			return 0, err
		}
		if !access.CanDelegateLivePolicyL3() {
			return 0, sql.ErrNoRows
		}
	}
	if !validLiveSupportCapability(capability) {
		return 0, fmt.Errorf("invalid live support capability")
	}
	var tenantID int64
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_id
		FROM live_support_authorizations
		WHERE room_id=? AND staff_user_id=?
		  AND capability=? AND status='active'
		LIMIT 1
	`, roomID, staffUserID, capability).Scan(&tenantID)
	return tenantID, err
}

func insertLiveSupportEventTx(
	ctx context.Context,
	tx *sql.Tx,
	action string,
	tenantID, roomID, staffUserID, actorUserID int64,
	capability string,
	detail map[string]any,
) error {
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_support_authorization_events (
			action, tenant_id, room_id, staff_user_id, actor_user_id,
			capability, detail_json
		) VALUES (?, ?, ?, ?, ?, ?, CAST(? AS JSON))
	`, action, tenantID, roomID, staffUserID, actorUserID, capability, string(raw))
	return err
}

func (s *Store) RecordLiveSupportEvent(
	ctx context.Context,
	action string,
	tenantID, roomID, staffUserID, actorUserID int64,
	capability string,
	detail map[string]any,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertLiveSupportEventTx(
		ctx, tx, action, tenantID, roomID, staffUserID, actorUserID, capability, detail,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RegisterLiveSupportConfigVersion(
	ctx context.Context,
	versionID, tenantID, roomID, staffUserID int64,
	capability string,
) error {
	if !validLiveSupportCapability(capability) {
		return fmt.Errorf("invalid live support capability")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_support_config_versions (
			version_id, tenant_id, room_id, staff_user_id, capability
		) VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			tenant_id=VALUES(tenant_id),
			room_id=VALUES(room_id),
			staff_user_id=VALUES(staff_user_id),
			capability=VALUES(capability)
	`, versionID, tenantID, roomID, staffUserID, capability)
	return err
}

func (s *Store) IsLiveSupportConfigVersion(
	ctx context.Context,
	versionID, tenantID, roomID, staffUserID int64,
	capability string,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM live_support_config_versions
		WHERE version_id=? AND tenant_id=? AND room_id=?
		  AND staff_user_id=? AND capability=?
	`, versionID, tenantID, roomID, staffUserID, capability).Scan(&count)
	return count > 0, err
}

func (s *Store) SetLiveSupportAuthorizations(
	ctx context.Context,
	tenantID, roomID, staffUserID, actorUserID int64,
	capabilities []string,
) ([]model.LiveSupportAuthorization, error) {
	normalized, err := normalizeLiveSupportCapabilities(capabilities)
	if err != nil {
		return nil, err
	}
	eligible, err := s.IsEligibleLiveSupportStaff(ctx, staffUserID)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, fmt.Errorf("staff user is not eligible for live support")
	}

	desired := make(map[string]struct{}, len(normalized))
	for _, capability := range normalized {
		desired[capability] = struct{}{}
	}
	if _, requestsL3 := desired[model.LiveSupportCapabilityL3Policy]; requestsL3 {
		access, err := s.GetStaffAccess(ctx, staffUserID)
		if err != nil {
			return nil, err
		}
		if !access.CanDelegateLivePolicyL3() {
			return nil, ErrLiveSupportL3Ineligible
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT capability
		FROM live_support_authorizations
		WHERE tenant_id=? AND room_id=? AND staff_user_id=? AND status='active'
		FOR UPDATE
	`, tenantID, roomID, staffUserID)
	if err != nil {
		return nil, err
	}
	active := make(map[string]struct{})
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			rows.Close()
			return nil, err
		}
		active[capability] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	for capability := range active {
		if _, keep := desired[capability]; keep {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE live_support_authorizations
			SET status='revoked',
			    revoked_by_user_id=?,
			    revoked_at=CURRENT_TIMESTAMP(3),
			    updated_at=CURRENT_TIMESTAMP(3)
			WHERE tenant_id=? AND room_id=? AND staff_user_id=?
			  AND capability=? AND status='active'
		`, actorUserID, tenantID, roomID, staffUserID, capability); err != nil {
			return nil, err
		}
		if err := insertLiveSupportEventTx(
			ctx, tx, "authorization.revoke", tenantID, roomID,
			staffUserID, actorUserID, capability, nil,
		); err != nil {
			return nil, err
		}
	}

	for _, capability := range normalized {
		if _, exists := active[capability]; exists {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO live_support_authorizations (
				tenant_id, room_id, staff_user_id, capability, status,
				granted_by_user_id, granted_at, revoked_by_user_id, revoked_at
			) VALUES (?, ?, ?, ?, 'active', ?, CURRENT_TIMESTAMP(3), NULL, NULL)
			ON DUPLICATE KEY UPDATE
				status='active',
				granted_by_user_id=VALUES(granted_by_user_id),
				granted_at=CURRENT_TIMESTAMP(3),
				revoked_by_user_id=NULL,
				revoked_at=NULL,
				updated_at=CURRENT_TIMESTAMP(3)
		`, tenantID, roomID, staffUserID, capability, actorUserID); err != nil {
			return nil, err
		}
		if err := insertLiveSupportEventTx(
			ctx, tx, "authorization.grant", tenantID, roomID,
			staffUserID, actorUserID, capability, nil,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListLiveSupportAuthorizationsForRoom(ctx, tenantID, roomID)
}
