package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

func scanCustomerCooperation(
	tenantID int64,
	status string,
	note string,
	markedAt sql.NullTime,
	markedBy sql.NullInt64,
	lastRecharge sql.NullTime,
	dormant bool,
) model.CustomerCooperationInfo {
	item := model.CustomerCooperationInfo{
		TenantID:              tenantID,
		Status:                strings.TrimSpace(status),
		Note:                  strings.TrimSpace(note),
		MarkedByUserID:        markedBy.Int64,
		RechargeDormant90Days: dormant,
	}
	if item.Status == "" {
		item.Status = model.CustomerCooperationStatusCooperating
	}
	if markedAt.Valid {
		value := markedAt.Time
		item.MarkedAt = &value
	}
	if lastRecharge.Valid {
		value := lastRecharge.Time
		item.LastRechargeAt = &value
	}
	return item
}

func customerCooperationSelectSQL() string {
	return `
		SELECT
			t.id,
			COALESCE(p.cooperation_status, 'cooperating'),
			COALESCE(p.cooperation_note, ''),
			p.cooperation_marked_at,
			p.cooperation_marked_by_user_id,
			recharge.last_recharge_at,
			CASE
				WHEN COALESCE(recharge.last_recharge_at, u.created_at) <= DATE_SUB(UTC_TIMESTAMP(3), INTERVAL 90 DAY)
				THEN 1 ELSE 0
			END AS recharge_dormant_90_days
		FROM mgmt_tenants t
		LEFT JOIN crm_customer_profiles p ON p.tenant_id=t.id
		LEFT JOIN mgmt_users u ON u.tenant_id=t.id AND u.role='customer'
		LEFT JOIN (
			SELECT tenant_id, MAX(paid_at) AS last_recharge_at
			FROM fin_recharge_orders
			WHERE status='paid' AND paid_at IS NOT NULL
			GROUP BY tenant_id
		) recharge ON recharge.tenant_id=t.id
	`
}

func (s *Store) GetCustomerCooperationInfo(
	ctx context.Context,
	tenantID int64,
) (model.CustomerCooperationInfo, error) {
	var (
		id           int64
		status       string
		note         string
		markedAt     sql.NullTime
		markedBy     sql.NullInt64
		lastRecharge sql.NullTime
		dormant      bool
	)
	err := s.db.QueryRowContext(
		ctx,
		customerCooperationSelectSQL()+" WHERE t.id=? AND t.org_type='customer' LIMIT 1",
		tenantID,
	).Scan(
		&id,
		&status,
		&note,
		&markedAt,
		&markedBy,
		&lastRecharge,
		&dormant,
	)
	if err != nil {
		return model.CustomerCooperationInfo{}, err
	}
	return scanCustomerCooperation(
		id,
		status,
		note,
		markedAt,
		markedBy,
		lastRecharge,
		dormant,
	), nil
}

func (s *Store) GetCustomerCooperationByTenantIDs(
	ctx context.Context,
	tenantIDs []int64,
) (map[int64]model.CustomerCooperationInfo, error) {
	result := make(map[int64]model.CustomerCooperationInfo)
	seen := make(map[int64]struct{})
	unique := make([]int64, 0, len(tenantIDs))
	for _, tenantID := range tenantIDs {
		if tenantID <= 0 {
			continue
		}
		if _, ok := seen[tenantID]; ok {
			continue
		}
		seen[tenantID] = struct{}{}
		unique = append(unique, tenantID)
	}
	if len(unique) == 0 {
		return result, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for index, tenantID := range unique {
		placeholders[index] = "?"
		args[index] = tenantID
	}
	rows, err := s.db.QueryContext(
		ctx,
		customerCooperationSelectSQL()+
			" WHERE t.org_type='customer' AND t.id IN ("+
			strings.Join(placeholders, ",")+
			")",
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id           int64
			status       string
			note         string
			markedAt     sql.NullTime
			markedBy     sql.NullInt64
			lastRecharge sql.NullTime
			dormant      bool
		)
		if err := rows.Scan(
			&id,
			&status,
			&note,
			&markedAt,
			&markedBy,
			&lastRecharge,
			&dormant,
		); err != nil {
			return nil, err
		}
		result[id] = scanCustomerCooperation(
			id,
			status,
			note,
			markedAt,
			markedBy,
			lastRecharge,
			dormant,
		)
	}
	return result, rows.Err()
}

func (s *Store) GetCustomerUserIDByTenantID(
	ctx context.Context,
	tenantID int64,
) (int64, error) {
	var userID int64
	err := s.db.QueryRowContext(
		ctx,
		"SELECT id FROM mgmt_users WHERE tenant_id=? AND role='customer' LIMIT 1",
		tenantID,
	).Scan(&userID)
	return userID, err
}

func (s *Store) SetCustomerCooperationStatus(
	ctx context.Context,
	tenantID int64,
	status string,
	note string,
	actorUserID int64,
) (model.CustomerCooperationInfo, error) {
	status = strings.TrimSpace(status)
	note = strings.TrimSpace(note)
	if status != model.CustomerCooperationStatusCooperating &&
		status != model.CustomerCooperationStatusNonCooperating {
		return model.CustomerCooperationInfo{}, fmt.Errorf("invalid cooperation status")
	}

	var exists int
	if err := s.db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM mgmt_tenants WHERE id=? AND org_type='customer'",
		tenantID,
	).Scan(&exists); err != nil {
		return model.CustomerCooperationInfo{}, err
	}
	if exists == 0 {
		return model.CustomerCooperationInfo{}, sql.ErrNoRows
	}

	markedAt := time.Now().UTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO crm_customer_profiles (
			tenant_id,
			source_type,
			cooperation_status,
			cooperation_note,
			cooperation_marked_at,
			cooperation_marked_by_user_id
		)
		VALUES (?, 'unknown', ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			cooperation_status=VALUES(cooperation_status),
			cooperation_note=VALUES(cooperation_note),
			cooperation_marked_at=VALUES(cooperation_marked_at),
			cooperation_marked_by_user_id=VALUES(cooperation_marked_by_user_id)
	`,
		tenantID,
		status,
		note,
		markedAt,
		actorUserID,
	); err != nil {
		return model.CustomerCooperationInfo{}, err
	}
	return s.GetCustomerCooperationInfo(ctx, tenantID)
}
