package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func createDeviceOrderRefundRequestTx(
	ctx context.Context,
	tx *sql.Tx,
	requesterUserID int64,
	rmaID int64,
	rmaNo string,
) (int64, string, error) {
	var (
		sourceOrderID sql.NullInt64
		sourceOrderNo string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT source_order_id, source_order_no
		FROM inv_rma_links
		WHERE rma_id=?
	`, rmaID).Scan(&sourceOrderID, &sourceOrderNo); err != nil {
		return 0, "", err
	}
	if !sourceOrderID.Valid {
		return 0, "", fmt.Errorf("RMA has no source order")
	}

	var (
		tenantID       int64
		orderType      string
		orderStatus    string
		paidAmount     uint64
		refundedAmount uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, order_type, status,
		       paid_amount_cents, refunded_amount_cents
		FROM biz_orders
		WHERE id=?
		FOR UPDATE
	`, sourceOrderID.Int64).Scan(
		&tenantID,
		&orderType,
		&orderStatus,
		&paidAmount,
		&refundedAmount,
	); err != nil {
		return 0, "", err
	}
	if orderType != "device" {
		return 0, "", fmt.Errorf("RMA refund source is not a device order")
	}
	if paidAmount == 0 || refundedAmount >= paidAmount {
		return 0, "", ErrShopOrderFullyRefunded
	}
	switch orderStatus {
	case "paid", "fulfilled", "partially_refunded":
	default:
		return 0, "", ErrShopOrderNotPaid
	}

	idempotencyKey := fmt.Sprintf("device-rma-refund-%d", rmaID)
	var (
		existingID int64
		existingNo string
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, refund_no
		FROM fin_refund_orders
		WHERE idempotency_key=?
		LIMIT 1
	`, idempotencyKey).Scan(&existingID, &existingNo)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inv_rma_links
			SET refund_id=?, refund_no=?
			WHERE rma_id=?
		`, existingID, existingNo, rmaID); err != nil {
			return 0, "", err
		}
		return existingID, existingNo, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}

	amountCents := paidAmount - refundedAmount
	refundNo := fmt.Sprintf("REF-DEV-%d", time.Now().UTC().UnixNano())
	reason := "设备退货售后验收完成 · " + rmaNo
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_refund_orders (
			refund_no, tenant_id, source_type, source_id,
			currency, refund_amount_cents, refund_method,
			status, reason, operator_user_id,
			idempotency_key
		)
		VALUES (
			?, ?, 'order', ?,
			'CNY', ?, 'sandbox_original',
			'pending_approval', ?, ?,
			?
		)
	`,
		refundNo,
		tenantID,
		sourceOrderID.Int64,
		amountCents,
		reason,
		requesterUserID,
		idempotencyKey,
	)
	if err != nil {
		return 0, "", err
	}
	refundID, err := result.LastInsertId()
	if err != nil {
		return 0, "", err
	}

	policy, _, err := loadFinancePolicyTx(
		ctx,
		tx,
		"finance.refund",
		amountCents,
	)
	if err != nil {
		return 0, "", err
	}
	payload := financeTaskPayload{
		AmountCents:   amountCents,
		OrderID:       refundID,
		SourceOrderID: sourceOrderID.Int64,
		RMAID:         rmaID,
		PaymentMethod: "sandbox_original",
		Reason:        reason,
	}
	if _, err := createFinanceApprovalTaskTx(
		ctx,
		tx,
		policy,
		"finance.refund",
		requesterUserID,
		tenantID,
		amountCents,
		payload,
		true,
	); err != nil {
		return 0, "", err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_rma_links
		SET refund_id=?, refund_no=?
		WHERE rma_id=?
	`, refundID, refundNo, rmaID); err != nil {
		return 0, "", err
	}

	_ = sourceOrderNo
	return refundID, refundNo, nil
}

func applyOrderRefundTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	refundID int64,
	refundNo string,
	sourceOrderID int64,
	amountCents uint64,
	operatorUserID int64,
	reason string,
) error {
	var (
		orderType      string
		orderStatus    string
		paidAmount     uint64
		refundedAmount uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT order_type, status,
		       paid_amount_cents, refunded_amount_cents
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		FOR UPDATE
	`, sourceOrderID, tenantID).Scan(
		&orderType,
		&orderStatus,
		&paidAmount,
		&refundedAmount,
	); err != nil {
		return err
	}
	if orderType != "device" {
		return fmt.Errorf("order refund only supports device orders")
	}
	if paidAmount == 0 || refundedAmount >= paidAmount {
		return ErrShopOrderFullyRefunded
	}
	outstanding := paidAmount - refundedAmount
	if amountCents == 0 || amountCents > outstanding {
		return ErrRefundAmountInvalid
	}

	newRefunded := refundedAmount + amountCents
	newOrderStatus := "partially_refunded"
	if newRefunded >= paidAmount {
		newOrderStatus = "refunded"
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_refund_orders
		SET status='completed',
		    operator_user_id=?,
		    processed_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, operatorUserID, refundID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_orders
		SET refunded_amount_cents=?, status=?
		WHERE id=? AND tenant_id=?
	`, newRefunded, newOrderStatus, sourceOrderID, tenantID); err != nil {
		return err
	}

	if err := reverseOrderIncentivesForRefundTx(
		ctx,
		tx,
		sourceOrderID,
		refundID,
		refundNo,
		amountCents,
		paidAmount,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE inv_rmas r
		INNER JOIN inv_rma_links l ON l.rma_id=r.id
		SET r.status='COMPLETED',
		    r.completed_at=COALESCE(r.completed_at, CURRENT_TIMESTAMP(3)),
		    r.updated_at=CURRENT_TIMESTAMP(3)
		WHERE l.refund_id=?
	`, refundID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO biz_audit_events (
			actor_user_id, action, entity_type, entity_id,
			reason, after_json, created_at
		)
		VALUES (
			?, 'finance.order_refund.completed', 'order', ?,
			?, JSON_OBJECT(
				'refund_id', ?,
				'refund_no', ?,
				'refund_amount_cents', ?,
				'order_status', ?
			),
			CURRENT_TIMESTAMP(3)
		)
	`,
		operatorUserID,
		fmt.Sprint(sourceOrderID),
		reason,
		refundID,
		refundNo,
		amountCents,
		newOrderStatus,
	); err != nil {
		return err
	}

	return nil
}

func reverseOrderIncentivesForRefundTx(
	ctx context.Context,
	tx *sql.Tx,
	sourceOrderID int64,
	refundID int64,
	refundNo string,
	refundAmount uint64,
	paidAmount uint64,
) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, beneficiary_type, beneficiary_id, earning_type,
		       program_version_id, rule_id, currency,
		       amount_cents, quota_seconds, status, available_at,
		       COALESCE(CAST(calculation_snapshot_json AS CHAR), '{}')
		FROM inc_earnings
		WHERE source_order_id=?
		  AND reversal_of_earning_id IS NULL
		  AND status <> 'reversed'
		ORDER BY id ASC
		FOR UPDATE
	`, sourceOrderID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type earningRow struct {
		ID               int64
		BeneficiaryType  string
		BeneficiaryID    int64
		EarningType      string
		ProgramVersionID sql.NullInt64
		RuleID           sql.NullInt64
		Currency         string
		AmountCents      int64
		QuotaSeconds     int64
		Status           string
		AvailableAt      sql.NullTime
		SnapshotJSON     string
	}
	items := make([]earningRow, 0)
	for rows.Next() {
		var item earningRow
		if err := rows.Scan(
			&item.ID,
			&item.BeneficiaryType,
			&item.BeneficiaryID,
			&item.EarningType,
			&item.ProgramVersionID,
			&item.RuleID,
			&item.Currency,
			&item.AmountCents,
			&item.QuotaSeconds,
			&item.Status,
			&item.AvailableAt,
			&item.SnapshotJSON,
		); err != nil {
			return err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	fullRefund := refundAmount >= paidAmount
	for _, item := range items {
		if item.BeneficiaryType == customerReferralBeneficiaryType {
			var snapshot map[string]any
			if json.Unmarshal([]byte(item.SnapshotJSON), &snapshot) == nil {
				if enabled, exists := snapshot["refund_reversal"].(bool); exists && !enabled {
					continue
				}
			}
		}
		reverseAmount := item.AmountCents
		reverseQuota := item.QuotaSeconds
		if !fullRefund && paidAmount > 0 {
			reverseAmount = item.AmountCents * int64(refundAmount) / int64(paidAmount)
			reverseQuota = item.QuotaSeconds * int64(refundAmount) / int64(paidAmount)
		}
		if reverseAmount == 0 && reverseQuota == 0 {
			continue
		}

		if item.BeneficiaryType != customerReferralBeneficiaryType {
			switch item.Status {
			case "pending", "available":
				if fullRefund {
					if _, err := tx.ExecContext(ctx, `
						UPDATE inc_earnings
						SET status='reversed', source_refund_id=?, updated_at=CURRENT_TIMESTAMP(3)
						WHERE id=?
					`, refundID, item.ID); err != nil {
						return err
					}
					continue
				}
			}
		}

		externalID := fmt.Sprintf("REV-%d-%d", refundID, item.ID)
		idempotencyKey := fmt.Sprintf("refund-earning-reversal-%d-%d", refundID, item.ID)
		reversalStatus := "available"
		var reversalAvailableAt any = time.Now().UTC()
		if item.BeneficiaryType == customerReferralBeneficiaryType && item.Status == "pending" && item.AvailableAt.Valid {
			reversalStatus = "pending"
			reversalAvailableAt = item.AvailableAt.Time
		}
		result, err := tx.ExecContext(ctx, `
			INSERT IGNORE INTO inc_earnings (
				external_id, beneficiary_type, beneficiary_id,
				earning_type, source_order_id, source_refund_id,
				program_version_id, rule_id, currency,
				amount_cents, quota_seconds, status, available_at,
				reversal_of_earning_id, calculation_snapshot_json,
				idempotency_key
			)
			VALUES (
				?, ?, ?, ?, ?, ?,
				?, ?, ?,
				?, ?, ?, ?,
				?, JSON_OBJECT(
					'refund_no', ?,
					'refund_amount_cents', ?,
					'paid_amount_cents', ?
				),
				?
			)
		`,
			externalID,
			item.BeneficiaryType,
			item.BeneficiaryID,
			item.EarningType+"_refund_reversal",
			sourceOrderID,
			refundID,
			nullableInt64Value(item.ProgramVersionID),
			nullableInt64Value(item.RuleID),
			item.Currency,
			-reverseAmount,
			-reverseQuota,
			reversalStatus,
			reversalAvailableAt,
			item.ID,
			refundNo,
			refundAmount,
			paidAmount,
			idempotencyKey,
		)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 && item.BeneficiaryType == customerReferralBeneficiaryType && reverseAmount != 0 {
			reversalID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			referenceID := reversalID
			availableDelta := -reverseAmount
			frozenDelta := int64(0)
			if item.Status == "pending" {
				availableDelta = 0
				frozenDelta = -reverseAmount
			}
			if err := applyBeneficiaryWalletDeltaTx(
				ctx,
				tx,
				customerReferralBeneficiaryType,
				item.BeneficiaryID,
				fmt.Sprintf("referral-reversal-%d-%d", refundID, item.ID),
				"referral_refund_reversal",
				"earning",
				&referenceID,
				availableDelta,
				frozenDelta,
				nil,
				"推荐客户退款，按原返佣规则冲回",
			); err != nil {
				return err
			}
		}
	}

	return nil
}
