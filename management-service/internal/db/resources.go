package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrInsufficientResource = errors.New("insufficient resource")

const (
	DefaultCustomerRoomLimit int64 = 3
	MaxCustomerRoomLimit     int64 = 10
)

type resourceSpec struct {
	resourceType string
	unit         string
	agent        bool
	customer     bool
	defaultValue int64
}

var standardResourceSpecs = []resourceSpec{
	{"ai_seconds", "seconds", true, true, 0},
	{"device_slots", "count", true, false, 0},
	{"room_slots", "count", false, true, DefaultCustomerRoomLimit},
}

func resourceSpecFor(resourceType string) (resourceSpec, bool) {
	for _, spec := range standardResourceSpecs {
		if spec.resourceType == resourceType {
			return spec, true
		}
	}
	return resourceSpec{}, false
}

func creditCustomerQuotaFromResourceLedgerTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	seconds int64,
	businessType string,
	resourceLedgerID int64,
	operatorUserID int64,
	reason string,
	occurredAt time.Time,
) error {
	if seconds <= 0 {
		return fmt.Errorf("invalid quota credit")
	}

	bucketExternalID := fmt.Sprintf("resource-ledger-%d", resourceLedgerID)
	expiresAt := time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO quota_buckets (
			external_id, tenant_id, source_type, source_id,
			original_seconds, remaining_seconds, effective_at,
			expires_at, priority, status, metadata_json
		)
		VALUES (
			?, ?, ?, ?,
			?, ?, ?,
			?, 90, 'active',
			JSON_OBJECT('resource_ledger_id', ?, 'non_expiring', TRUE)
		)
	`,
		bucketExternalID,
		tenantID,
		strings.TrimSpace(businessType),
		resourceLedgerID,
		seconds,
		seconds,
		occurredAt,
		expiresAt,
		resourceLedgerID,
	)
	if err != nil {
		return err
	}
	bucketID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	ledgerExternalID := fmt.Sprintf("resource-ledger-credit-%d", resourceLedgerID)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quota_ledger (
			external_id, tenant_id, bucket_id,
			change_seconds, remaining_before_seconds,
			remaining_after_seconds, business_type, business_id,
			operator_user_id, reason, idempotency_key, occurred_at
		)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, ?)
	`,
		ledgerExternalID,
		tenantID,
		bucketID,
		seconds,
		seconds,
		strings.TrimSpace(businessType),
		resourceLedgerID,
		operatorUserID,
		strings.TrimSpace(reason),
		ledgerExternalID,
		occurredAt,
	); err != nil {
		return err
	}
	return nil
}

func debitCustomerQuotaFromResourceLedgerTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	seconds int64,
	businessType string,
	resourceLedgerID int64,
	operatorUserID int64,
	reason string,
	occurredAt time.Time,
) error {
	if seconds <= 0 {
		return fmt.Errorf("invalid quota debit")
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, remaining_seconds
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at<=?
		  AND expires_at>?
		  AND remaining_seconds>0
		ORDER BY expires_at ASC, priority ASC, id ASC
		FOR UPDATE
	`, tenantID, occurredAt, occurredAt)
	if err != nil {
		return err
	}
	type quotaBalance struct {
		id        int64
		remaining int64
	}
	buckets := make([]quotaBalance, 0)
	var available int64
	for rows.Next() {
		var item quotaBalance
		if err := rows.Scan(&item.id, &item.remaining); err != nil {
			rows.Close()
			return err
		}
		buckets = append(buckets, item)
		available += item.remaining
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if available < seconds {
		return ErrInsufficientResource
	}

	remaining := seconds
	for _, bucket := range buckets {
		if remaining <= 0 {
			break
		}
		use := bucket.remaining
		if use > remaining {
			use = remaining
		}
		after := bucket.remaining - use
		status := "active"
		if after == 0 {
			status = "exhausted"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE quota_buckets
			SET remaining_seconds=?, status=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=?
		`, after, status, bucket.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_time_card_assets
			SET remaining_seconds=?,
			    status=CASE WHEN ?=0 THEN 'exhausted' ELSE status END,
			    updated_at=CURRENT_TIMESTAMP(3)
			WHERE quota_bucket_id=? AND status='active'
		`, after, after, bucket.id); err != nil {
			return err
		}

		externalID := fmt.Sprintf(
			"resource-ledger-debit-%d-%d",
			resourceLedgerID,
			bucket.id,
		)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quota_ledger (
				external_id, tenant_id, bucket_id,
				change_seconds, remaining_before_seconds,
				remaining_after_seconds, business_type, business_id,
				operator_user_id, reason, idempotency_key, occurred_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			externalID,
			tenantID,
			bucket.id,
			-use,
			bucket.remaining,
			after,
			strings.TrimSpace(businessType),
			resourceLedgerID,
			operatorUserID,
			strings.TrimSpace(reason),
			externalID,
			occurredAt,
		); err != nil {
			return err
		}
		remaining -= use
	}
	return nil
}

func syncCustomerAIResourceAfterQuotaChargeTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	charged int64,
	roomID int64,
	occurredAt time.Time,
) error {
	if charged <= 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, 'ai_seconds', 'seconds', 0, 0, 'active')
	`, tenantID); err != nil {
		return err
	}

	var quotaAfter int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(remaining_seconds), 0)
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at<=?
		  AND expires_at>?
		  AND remaining_seconds>0
	`, tenantID, occurredAt, occurredAt).Scan(&quotaAfter); err != nil {
		return err
	}

	var storedBefore int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type='ai_seconds'
		FOR UPDATE
	`, tenantID).Scan(&storedBefore); err != nil {
		return err
	}

	quotaBefore := quotaAfter + charged
	if storedBefore != quotaBefore {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO org_resource_ledger (
				organization_id, resource_type, change_quantity,
				balance_before, balance_after, business_type,
				operator_user_id, reason, created_at
			)
			VALUES (?, 'ai_seconds', ?, ?, ?, 'quota_reconcile', NULL, ?, ?)
		`,
			tenantID,
			quotaBefore-storedBefore,
			storedBefore,
			quotaBefore,
			"直播扣时前同步真实可消费配额",
			occurredAt,
		); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='ai_seconds'
	`, quotaAfter, tenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id, resource_type, change_quantity,
			balance_before, balance_after, business_type,
			operator_user_id, reason, created_at
		)
		VALUES (?, 'ai_seconds', ?, ?, ?, 'live_ai_usage', NULL, ?, ?)
	`,
		tenantID,
		-charged,
		quotaBefore,
		quotaAfter,
		fmt.Sprintf("直播间 %d AI运行时长消费", roomID),
		occurredAt,
	); err != nil {
		return err
	}
	return nil
}

func (s *Store) MigrateResources(ctx context.Context) error {
	statements := []string{
		`
		CREATE TABLE IF NOT EXISTS org_resource_accounts (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			organization_id BIGINT UNSIGNED NOT NULL,
			resource_type VARCHAR(64) NOT NULL,
			unit VARCHAR(32) NOT NULL,
			balance BIGINT NOT NULL DEFAULT 0,
			reserved BIGINT NOT NULL DEFAULT 0,
			status VARCHAR(32) NOT NULL DEFAULT 'active',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			UNIQUE KEY uk_org_resource_accounts_org_type (organization_id, resource_type),
			KEY idx_org_resource_accounts_type (resource_type, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
		`,
		`
		CREATE TABLE IF NOT EXISTS org_resource_ledger (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			organization_id BIGINT UNSIGNED NOT NULL,
			counterparty_org_id BIGINT UNSIGNED NULL,
			resource_type VARCHAR(64) NOT NULL,
			change_quantity BIGINT NOT NULL,
			balance_before BIGINT NOT NULL,
			balance_after BIGINT NOT NULL,
			business_type VARCHAR(64) NOT NULL,
			operator_user_id BIGINT UNSIGNED NULL,
			reason VARCHAR(512) NOT NULL DEFAULT '',
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			PRIMARY KEY (id),
			KEY idx_org_resource_ledger_org (organization_id, created_at),
			KEY idx_org_resource_ledger_counterparty (counterparty_org_id, created_at),
			KEY idx_org_resource_ledger_type (resource_type, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
		`,
		`
		CREATE TABLE IF NOT EXISTS mgmt_membership_room_limits (
			plan_id BIGINT UNSIGNED NOT NULL,
			room_limit INT UNSIGNED NOT NULL DEFAULT 3,
			updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
			created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
			updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
			PRIMARY KEY (plan_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
		`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate resource accounts: %w", err)
		}
	}

	for _, spec := range standardResourceSpecs {
		orgTypes := []string{}
		if spec.agent {
			orgTypes = append(orgTypes, "agent")
		}
		if spec.customer {
			orgTypes = append(orgTypes, "customer")
		}
		for _, orgType := range orgTypes {
			if _, err := s.db.ExecContext(ctx, `
				INSERT IGNORE INTO org_resource_accounts (
					organization_id, resource_type, unit, balance, reserved, status
				)
				SELECT id, ?, ?, ?, 0, 'active'
				FROM mgmt_tenants
				WHERE org_type=?
			`, spec.resourceType, spec.unit, spec.defaultValue, orgType); err != nil {
				return fmt.Errorf(
					"backfill resource account %s/%s: %w",
					orgType,
					spec.resourceType,
					err,
				)
			}
		}
	}

	// The old development default was 5. Only untouched room-slot accounts are
	// migrated to the new platform default. Any account with an operations
	// adjustment ledger keeps its explicit value.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE org_resource_accounts a
		LEFT JOIN (
			SELECT organization_id, COUNT(*) AS adjustments
			FROM org_resource_ledger
			WHERE resource_type='room_slots'
			  AND business_type='liveops_room_quota_adjust'
			GROUP BY organization_id
		) adjusted ON adjusted.organization_id=a.organization_id
		LEFT JOIN (
			SELECT tenant_id, COUNT(*) AS room_count
			FROM core_rooms
			GROUP BY tenant_id
		) rooms ON rooms.tenant_id=a.organization_id
		SET a.balance=GREATEST(?, COALESCE(rooms.room_count, 0))
		WHERE a.resource_type='room_slots'
		  AND a.balance=5
		  AND COALESCE(adjusted.adjustments, 0)=0
	`, DefaultCustomerRoomLimit); err != nil {
		return fmt.Errorf("migrate default customer room quota: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO quota_buckets (
			external_id, tenant_id, source_type, source_id,
			original_seconds, remaining_seconds, effective_at,
			expires_at, priority, status, metadata_json
		)
		SELECT
			CONCAT('resource-ledger-', l.id),
			l.organization_id,
			l.business_type,
			l.id,
			l.change_quantity,
			l.change_quantity,
			l.created_at,
			'9999-12-31 23:59:59.000',
			90,
			'active',
			JSON_OBJECT('resource_ledger_id', l.id, 'non_expiring', TRUE)
		FROM org_resource_ledger l
		INNER JOIN mgmt_tenants t
			ON t.id=l.organization_id AND t.org_type='customer'
		WHERE l.resource_type='ai_seconds'
		  AND l.change_quantity>0
		  AND l.business_type IN ('platform_adjust', 'agent_allocate')
	`); err != nil {
		return fmt.Errorf("backfill customer quota buckets from resource ledger: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO quota_ledger (
			external_id, tenant_id, bucket_id,
			change_seconds, remaining_before_seconds,
			remaining_after_seconds, business_type, business_id,
			operator_user_id, reason, idempotency_key, occurred_at
		)
		SELECT
			CONCAT('resource-ledger-credit-', l.id),
			l.organization_id,
			q.id,
			l.change_quantity,
			0,
			l.change_quantity,
			l.business_type,
			l.id,
			l.operator_user_id,
			l.reason,
			CONCAT('resource-ledger-credit-', l.id),
			l.created_at
		FROM org_resource_ledger l
		INNER JOIN mgmt_tenants t
			ON t.id=l.organization_id AND t.org_type='customer'
		INNER JOIN quota_buckets q
			ON q.external_id=CONCAT('resource-ledger-', l.id)
		WHERE l.resource_type='ai_seconds'
		  AND l.change_quantity>0
		  AND l.business_type IN ('platform_adjust', 'agent_allocate')
	`); err != nil {
		return fmt.Errorf("backfill customer quota ledger from resource ledger: %w", err)
	}

	return nil
}

func ensureOrganizationResourceAccountsTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID int64,
	orgType string,
) error {
	for _, spec := range standardResourceSpecs {
		if (orgType == "agent" && !spec.agent) ||
			(orgType == "customer" && !spec.customer) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO org_resource_accounts (
				organization_id, resource_type, unit, balance, reserved, status
			)
			VALUES (?, ?, ?, ?, 0, 'active')
		`, orgID, spec.resourceType, spec.unit, spec.defaultValue); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetResourceDashboard(
	ctx context.Context,
	orgID int64,
) (model.ResourceDashboard, error) {
	var result model.ResourceDashboard
	result.OrganizationID = orgID

	if err := s.db.QueryRowContext(ctx, `
		SELECT name, org_type
		FROM mgmt_tenants
		WHERE id=?
		LIMIT 1
	`, orgID).Scan(&result.Organization, &result.OrgType); err != nil {
		return model.ResourceDashboard{}, err
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			organization_id,
			resource_type,
			unit,
			balance,
			reserved,
			status,
			updated_at
		FROM org_resource_accounts
		WHERE organization_id=?
		ORDER BY resource_type
	`, orgID)
	if err != nil {
		return model.ResourceDashboard{}, err
	}
	defer rows.Close()

	result.Accounts = make([]model.ResourceAccount, 0)
	for rows.Next() {
		var item model.ResourceAccount
		if err := rows.Scan(
			&item.ID,
			&item.OrganizationID,
			&item.ResourceType,
			&item.Unit,
			&item.Balance,
			&item.Reserved,
			&item.Status,
			&item.UpdatedAt,
		); err != nil {
			return model.ResourceDashboard{}, err
		}
		spec, ok := resourceSpecFor(item.ResourceType)
		if !ok ||
			(result.OrgType == "agent" && !spec.agent) ||
			(result.OrgType == "customer" && !spec.customer) {
			continue
		}
		result.Accounts = append(result.Accounts, item)
	}
	if err := rows.Err(); err != nil {
		return model.ResourceDashboard{}, err
	}
	if result.OrgType == "customer" {
		var activeAISeconds int64
		if err := s.db.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(remaining_seconds), 0)
			FROM quota_buckets
			WHERE tenant_id=?
			  AND status='active'
			  AND effective_at<=CURRENT_TIMESTAMP(3)
			  AND expires_at>CURRENT_TIMESTAMP(3)
			  AND remaining_seconds>0
		`, orgID).Scan(&activeAISeconds); err != nil {
			return model.ResourceDashboard{}, err
		}
		for index := range result.Accounts {
			if result.Accounts[index].ResourceType == "ai_seconds" {
				result.Accounts[index].Balance = activeAISeconds
			}
		}
	}

	ledgerRows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			organization_id,
			counterparty_org_id,
			resource_type,
			change_quantity,
			balance_before,
			balance_after,
			business_type,
			operator_user_id,
			reason,
			created_at
		FROM org_resource_ledger
		WHERE organization_id=?
		ORDER BY id DESC
		LIMIT 100
	`, orgID)
	if err != nil {
		return model.ResourceDashboard{}, err
	}
	defer ledgerRows.Close()

	result.Ledger = make([]model.ResourceLedgerEntry, 0)
	for ledgerRows.Next() {
		var item model.ResourceLedgerEntry
		var counterparty sql.NullInt64
		var operator sql.NullInt64
		if err := ledgerRows.Scan(
			&item.ID,
			&item.OrganizationID,
			&counterparty,
			&item.ResourceType,
			&item.ChangeQuantity,
			&item.BalanceBefore,
			&item.BalanceAfter,
			&item.BusinessType,
			&operator,
			&item.Reason,
			&item.CreatedAt,
		); err != nil {
			return model.ResourceDashboard{}, err
		}
		if counterparty.Valid {
			value := counterparty.Int64
			item.CounterpartyOrgID = &value
		}
		if operator.Valid {
			value := operator.Int64
			item.OperatorUserID = &value
		}
		result.Ledger = append(result.Ledger, item)
	}
	if err := ledgerRows.Err(); err != nil {
		return model.ResourceDashboard{}, err
	}

	return result, nil
}

func (s *Store) AdjustOrganizationResource(
	ctx context.Context,
	orgID int64,
	resourceType string,
	delta int64,
	operatorUserID int64,
	reason string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := adjustOrganizationResourceTx(
		ctx, tx, orgID, resourceType, delta, operatorUserID, reason, "platform_adjust",
	); err != nil {
		return err
	}
	return tx.Commit()
}

func adjustOrganizationResourceTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID int64,
	resourceType string,
	delta int64,
	operatorUserID int64,
	reason string,
	businessType string,
) error {
	spec, ok := resourceSpecFor(strings.TrimSpace(resourceType))
	if !ok || delta == 0 {
		return fmt.Errorf("invalid resource adjustment")
	}
	businessType = strings.TrimSpace(businessType)
	if businessType == "" {
		businessType = "platform_adjust"
	}

	var orgType string
	if err := tx.QueryRowContext(ctx, `
		SELECT org_type
		FROM mgmt_tenants
		WHERE id=? AND status='active'
	`, orgID).Scan(&orgType); err != nil {
		return err
	}
	if orgType != "agent" && orgType != "customer" {
		return sql.ErrNoRows
	}
	if orgType == "agent" && !spec.agent {
		return fmt.Errorf("resource not supported by agent")
	}
	if orgType == "customer" && !spec.customer {
		return fmt.Errorf("resource not supported by customer")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, ?, ?, 0, 0, 'active')
	`, orgID, spec.resourceType, spec.unit); err != nil {
		return err
	}

	var before int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type=?
		FOR UPDATE
	`, orgID, spec.resourceType).Scan(&before); err != nil {
		return err
	}

	after := before + delta
	if after < 0 {
		return ErrInsufficientResource
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type=?
	`, after, orgID, spec.resourceType); err != nil {
		return err
	}

	ledgerResult, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id,
			resource_type,
			change_quantity,
			balance_before,
			balance_after,
			business_type,
			operator_user_id,
			reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orgID,
		spec.resourceType,
		delta,
		before,
		after,
		businessType,
		operatorUserID,
		strings.TrimSpace(reason),
	)
	if err != nil {
		return err
	}
	resourceLedgerID, err := ledgerResult.LastInsertId()
	if err != nil {
		return err
	}

	if orgType == "customer" && spec.resourceType == "ai_seconds" {
		now := time.Now().UTC()
		if delta > 0 {
			if err := creditCustomerQuotaFromResourceLedgerTx(
				ctx,
				tx,
				orgID,
				delta,
				businessType,
				resourceLedgerID,
				operatorUserID,
				reason,
				now,
			); err != nil {
				return err
			}
		} else {
			if err := debitCustomerQuotaFromResourceLedgerTx(
				ctx,
				tx,
				orgID,
				-delta,
				businessType,
				resourceLedgerID,
				operatorUserID,
				reason,
				now,
			); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Store) TransferOrganizationResource(
	ctx context.Context,
	fromOrgID int64,
	toOrgID int64,
	resourceType string,
	quantity int64,
	operatorUserID int64,
	reason string,
) error {
	spec, ok := resourceSpecFor(strings.TrimSpace(resourceType))
	if !ok || quantity <= 0 || !spec.customer || resourceType == "customer_slots" {
		return fmt.Errorf("invalid resource transfer")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var toParentID sql.NullInt64
	var fromType string
	var toType string
	if err := tx.QueryRowContext(ctx, `
		SELECT org_type
		FROM mgmt_tenants
		WHERE id=? AND status='active'
	`, fromOrgID).Scan(&fromType); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT parent_id, org_type
		FROM mgmt_tenants
		WHERE id=? AND status='active'
	`, toOrgID).Scan(&toParentID, &toType); err != nil {
		return err
	}
	if fromType != "agent" ||
		toType != "customer" ||
		!toParentID.Valid ||
		toParentID.Int64 != fromOrgID {
		return sql.ErrNoRows
	}

	for _, orgID := range []int64{fromOrgID, toOrgID} {
		if _, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO org_resource_accounts (
				organization_id, resource_type, unit, balance, reserved, status
			)
			VALUES (?, ?, ?, 0, 0, 'active')
		`, orgID, spec.resourceType, spec.unit); err != nil {
			return err
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT organization_id, balance
		FROM org_resource_accounts
		WHERE resource_type=?
		  AND organization_id IN (?, ?)
		ORDER BY organization_id
		FOR UPDATE
	`, spec.resourceType, fromOrgID, toOrgID)
	if err != nil {
		return err
	}

	balances := map[int64]int64{}
	for rows.Next() {
		var orgID int64
		var balance int64
		if err := rows.Scan(&orgID, &balance); err != nil {
			rows.Close()
			return err
		}
		balances[orgID] = balance
	}
	if err := rows.Close(); err != nil {
		return err
	}

	fromBefore := balances[fromOrgID]
	toBefore := balances[toOrgID]
	if fromBefore < quantity {
		return ErrInsufficientResource
	}
	fromAfter := fromBefore - quantity
	toAfter := toBefore + quantity

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type=?
	`, fromAfter, fromOrgID, spec.resourceType); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type=?
	`, toAfter, toOrgID, spec.resourceType); err != nil {
		return err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id,
			counterparty_org_id,
			resource_type,
			change_quantity,
			balance_before,
			balance_after,
			business_type,
			operator_user_id,
			reason,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, 'agent_allocate', ?, ?, ?)
	`,
		fromOrgID,
		toOrgID,
		spec.resourceType,
		-quantity,
		fromBefore,
		fromAfter,
		operatorUserID,
		strings.TrimSpace(reason),
		now,
	); err != nil {
		return err
	}

	targetLedgerResult, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id,
			counterparty_org_id,
			resource_type,
			change_quantity,
			balance_before,
			balance_after,
			business_type,
			operator_user_id,
			reason,
			created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, 'agent_allocate', ?, ?, ?)
	`,
		toOrgID,
		fromOrgID,
		spec.resourceType,
		quantity,
		toBefore,
		toAfter,
		operatorUserID,
		strings.TrimSpace(reason),
		now,
	)
	if err != nil {
		return err
	}
	targetResourceLedgerID, err := targetLedgerResult.LastInsertId()
	if err != nil {
		return err
	}

	if spec.resourceType == "ai_seconds" {
		if err := creditCustomerQuotaFromResourceLedgerTx(
			ctx,
			tx,
			toOrgID,
			quantity,
			"agent_allocate",
			targetResourceLedgerID,
			operatorUserID,
			reason,
			now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}
func (s *Store) IsDirectCustomerOfAgent(
	ctx context.Context,
	customerOrgID int64,
	agentOrgID int64,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mgmt_tenants
		WHERE id=?
		  AND org_type='customer'
		  AND parent_id=?
		  AND status='active'
	`, customerOrgID, agentOrgID).Scan(&count)
	return count > 0, err
}
func consumeOrganizationResourceTx(
	ctx context.Context,
	tx *sql.Tx,
	orgID int64,
	counterpartyOrgID *int64,
	resourceType string,
	quantity int64,
	operatorUserID *int64,
	businessType string,
	reason string,
) error {
	if quantity <= 0 {
		return fmt.Errorf("invalid resource quantity")
	}
	spec, ok := resourceSpecFor(resourceType)
	if !ok {
		return fmt.Errorf("invalid resource type")
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, ?, ?, 0, 0, 'active')
	`, orgID, spec.resourceType, spec.unit); err != nil {
		return err
	}

	var before int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=?
		  AND resource_type=?
		  AND status='active'
		FOR UPDATE
	`, orgID, spec.resourceType).Scan(&before); err != nil {
		return err
	}

	if before < quantity {
		return ErrInsufficientResource
	}
	after := before - quantity

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=?
		  AND resource_type=?
	`, after, orgID, spec.resourceType); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id,
			counterparty_org_id,
			resource_type,
			change_quantity,
			balance_before,
			balance_after,
			business_type,
			operator_user_id,
			reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orgID,
		counterpartyOrgID,
		spec.resourceType,
		-quantity,
		before,
		after,
		strings.TrimSpace(businessType),
		operatorUserID,
		strings.TrimSpace(reason),
	); err != nil {
		return err
	}

	return nil
}
func (s *Store) tableExists(
	ctx context.Context,
	tableName string,
) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA=DATABASE()
		  AND TABLE_NAME=?
	`, tableName).Scan(&count)
	return count > 0, err
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func customerMembershipRoomPolicy(
	ctx context.Context,
	query rowQuerier,
	tenantID int64,
) (int64, string, int64, error) {
	var planID int64
	var planName string
	var roomLimit int64
	err := query.QueryRowContext(ctx, `
		SELECT
			m.plan_id,
			p.name,
			LEAST(COALESCE(l.room_limit, ?), ?)
		FROM biz_memberships m
		INNER JOIN catalog_membership_plans p ON p.id=m.plan_id
		LEFT JOIN mgmt_membership_room_limits l ON l.plan_id=m.plan_id
		WHERE m.tenant_id=?
		  AND m.status='active'
		  AND m.cycle_start_at<=CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at>CURRENT_TIMESTAMP(3)
		ORDER BY m.cycle_end_at DESC, m.id DESC
		LIMIT 1
	`, DefaultCustomerRoomLimit, MaxCustomerRoomLimit, tenantID).Scan(
		&planID,
		&planName,
		&roomLimit,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "无会员", DefaultCustomerRoomLimit, nil
	}
	if err != nil {
		return 0, "", 0, err
	}
	if roomLimit < 1 {
		roomLimit = DefaultCustomerRoomLimit
	}
	if roomLimit > MaxCustomerRoomLimit {
		roomLimit = MaxCustomerRoomLimit
	}
	return planID, planName, roomLimit, nil
}

func effectiveCustomerRoomLimit(configured, membershipLimit int64) int64 {
	if membershipLimit < 1 {
		membershipLimit = DefaultCustomerRoomLimit
	}
	if membershipLimit > MaxCustomerRoomLimit {
		membershipLimit = MaxCustomerRoomLimit
	}
	if configured < 0 {
		configured = 0
	}
	if configured > MaxCustomerRoomLimit {
		configured = MaxCustomerRoomLimit
	}
	if configured > membershipLimit {
		configured = membershipLimit
	}
	return configured
}

func (s *Store) GetEffectiveCustomerRoomLimit(
	ctx context.Context,
	tenantID int64,
) (int64, error) {
	configured, err := s.GetOrganizationResourceBalance(ctx, tenantID, "room_slots")
	if errors.Is(err, sql.ErrNoRows) {
		configured = DefaultCustomerRoomLimit
	} else if err != nil {
		return 0, err
	}
	_, _, membershipLimit, err := customerMembershipRoomPolicy(ctx, s.db, tenantID)
	if err != nil {
		return 0, err
	}
	return effectiveCustomerRoomLimit(configured, membershipLimit), nil
}

func (s *Store) ListMembershipRoomLimits(
	ctx context.Context,
) ([]model.MembershipRoomLimitSetting, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			p.id,
			p.code,
			p.name,
			p.status,
			LEAST(COALESCE(l.room_limit, ?), ?)
		FROM catalog_membership_plans p
		LEFT JOIN mgmt_membership_room_limits l ON l.plan_id=p.id
		ORDER BY p.sort_order ASC, p.id ASC
	`, DefaultCustomerRoomLimit, MaxCustomerRoomLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.MembershipRoomLimitSetting, 0)
	for rows.Next() {
		var item model.MembershipRoomLimitSetting
		if err := rows.Scan(
			&item.PlanID,
			&item.PlanCode,
			&item.PlanName,
			&item.PlanStatus,
			&item.RoomLimit,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpdateMembershipRoomLimits(
	ctx context.Context,
	updates []model.MembershipRoomLimitUpdate,
	actorUserID int64,
) error {
	if len(updates) == 0 {
		return fmt.Errorf("membership room limit updates are empty")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	seen := make(map[int64]struct{}, len(updates))
	for _, update := range updates {
		if update.PlanID <= 0 || update.RoomLimit < 1 || update.RoomLimit > MaxCustomerRoomLimit {
			return fmt.Errorf("invalid membership room limit")
		}
		if _, ok := seen[update.PlanID]; ok {
			return fmt.Errorf("duplicate membership plan")
		}
		seen[update.PlanID] = struct{}{}

		var exists int
		if err := tx.QueryRowContext(ctx, `
			SELECT 1 FROM catalog_membership_plans WHERE id=? LIMIT 1
		`, update.PlanID).Scan(&exists); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mgmt_membership_room_limits (
				plan_id, room_limit, updated_by_user_id
			)
			VALUES (?, ?, ?)
			ON DUPLICATE KEY UPDATE
				room_limit=VALUES(room_limit),
				updated_by_user_id=VALUES(updated_by_user_id)
		`, update.PlanID, update.RoomLimit, actorUserID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetOrganizationResourceBalance(
	ctx context.Context,
	orgID int64,
	resourceType string,
) (int64, error) {
	var balance int64
	err := s.db.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=?
		  AND resource_type=?
		  AND status='active'
		LIMIT 1
	`, orgID, strings.TrimSpace(resourceType)).Scan(&balance)
	return balance, err
}

func (s *Store) GetTenantRoomCount(
	ctx context.Context,
	tenantID int64,
) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM core_rooms
		WHERE tenant_id=?
	`, tenantID).Scan(&count)
	return count, err
}
