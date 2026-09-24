package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Store) ListLiveOpsRoomQuotas(
	ctx context.Context,
) ([]model.LiveOpsRoomQuotaSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			u.tenant_id,
			u.id,
			u.username,
			u.display_name,
			u.phone,
			u.status,
			COALESCE(parent.name, ''),
			COALESCE(m.plan_id, 0),
			COALESCE(p.name, '无会员'),
			LEAST(COALESCE(ml.room_limit, ?), ?),
			COALESCE(rc.room_count, 0),
			LEAST(
				COALESCE(a.balance, ?),
				LEAST(COALESCE(ml.room_limit, ?), ?),
				?
			),
			COALESCE(last.reason, ''),
			COALESCE(op.display_name, op.username, ''),
			last.created_at
		FROM mgmt_users u
		INNER JOIN mgmt_tenants customer_org ON customer_org.id=u.tenant_id
		LEFT JOIN mgmt_tenants parent ON parent.id=customer_org.parent_id
		LEFT JOIN biz_memberships m ON m.id=(
			SELECT m2.id
			FROM biz_memberships m2
			WHERE m2.tenant_id=u.tenant_id
			  AND m2.status='active'
			  AND m2.cycle_start_at<=CURRENT_TIMESTAMP(3)
			  AND m2.cycle_end_at>CURRENT_TIMESTAMP(3)
			ORDER BY m2.cycle_end_at DESC, m2.id DESC
			LIMIT 1
		)
		LEFT JOIN catalog_membership_plans p ON p.id=m.plan_id
		LEFT JOIN mgmt_membership_room_limits ml ON ml.plan_id=m.plan_id
		LEFT JOIN org_resource_accounts a
		  ON a.organization_id=u.tenant_id
		 AND a.resource_type='room_slots'
		 AND a.status='active'
		LEFT JOIN (
			SELECT tenant_id, COUNT(*) AS room_count
			FROM core_rooms
			GROUP BY tenant_id
		) rc ON rc.tenant_id=u.tenant_id
		LEFT JOIN org_resource_ledger last
		  ON last.id=(
			SELECT MAX(l2.id)
			FROM org_resource_ledger l2
			WHERE l2.organization_id=u.tenant_id
			  AND l2.resource_type='room_slots'
			  AND l2.business_type='liveops_room_quota_adjust'
		  )
		LEFT JOIN mgmt_users op ON op.id=last.operator_user_id
		WHERE u.role='customer'
		  AND u.tenant_id IS NOT NULL
		ORDER BY u.id DESC
	`,
		DefaultCustomerRoomLimit,
		MaxCustomerRoomLimit,
		DefaultCustomerRoomLimit,
		DefaultCustomerRoomLimit,
		MaxCustomerRoomLimit,
		MaxCustomerRoomLimit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.LiveOpsRoomQuotaSummary, 0)
	for rows.Next() {
		var item model.LiveOpsRoomQuotaSummary
		var adjustedAt sql.NullTime
		if err := rows.Scan(
			&item.TenantID,
			&item.UserID,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.Status,
			&item.ParentOrgName,
			&item.MembershipPlanID,
			&item.MembershipName,
			&item.MembershipRoomLimit,
			&item.CurrentRoomCount,
			&item.RoomLimit,
			&item.LastReason,
			&item.LastOperatorName,
			&adjustedAt,
		); err != nil {
			return nil, err
		}
		item.RemainingSlots = item.RoomLimit - item.CurrentRoomCount
		if item.RemainingSlots < 0 {
			item.RemainingSlots = 0
		}
		if adjustedAt.Valid {
			value := adjustedAt.Time
			item.LastAdjustedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) AdjustLiveOpsRoomQuota(
	ctx context.Context,
	operatorUserID int64,
	tenantID int64,
	input model.LiveOpsRoomQuotaAdjustInput,
) (model.LiveOpsRoomQuotaSummary, error) {
	reason := strings.TrimSpace(input.Reason)
	if input.RoomLimit < 0 {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf("room limit cannot be negative")
	}
	if input.RoomLimit > MaxCustomerRoomLimit {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf("room limit exceeds platform maximum")
	}
	if reason == "" {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf("reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}
	defer tx.Rollback()

	var orgType string
	if err := tx.QueryRowContext(ctx, `
		SELECT org_type
		FROM mgmt_tenants
		WHERE id=?
		FOR UPDATE
	`, tenantID).Scan(&orgType); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}
	if orgType != "customer" {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf("target is not a customer tenant")
	}

	_, membershipName, membershipLimit, err := customerMembershipRoomPolicy(ctx, tx, tenantID)
	if err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}
	if input.RoomLimit > membershipLimit {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf(
			"room limit exceeds membership maximum: %s/%d",
			membershipName,
			membershipLimit,
		)
	}

	var roomCount int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM core_rooms
		WHERE tenant_id=?
	`, tenantID).Scan(&roomCount); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}
	if input.RoomLimit < roomCount {
		return model.LiveOpsRoomQuotaSummary{}, fmt.Errorf(
			"room limit cannot be lower than current room count",
		)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, 'room_slots', 'count', ?, 0, 'active')
	`, tenantID, DefaultCustomerRoomLimit); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}

	var before int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type='room_slots'
		FOR UPDATE
	`, tenantID).Scan(&before); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='room_slots'
	`, input.RoomLimit, tenantID); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id, resource_type, change_quantity,
			balance_before, balance_after, business_type,
			operator_user_id, reason
		)
		VALUES (?, 'room_slots', ?, ?, ?, 'liveops_room_quota_adjust', ?, ?)
	`,
		tenantID,
		input.RoomLimit-before,
		before,
		input.RoomLimit,
		operatorUserID,
		reason,
	); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}

	items, err := s.ListLiveOpsRoomQuotas(ctx)
	if err != nil {
		return model.LiveOpsRoomQuotaSummary{}, err
	}
	for _, item := range items {
		if item.TenantID == tenantID {
			return item, nil
		}
	}
	return model.LiveOpsRoomQuotaSummary{}, sql.ErrNoRows
}
