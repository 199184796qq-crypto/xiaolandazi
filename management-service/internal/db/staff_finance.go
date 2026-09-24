package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type financeApprovalPolicy struct {
	ID               int64
	Mode             string
	ThresholdAmount  float64
	ApproverRoleCode string
}

type financeTaskPayload struct {
	AmountCents     uint64 `json:"amount_cents"`
	OrderID         int64  `json:"order_id,omitempty"`
	SourceOrderID   int64  `json:"source_order_id,omitempty"`
	RMAID           int64  `json:"rma_id,omitempty"`
	PaymentMethod   string `json:"payment_method,omitempty"`
	ResourceType    string `json:"resource_type,omitempty"`
	ResourceSeconds int64  `json:"resource_seconds,omitempty"`
	TargetOrgType   string `json:"target_org_type,omitempty"`
	Reason          string `json:"reason"`
}

func (s *Store) ListStaffFinanceCustomers(
	ctx context.Context,
) ([]model.StaffFinanceCustomerSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.id,
			u.id,
			u.username,
			u.display_name,
			u.phone,
			COALESCE(parent.name, ''),
			COALESCE(parent.org_type, ''),
			COALESCE((
				SELECT SUM(w.balance_cents)
				FROM fin_wallet_accounts w
				WHERE w.tenant_id=t.id
				  AND w.account_type='cash'
				  AND w.status='active'
			), 0),
			COALESCE((
				SELECT SUM(w.balance_cents)
				FROM fin_wallet_accounts w
				WHERE w.tenant_id=t.id
				  AND w.account_type='reward'
				  AND w.status='active'
			), 0)
		FROM mgmt_tenants t
		INNER JOIN mgmt_users u
			ON u.id=(
				SELECT MIN(u2.id)
				FROM mgmt_users u2
				WHERE u2.tenant_id=t.id
				  AND u2.role='customer'
			)
		LEFT JOIN mgmt_tenants parent ON parent.id=t.parent_id
		WHERE t.org_type='customer'
		  AND t.status='active'
		ORDER BY t.id DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffFinanceCustomerSummary, 0)
	for rows.Next() {
		var item model.StaffFinanceCustomerSummary
		if err := rows.Scan(
			&item.TenantID,
			&item.UserID,
			&item.Username,
			&item.DisplayName,
			&item.Phone,
			&item.ParentOrgName,
			&item.ParentOrgType,
			&item.CashBalanceCents,
			&item.RewardBalanceCents,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListStaffFinanceTasks(
	ctx context.Context,
	limit int,
) ([]model.StaffFinanceTaskSummary, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			t.id,
			t.policy_id,
			t.operation_code,
			t.requester_user_id,
			COALESCE(requester.display_name, requester.username, ''),
			t.approver_user_id,
			COALESCE(approver.display_name, approver.username, ''),
			COALESCE(t.target_id, 0),
			COALESCE(customer.name, ''),
			COALESCE(t.target_type, ''),
			t.amount,
			t.status,
			COALESCE(CAST(t.payload_json AS CHAR), '{}'),
			COALESCE(p.approver_role_code, ''),
			t.created_at,
			t.decided_at
		FROM staff_approval_tasks t
		LEFT JOIN staff_approval_policies p ON p.id=t.policy_id
		LEFT JOIN mgmt_users requester ON requester.id=t.requester_user_id
		LEFT JOIN mgmt_users approver ON approver.id=t.approver_user_id
		LEFT JOIN mgmt_tenants customer ON customer.id=t.target_id
		WHERE t.operation_code IN (
			'finance.recharge',
			'finance.refund',
			'finance.reward',
			'finance.ai_time_grant'
		)
		ORDER BY (t.status='pending') DESC, t.created_at DESC, t.id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.StaffFinanceTaskSummary, 0)
	for rows.Next() {
		item, err := scanStaffFinanceTask(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type financeTaskScanner interface {
	Scan(dest ...any) error
}

func scanStaffFinanceTask(
	scanner financeTaskScanner,
) (model.StaffFinanceTaskSummary, error) {
	var item model.StaffFinanceTaskSummary
	var policyID sql.NullInt64
	var approverUserID sql.NullInt64
	var decidedAt sql.NullTime
	var payloadRaw string

	if err := scanner.Scan(
		&item.ID,
		&policyID,
		&item.OperationCode,
		&item.RequesterUserID,
		&item.RequesterName,
		&approverUserID,
		&item.ApproverName,
		&item.TenantID,
		&item.CustomerName,
		&item.TargetType,
		&item.AmountYuan,
		&item.Status,
		&payloadRaw,
		&item.ApproverRoleCode,
		&item.CreatedAt,
		&decidedAt,
	); err != nil {
		return model.StaffFinanceTaskSummary{}, err
	}

	if policyID.Valid {
		value := policyID.Int64
		item.PolicyID = &value
	}
	if approverUserID.Valid {
		value := approverUserID.Int64
		item.ApproverUserID = &value
	}
	if decidedAt.Valid {
		value := decidedAt.Time
		item.DecidedAt = &value
	}

	var payload financeTaskPayload
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err == nil {
		item.Reason = payload.Reason
		item.ResourceSeconds = payload.ResourceSeconds
	}

	return item, nil
}

func (s *Store) GetStaffFinanceTask(
	ctx context.Context,
	taskID int64,
) (model.StaffFinanceTaskSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			t.id,
			t.policy_id,
			t.operation_code,
			t.requester_user_id,
			COALESCE(requester.display_name, requester.username, ''),
			t.approver_user_id,
			COALESCE(approver.display_name, approver.username, ''),
			COALESCE(t.target_id, 0),
			COALESCE(customer.name, ''),
			COALESCE(t.target_type, ''),
			t.amount,
			t.status,
			COALESCE(CAST(t.payload_json AS CHAR), '{}'),
			COALESCE(p.approver_role_code, ''),
			t.created_at,
			t.decided_at
		FROM staff_approval_tasks t
		LEFT JOIN staff_approval_policies p ON p.id=t.policy_id
		LEFT JOIN mgmt_users requester ON requester.id=t.requester_user_id
		LEFT JOIN mgmt_users approver ON approver.id=t.approver_user_id
		LEFT JOIN mgmt_tenants customer ON customer.id=t.target_id
		WHERE t.id=?
		LIMIT 1
	`, taskID)
	return scanStaffFinanceTask(row)
}

func (s *Store) CreateStaffFinanceRecharge(
	ctx context.Context,
	tenantID int64,
	amountCents uint64,
	paymentMethod string,
	reason string,
	requesterUserID int64,
) (model.StaffFinanceOperationResult, error) {
	if amountCents == 0 {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("amount must be positive")
	}
	paymentMethod = strings.TrimSpace(paymentMethod)
	if paymentMethod == "" {
		paymentMethod = "manual"
	}
	reason = strings.TrimSpace(reason)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	defer tx.Rollback()

	if err := ensureFinanceCustomerTx(ctx, tx, tenantID); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	policy, requiresApproval, err := loadFinancePolicyTx(
		ctx,
		tx,
		"finance.recharge",
		amountCents,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	rechargeNo, err := newFinanceReference("RCG")
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	status := "pending"
	if requiresApproval {
		status = "pending_approval"
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_recharge_orders (
			recharge_no,
			tenant_id,
			currency,
			requested_amount_cents,
			credited_amount_cents,
			payment_method,
			status,
			operator_user_id,
			idempotency_key
		)
		VALUES (?, ?, 'CNY', ?, 0, ?, ?, ?, ?)
	`,
		rechargeNo,
		tenantID,
		amountCents,
		paymentMethod,
		status,
		requesterUserID,
		rechargeNo,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	payload := financeTaskPayload{
		AmountCents:   amountCents,
		OrderID:       orderID,
		PaymentMethod: paymentMethod,
		Reason:        reason,
	}
	taskID, err := createFinanceApprovalTaskTx(
		ctx,
		tx,
		policy,
		"finance.recharge",
		requesterUserID,
		tenantID,
		amountCents,
		payload,
		requiresApproval,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	if !requiresApproval {
		if err := applyRechargeTx(
			ctx,
			tx,
			tenantID,
			orderID,
			rechargeNo,
			amountCents,
			requesterUserID,
			reason,
		); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
		if err := markFinanceTaskCompletedTx(ctx, tx, taskID); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	task, err := s.GetStaffFinanceTask(ctx, taskID)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	return model.StaffFinanceOperationResult{
		Task:             task,
		RequiresApproval: requiresApproval,
		Applied:          !requiresApproval,
	}, nil
}

func (s *Store) CreateCustomerRechargeRequest(
	ctx context.Context,
	tenantID int64,
	amountCents uint64,
	reason string,
	requesterUserID int64,
) (model.StaffFinanceOperationResult, error) {
	if amountCents == 0 {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("amount must be positive")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	defer tx.Rollback()

	if err := ensureFinanceCustomerTx(ctx, tx, tenantID); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	policy, _, err := loadFinancePolicyTx(
		ctx,
		tx,
		"finance.recharge",
		amountCents,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	rechargeNo, err := newFinanceReference("RCG")
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_recharge_orders (
			recharge_no,
			tenant_id,
			currency,
			requested_amount_cents,
			credited_amount_cents,
			payment_method,
			status,
			operator_user_id,
			idempotency_key
		)
		VALUES (?, ?, 'CNY', ?, 0, 'customer_request', 'pending_approval', ?, ?)
	`,
		rechargeNo,
		tenantID,
		amountCents,
		requesterUserID,
		rechargeNo,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	payload := financeTaskPayload{
		AmountCents:   amountCents,
		OrderID:       orderID,
		PaymentMethod: "customer_request",
		Reason:        reason,
	}
	taskID, err := createFinanceApprovalTaskTx(
		ctx,
		tx,
		policy,
		"finance.recharge",
		requesterUserID,
		tenantID,
		amountCents,
		payload,
		true,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	task, err := s.GetStaffFinanceTask(ctx, taskID)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	return model.StaffFinanceOperationResult{
		Task:             task,
		RequiresApproval: true,
		Applied:          false,
	}, nil
}

func (s *Store) CreateStaffFinanceRefund(
	ctx context.Context,
	tenantID int64,
	amountCents uint64,
	reason string,
	requesterUserID int64,
) (model.StaffFinanceOperationResult, error) {
	if amountCents == 0 {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("amount must be positive")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("refund reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	defer tx.Rollback()

	if err := ensureFinanceCustomerTx(ctx, tx, tenantID); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	walletID, err := ensureWalletAccountTx(ctx, tx, tenantID, "cash")
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	policy, requiresApproval, err := loadFinancePolicyTx(
		ctx,
		tx,
		"finance.refund",
		amountCents,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	refundNo, err := newFinanceReference("RFD")
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	status := "pending"
	if requiresApproval {
		status = "pending_approval"
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_refund_orders (
			refund_no,
			tenant_id,
			source_type,
			source_id,
			currency,
			refund_amount_cents,
			refund_method,
			status,
			reason,
			operator_user_id,
			idempotency_key
		)
		VALUES (?, ?, 'wallet', ?, 'CNY', ?, 'wallet', ?, ?, ?, ?)
	`,
		refundNo,
		tenantID,
		walletID,
		amountCents,
		status,
		reason,
		requesterUserID,
		refundNo,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	payload := financeTaskPayload{
		AmountCents: amountCents,
		OrderID:     orderID,
		Reason:      reason,
	}
	taskID, err := createFinanceApprovalTaskTx(
		ctx,
		tx,
		policy,
		"finance.refund",
		requesterUserID,
		tenantID,
		amountCents,
		payload,
		requiresApproval,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	if !requiresApproval {
		if err := applyRefundTx(
			ctx,
			tx,
			tenantID,
			orderID,
			refundNo,
			amountCents,
			requesterUserID,
			reason,
		); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
		if err := markFinanceTaskCompletedTx(ctx, tx, taskID); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	task, err := s.GetStaffFinanceTask(ctx, taskID)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	return model.StaffFinanceOperationResult{
		Task:             task,
		RequiresApproval: requiresApproval,
		Applied:          !requiresApproval,
	}, nil
}

func (s *Store) CreateStaffFinanceReward(
	ctx context.Context,
	tenantID int64,
	amountCents uint64,
	reason string,
	requesterUserID int64,
) (model.StaffFinanceOperationResult, error) {
	if amountCents == 0 {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("amount must be positive")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("reward reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	defer tx.Rollback()

	if err := ensureFinanceCustomerTx(ctx, tx, tenantID); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	policy, requiresApproval, err := loadFinancePolicyTx(
		ctx,
		tx,
		"finance.reward",
		amountCents,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	payload := financeTaskPayload{
		AmountCents: amountCents,
		Reason:      reason,
	}
	taskID, err := createFinanceApprovalTaskTx(
		ctx,
		tx,
		policy,
		"finance.reward",
		requesterUserID,
		tenantID,
		amountCents,
		payload,
		requiresApproval,
	)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	if !requiresApproval {
		if err := applyRewardTx(
			ctx,
			tx,
			tenantID,
			taskID,
			amountCents,
			requesterUserID,
			reason,
		); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
		if err := markFinanceTaskCompletedTx(ctx, tx, taskID); err != nil {
			return model.StaffFinanceOperationResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	task, err := s.GetStaffFinanceTask(ctx, taskID)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	return model.StaffFinanceOperationResult{
		Task:             task,
		RequiresApproval: requiresApproval,
		Applied:          !requiresApproval,
	}, nil
}

func (s *Store) CreateStaffAITimeGrantRequest(
	ctx context.Context,
	organizationID int64,
	resourceSeconds int64,
	reason string,
	requesterUserID int64,
) (model.StaffFinanceOperationResult, error) {
	if organizationID <= 0 || resourceSeconds <= 0 {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("invalid ai time request")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("reason is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	defer tx.Rollback()

	var orgType string
	if err := tx.QueryRowContext(ctx, `
		SELECT org_type
		FROM mgmt_tenants
		WHERE id=? AND status='active'
		LIMIT 1
	`, organizationID).Scan(&orgType); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	if orgType != "agent" && orgType != "customer" {
		return model.StaffFinanceOperationResult{}, fmt.Errorf("unsupported ai time target")
	}

	policy, _, err := loadFinancePolicyTx(ctx, tx, "finance.ai_time_grant", 0)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	payload := financeTaskPayload{
		ResourceType:    "ai_seconds",
		ResourceSeconds: resourceSeconds,
		TargetOrgType:   orgType,
		Reason:          reason,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	var policyID any
	if policy != nil {
		policyID = policy.ID
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO staff_approval_tasks (
			policy_id,
			operation_code,
			requester_user_id,
			target_type,
			target_id,
			amount,
			status,
			payload_json
		)
		VALUES (?, 'finance.ai_time_grant', ?, ?, ?, 0, 'pending', ?)
	`, policyID, requesterUserID, orgType, organizationID, payloadJSON)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	taskID, err := result.LastInsertId()
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	task, err := s.GetStaffFinanceTask(ctx, taskID)
	if err != nil {
		return model.StaffFinanceOperationResult{}, err
	}
	return model.StaffFinanceOperationResult{
		Task:             task,
		RequiresApproval: true,
		Applied:          false,
	}, nil
}

func (s *Store) ApproveStaffFinanceTask(
	ctx context.Context,
	taskID int64,
	approverUserID int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var operationCode string
	var requesterUserID int64
	var tenantID sql.NullInt64
	var status string
	var payloadRaw []byte
	err = tx.QueryRowContext(ctx, `
		SELECT
			operation_code,
			requester_user_id,
			target_id,
			status,
			payload_json
		FROM staff_approval_tasks
		WHERE id=?
		FOR UPDATE
	`, taskID).Scan(
		&operationCode,
		&requesterUserID,
		&tenantID,
		&status,
		&payloadRaw,
	)
	if err != nil {
		return err
	}
	if status != "pending" {
		return fmt.Errorf("approval task is not pending")
	}
	if requesterUserID == approverUserID {
		return fmt.Errorf("requester cannot approve own task")
	}
	if !tenantID.Valid || tenantID.Int64 <= 0 {
		return fmt.Errorf("approval target customer is missing")
	}

	var payload financeTaskPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return err
	}

	switch operationCode {
	case "finance.recharge":
		var rechargeNo string
		if err := tx.QueryRowContext(
			ctx,
			"SELECT recharge_no FROM fin_recharge_orders WHERE id=? FOR UPDATE",
			payload.OrderID,
		).Scan(&rechargeNo); err != nil {
			return err
		}
		if err := applyRechargeTx(
			ctx,
			tx,
			tenantID.Int64,
			payload.OrderID,
			rechargeNo,
			payload.AmountCents,
			approverUserID,
			payload.Reason,
		); err != nil {
			return err
		}
	case "finance.refund":
		var (
			refundNo     string
			sourceType   string
			sourceID     int64
			refundMethod string
		)
		if err := tx.QueryRowContext(
			ctx,
			"SELECT refund_no, source_type, source_id, refund_method FROM fin_refund_orders WHERE id=? FOR UPDATE",
			payload.OrderID,
		).Scan(&refundNo, &sourceType, &sourceID, &refundMethod); err != nil {
			return err
		}
		if sourceType == "order" && refundMethod == "sandbox_original" {
			if err := applyOrderRefundTx(
				ctx,
				tx,
				tenantID.Int64,
				payload.OrderID,
				refundNo,
				sourceID,
				payload.AmountCents,
				approverUserID,
				payload.Reason,
			); err != nil {
				return err
			}
		} else {
			if err := applyRefundTx(
				ctx,
				tx,
				tenantID.Int64,
				payload.OrderID,
				refundNo,
				payload.AmountCents,
				approverUserID,
				payload.Reason,
			); err != nil {
				return err
			}
		}
	case "finance.reward":
		if err := applyRewardTx(
			ctx,
			tx,
			tenantID.Int64,
			taskID,
			payload.AmountCents,
			approverUserID,
			payload.Reason,
		); err != nil {
			return err
		}
	case "finance.ai_time_grant":
		if payload.ResourceType != "ai_seconds" || payload.ResourceSeconds <= 0 {
			return fmt.Errorf("invalid ai time approval payload")
		}
		reason := fmt.Sprintf("%s（申请人用户ID:%d；审批任务#%d）", payload.Reason, requesterUserID, taskID)
		if err := adjustOrganizationResourceTx(
			ctx,
			tx,
			tenantID.Int64,
			payload.ResourceType,
			payload.ResourceSeconds,
			approverUserID,
			reason,
			"marketing_ai_time_grant",
		); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported finance approval operation")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE staff_approval_tasks
		SET
			status='approved',
			approver_user_id=?,
			decided_at=UTC_TIMESTAMP(3)
		WHERE id=? AND status='pending'
	`, approverUserID, taskID); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) RejectStaffFinanceTask(
	ctx context.Context,
	taskID int64,
	approverUserID int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var operationCode string
	var requesterUserID int64
	var status string
	var payloadRaw []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT
			operation_code,
			requester_user_id,
			status,
			payload_json
		FROM staff_approval_tasks
		WHERE id=?
		FOR UPDATE
	`, taskID).Scan(
		&operationCode,
		&requesterUserID,
		&status,
		&payloadRaw,
	); err != nil {
		return err
	}
	if status != "pending" {
		return fmt.Errorf("approval task is not pending")
	}
	if requesterUserID == approverUserID {
		return fmt.Errorf("requester cannot review own task")
	}

	var payload financeTaskPayload
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return err
	}

	switch operationCode {
	case "finance.recharge":
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE fin_recharge_orders SET status='rejected' WHERE id=?",
			payload.OrderID,
		); err != nil {
			return err
		}
	case "finance.refund":
		if _, err := tx.ExecContext(
			ctx,
			"UPDATE fin_refund_orders SET status='rejected' WHERE id=?",
			payload.OrderID,
		); err != nil {
			return err
		}
	case "finance.reward":
	case "finance.ai_time_grant":
	default:
		return fmt.Errorf("unsupported finance approval operation")
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE staff_approval_tasks
		SET
			status='rejected',
			approver_user_id=?,
			decided_at=UTC_TIMESTAMP(3)
		WHERE id=? AND status='pending'
	`, approverUserID, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func loadFinancePolicyTx(
	ctx context.Context,
	tx *sql.Tx,
	operationCode string,
	amountCents uint64,
) (*financeApprovalPolicy, bool, error) {
	var policy financeApprovalPolicy
	err := tx.QueryRowContext(ctx, `
		SELECT
			id,
			mode,
			threshold_amount,
			approver_role_code
		FROM staff_approval_policies
		WHERE operation_code=?
		  AND status='active'
		ORDER BY id ASC
		LIMIT 1
	`, operationCode).Scan(
		&policy.ID,
		&policy.Mode,
		&policy.ThresholdAmount,
		&policy.ApproverRoleCode,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	amountYuan := float64(amountCents) / 100
	switch policy.Mode {
	case "manual":
		return &policy, true, nil
	case "threshold":
		return &policy, amountYuan > policy.ThresholdAmount, nil
	case "direct":
		return &policy, false, nil
	default:
		return nil, false, fmt.Errorf("invalid finance approval policy mode")
	}
}

func createFinanceApprovalTaskTx(
	ctx context.Context,
	tx *sql.Tx,
	policy *financeApprovalPolicy,
	operationCode string,
	requesterUserID int64,
	tenantID int64,
	amountCents uint64,
	payload financeTaskPayload,
	requiresApproval bool,
) (int64, error) {
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var policyID any
	if policy != nil {
		policyID = policy.ID
	}
	status := "pending"
	if !requiresApproval {
		status = "processing"
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO staff_approval_tasks (
			policy_id,
			operation_code,
			requester_user_id,
			target_type,
			target_id,
			amount,
			status,
			payload_json
		)
		VALUES (?, ?, ?, 'customer', ?, ?, ?, ?)
	`,
		policyID,
		operationCode,
		requesterUserID,
		tenantID,
		float64(amountCents)/100,
		status,
		payloadJSON,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func markFinanceTaskCompletedTx(
	ctx context.Context,
	tx *sql.Tx,
	taskID int64,
) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE staff_approval_tasks
		SET status='completed', decided_at=UTC_TIMESTAMP(3)
		WHERE id=?
	`, taskID)
	return err
}

func applyRechargeTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	orderID int64,
	rechargeNo string,
	amountCents uint64,
	operatorUserID int64,
	reason string,
) error {
	walletID, before, err := lockWalletAccountTx(
		ctx,
		tx,
		tenantID,
		"cash",
	)
	if err != nil {
		return err
	}
	after := before + int64(amountCents)
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE fin_wallet_accounts SET balance_cents=? WHERE id=?",
		after,
		walletID,
	); err != nil {
		return err
	}

	externalID, err := newFinanceReference("LED")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_ledger (
			external_id,
			tenant_id,
			wallet_account_id,
			direction,
			amount_cents,
			balance_before_cents,
			balance_after_cents,
			business_type,
			business_id,
			order_no,
			operator_user_id,
			reason,
			idempotency_key,
			occurred_at
		)
		VALUES (
			?, ?, ?, 'credit', ?, ?, ?,
			'recharge', ?, ?, ?, ?, ?, UTC_TIMESTAMP(3)
		)
	`,
		externalID,
		tenantID,
		walletID,
		amountCents,
		before,
		after,
		orderID,
		rechargeNo,
		operatorUserID,
		reason,
		rechargeNo+"-ledger",
	); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE fin_recharge_orders
		SET
			credited_amount_cents=?,
			status='paid',
			paid_at=UTC_TIMESTAMP(3)
		WHERE id=?
	`, amountCents, orderID)
	return err
}

func applyRefundTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	orderID int64,
	refundNo string,
	amountCents uint64,
	operatorUserID int64,
	reason string,
) error {
	walletID, before, err := lockWalletAccountTx(
		ctx,
		tx,
		tenantID,
		"cash",
	)
	if err != nil {
		return err
	}
	if before < int64(amountCents) {
		return fmt.Errorf("cash wallet balance is insufficient")
	}
	after := before - int64(amountCents)
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE fin_wallet_accounts SET balance_cents=? WHERE id=?",
		after,
		walletID,
	); err != nil {
		return err
	}

	externalID, err := newFinanceReference("LED")
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_ledger (
			external_id,
			tenant_id,
			wallet_account_id,
			direction,
			amount_cents,
			balance_before_cents,
			balance_after_cents,
			business_type,
			business_id,
			order_no,
			operator_user_id,
			reason,
			idempotency_key,
			occurred_at
		)
		VALUES (
			?, ?, ?, 'debit', ?, ?, ?,
			'refund', ?, ?, ?, ?, ?, UTC_TIMESTAMP(3)
		)
	`,
		externalID,
		tenantID,
		walletID,
		amountCents,
		before,
		after,
		orderID,
		refundNo,
		operatorUserID,
		reason,
		refundNo+"-ledger",
	); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE fin_refund_orders
		SET
			status='completed',
			processed_at=UTC_TIMESTAMP(3)
		WHERE id=?
	`, orderID)
	return err
}

func applyRewardTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	taskID int64,
	amountCents uint64,
	operatorUserID int64,
	reason string,
) error {
	walletID, before, err := lockWalletAccountTx(
		ctx,
		tx,
		tenantID,
		"reward",
	)
	if err != nil {
		return err
	}
	after := before + int64(amountCents)
	if _, err := tx.ExecContext(
		ctx,
		"UPDATE fin_wallet_accounts SET balance_cents=? WHERE id=?",
		after,
		walletID,
	); err != nil {
		return err
	}

	externalID, err := newFinanceReference("LED")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_ledger (
			external_id,
			tenant_id,
			wallet_account_id,
			direction,
			amount_cents,
			balance_before_cents,
			balance_after_cents,
			business_type,
			business_id,
			operator_user_id,
			reason,
			idempotency_key,
			occurred_at
		)
		VALUES (
			?, ?, ?, 'credit', ?, ?, ?,
			'reward_grant', ?, ?, ?, ?, UTC_TIMESTAMP(3)
		)
	`,
		externalID,
		tenantID,
		walletID,
		amountCents,
		before,
		after,
		taskID,
		operatorUserID,
		reason,
		fmt.Sprintf("reward-task-%d", taskID),
	)
	return err
}

func ensureFinanceCustomerTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
) error {
	var status string
	err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM mgmt_tenants
		WHERE id=? AND org_type='customer'
		LIMIT 1
	`, tenantID).Scan(&status)
	if err != nil {
		return err
	}
	if status != "active" {
		return fmt.Errorf("customer is disabled")
	}
	return nil
}

func ensureWalletAccountTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	accountType string,
) (int64, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO fin_wallet_accounts (
			tenant_id,
			account_type,
			currency,
			balance_cents,
			status
		)
		VALUES (?, ?, 'CNY', 0, 'active')
	`, tenantID, accountType); err != nil {
		return 0, err
	}
	var walletID int64
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM fin_wallet_accounts
		WHERE tenant_id=?
		  AND account_type=?
		  AND currency='CNY'
		  AND status='active'
		LIMIT 1
	`, tenantID, accountType).Scan(&walletID)
	return walletID, err
}

func lockWalletAccountTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	accountType string,
) (int64, int64, error) {
	walletID, err := ensureWalletAccountTx(
		ctx,
		tx,
		tenantID,
		accountType,
	)
	if err != nil {
		return 0, 0, err
	}
	var balance int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance_cents
		FROM fin_wallet_accounts
		WHERE id=?
		FOR UPDATE
	`, walletID).Scan(&balance); err != nil {
		return 0, 0, err
	}
	return walletID, balance, nil
}

func newFinanceReference(prefix string) (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"%s-%s-%s",
		prefix,
		time.Now().UTC().Format("20060102150405"),
		hex.EncodeToString(raw),
	), nil
}
