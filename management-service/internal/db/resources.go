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

type resourceSpec struct {
	resourceType string
	unit         string
	agent        bool
	customer     bool
}

var standardResourceSpecs = []resourceSpec{
	{"ai_seconds", "seconds", true, true},
	{"device_slots", "count", true, false},
}

func resourceSpecFor(resourceType string) (resourceSpec, bool) {
	for _, spec := range standardResourceSpecs {
		if spec.resourceType == resourceType {
			return spec, true
		}
	}
	return resourceSpec{}, false
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
				SELECT id, ?, ?, 0, 0, 'active'
				FROM mgmt_tenants
				WHERE org_type=?
			`, spec.resourceType, spec.unit, orgType); err != nil {
				return fmt.Errorf(
					"backfill resource account %s/%s: %w",
					orgType,
					spec.resourceType,
					err,
				)
			}
		}
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
			VALUES (?, ?, ?, 0, 0, 'active')
		`, orgID, spec.resourceType, spec.unit); err != nil {
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
	spec, ok := resourceSpecFor(strings.TrimSpace(resourceType))
	if !ok || delta == 0 {
		return fmt.Errorf("invalid resource adjustment")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

	if _, err := tx.ExecContext(ctx, `
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
		VALUES (?, ?, ?, ?, ?, 'platform_adjust', ?, ?)
	`,
		orgID,
		spec.resourceType,
		delta,
		before,
		after,
		operatorUserID,
		strings.TrimSpace(reason),
	); err != nil {
		return err
	}

	return tx.Commit()
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
		toOrgID,
		fromOrgID,
		spec.resourceType,
		quantity,
		toBefore,
		toAfter,
		operatorUserID,
		strings.TrimSpace(reason),
		now,
	); err != nil {
		return err
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
