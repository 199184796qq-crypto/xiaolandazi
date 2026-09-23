package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

var ErrAgentExitBlocked = errors.New("agent exit is blocked")

type agentExitQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) ListAgentExitHistory(
	ctx context.Context,
	organizationID int64,
) ([]model.AgentExitRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			r.id, r.record_key, r.organization_id, r.agent_code, r.agent_name,
			r.status, r.transferred_customer_count, r.active_device_count,
			r.open_rma_count, r.unsettled_earning_count,
			r.unsettled_earning_amount_cents, r.open_settlement_batch_count,
			r.nonzero_resource_account_count, r.finalized_by_user_id,
			COALESCE(u.display_name, u.username, ''), r.finalized_at, r.created_at
		FROM crm_agent_exit_records r
		LEFT JOIN mgmt_users u ON u.id=r.finalized_by_user_id
		WHERE r.organization_id=?
		ORDER BY r.finalized_at DESC, r.id DESC
	`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AgentExitRecord, 0)
	for rows.Next() {
		var item model.AgentExitRecord
		if err := rows.Scan(
			&item.ID, &item.RecordKey, &item.OrganizationID, &item.AgentCode,
			&item.AgentName, &item.Status, &item.TransferredCustomerCount,
			&item.ActiveDeviceCount, &item.OpenRMACount, &item.UnsettledEarningCount,
			&item.UnsettledEarningAmountCents, &item.OpenSettlementBatchCount,
			&item.NonzeroResourceAccountCount, &item.FinalizedByUserID,
			&item.FinalizedByName, &item.FinalizedAt, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) GetAgentExitCheck(
	ctx context.Context,
	organizationID int64,
) (model.AgentExitCheck, error) {
	return getAgentExitCheck(ctx, s.db, organizationID)
}

func getAgentExitCheck(
	ctx context.Context,
	q agentExitQueryer,
	organizationID int64,
) (model.AgentExitCheck, error) {
	result := model.AgentExitCheck{
		OrganizationID: organizationID,
		Blockers:       make([]string, 0),
	}

	var orgStatus string
	if err := q.QueryRowContext(ctx, `
		SELECT status
		FROM mgmt_tenants
		WHERE id=? AND org_type='agent'
		LIMIT 1
	`, organizationID).Scan(&orgStatus); err != nil {
		return model.AgentExitCheck{}, err
	}
	if orgStatus != "active" {
		result.Blockers = append(result.Blockers, "代理组织当前不是正常经营状态")
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_devices
		WHERE owner_org_id=?
		  AND lifecycle_status <> 'SCRAPPED'
	`, organizationID).Scan(&result.ActiveDeviceCount); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent devices: %w", err)
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_rmas r
		INNER JOIN inv_devices d ON d.id=r.device_id
		WHERE d.owner_org_id=?
		  AND r.status NOT IN ('COMPLETED','CANCELLED')
	`, organizationID).Scan(&result.OpenRMACount); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent rmas: %w", err)
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(amount_cents),0)
		FROM inc_earnings
		WHERE beneficiary_type='agent'
		  AND beneficiary_id=?
		  AND status IN ('pending','available','settling')
	`, organizationID).Scan(
		&result.UnsettledEarningCount,
		&result.UnsettledEarningAmountCents,
	); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent earnings: %w", err)
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inc_settlement_batches
		WHERE beneficiary_type='agent'
		  AND beneficiary_id=?
		  AND status IN ('reviewing','approved')
	`, organizationID).Scan(&result.OpenSettlementBatchCount); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent settlement batches: %w", err)
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM org_resource_accounts
		WHERE organization_id=?
		  AND status='active'
		  AND (balance <> 0 OR reserved <> 0)
	`, organizationID).Scan(&result.NonzeroResourceAccountCount); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent resource balances: %w", err)
	}

	if err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mgmt_tenants
		WHERE parent_id=?
		  AND org_type='customer'
		  AND status='active'
	`, organizationID).Scan(&result.ActiveCustomerCount); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("count agent customers: %w", err)
	}

	if result.ActiveDeviceCount > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("仍有 %d 台未完成退出处置的设备", result.ActiveDeviceCount))
	}
	if result.OpenRMACount > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("仍有 %d 个未完成售后单", result.OpenRMACount))
	}
	if result.UnsettledEarningCount > 0 {
		result.Blockers = append(
			result.Blockers,
			fmt.Sprintf(
				"仍有 %d 笔未结收益，金额 %.2f 元",
				result.UnsettledEarningCount,
				float64(result.UnsettledEarningAmountCents)/100,
			),
		)
	}
	if result.OpenSettlementBatchCount > 0 {
		result.Blockers = append(result.Blockers, fmt.Sprintf("仍有 %d 个未完成结算批次", result.OpenSettlementBatchCount))
	}
	if result.NonzeroResourceAccountCount > 0 {
		result.Blockers = append(
			result.Blockers,
			fmt.Sprintf(
				"仍有 %d 个资源账户存在余额或预留量，请先完成资源清算",
				result.NonzeroResourceAccountCount,
			),
		)
	}

	result.CanExit = len(result.Blockers) == 0
	return result, nil
}

func (s *Store) FinalizeAgentExit(
	ctx context.Context,
	organizationID int64,
	operatorUserID int64,
) (model.AgentExitCheck, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentExitCheck{}, err
	}
	defer tx.Rollback()

	var agentCode string
	var agentName string
	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT code, name, status
		FROM mgmt_tenants
		WHERE id=? AND org_type='agent'
		FOR UPDATE
	`, organizationID).Scan(&agentCode, &agentName, &status); err != nil {
		return model.AgentExitCheck{}, err
	}
	if status != "active" {
		return model.AgentExitCheck{}, fmt.Errorf("agent is not active")
	}

	check, err := getAgentExitCheck(ctx, tx, organizationID)
	if err != nil {
		return model.AgentExitCheck{}, err
	}
	if !check.CanExit {
		return check, ErrAgentExitBlocked
	}

	var platformID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM mgmt_tenants
		WHERE org_type='platform' AND status='active'
		ORDER BY id
		LIMIT 1
	`).Scan(&platformID); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("load platform organization: %w", err)
	}

	customerResult, err := tx.ExecContext(ctx, `
		UPDATE mgmt_tenants
		SET parent_id=?
		WHERE parent_id=?
		  AND org_type='customer'
		  AND status='active'
	`, platformID, organizationID)
	if err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("return agent customers to platform: %w", err)
	}
	transferredCustomers, _ := customerResult.RowsAffected()

	var commercialAgentID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM crm_agent_orgs
		WHERE mgmt_tenant_id=?
		LIMIT 1
	`, organizationID).Scan(&commercialAgentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.AgentExitCheck{}, fmt.Errorf("load commercial agent organization: %w", err)
	}

	if commercialAgentID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_customer_agent_relations
			SET
				status='inactive',
				effective_to=UTC_TIMESTAMP(3),
				reason=CASE
					WHEN reason='' THEN 'agent exit: service relation returned to platform'
					ELSE CONCAT(reason, ' | agent exit: service relation returned to platform')
				END
			WHERE agent_org_id=?
			  AND status='active'
		`, commercialAgentID.Int64); err != nil {
			return model.AgentExitCheck{}, fmt.Errorf("close customer agent relations: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_agent_members
			SET status='disabled'
			WHERE agent_org_id=?
			  AND status='active'
		`, commercialAgentID.Int64); err != nil {
			return model.AgentExitCheck{}, fmt.Errorf("disable agent members: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE crm_agent_orgs
			SET status='disabled'
			WHERE id=?
		`, commercialAgentID.Int64); err != nil {
			return model.AgentExitCheck{}, fmt.Errorf("disable commercial agent organization: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE iam_invite_codes c
		INNER JOIN mgmt_users u ON u.id=c.owner_user_id
		SET c.status='disabled'
		WHERE u.tenant_id=?
		  AND u.role='agent_admin'
		  AND c.status='active'
	`, organizationID); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("disable agent invite codes: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE mgmt_users
		SET status='disabled'
		WHERE tenant_id=?
		  AND role='agent_admin'
		  AND status='active'
	`, organizationID); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("disable agent administrators: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE mgmt_tenants
		SET status='disabled'
		WHERE id=? AND org_type='agent' AND status='active'
	`, organizationID); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("disable agent organization: %w", err)
	}

	now := time.Now().UTC()
	recordKey := fmt.Sprintf("exit-%d-%d", organizationID, now.UnixMilli())
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO crm_agent_exit_records (
			record_key, organization_id, agent_code, agent_name, status,
			transferred_customer_count, active_device_count, open_rma_count,
			unsettled_earning_count, unsettled_earning_amount_cents,
			open_settlement_batch_count, nonzero_resource_account_count,
			finalized_by_user_id, finalized_at
		)
		VALUES (?, ?, ?, ?, 'completed', ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		recordKey,
		organizationID,
		agentCode,
		agentName,
		transferredCustomers,
		check.ActiveDeviceCount,
		check.OpenRMACount,
		check.UnsettledEarningCount,
		check.UnsettledEarningAmountCents,
		check.OpenSettlementBatchCount,
		check.NonzeroResourceAccountCount,
		operatorUserID,
		now,
	); err != nil {
		return model.AgentExitCheck{}, fmt.Errorf("create agent exit history: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.AgentExitCheck{}, err
	}

	check.ActiveCustomerCount = transferredCustomers
	return check, nil
}
