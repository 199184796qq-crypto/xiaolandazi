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

func (s *Store) GetLiveQuotaSummary(
	ctx context.Context,
	tenantID int64,
	now time.Time,
) (model.LiveQuotaSummary, error) {
	if tenantID <= 0 {
		return model.LiveQuotaSummary{}, errors.New("invalid tenant")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveQuotaSummary{}, err
	}
	defer tx.Rollback()

	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return model.LiveQuotaSummary{}, err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return model.LiveQuotaSummary{}, err
	}

	var summary model.LiveQuotaSummary
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(remaining_seconds), 0)
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at<=?
		  AND expires_at>?
		  AND remaining_seconds>0
	`, tenantID, now, now).Scan(&summary.ActiveSeconds); err != nil {
		return model.LiveQuotaSummary{}, err
	}

	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(remaining_seconds), 0)
		FROM biz_time_card_assets
		WHERE tenant_id=?
		  AND status='unactivated'
		  AND remaining_seconds>0
		  AND (activation_deadline_at IS NULL OR activation_deadline_at>?)
	`, tenantID, now).Scan(
		&summary.ReserveTimeCardCount,
		&summary.ReserveTimeCardSeconds,
	); err != nil {
		return model.LiveQuotaSummary{}, err
	}

	var (
		sourceType  string
		sourceID    sql.NullInt64
		remaining   uint64
		expiresAt   time.Time
		assetNo     string
		productName string
	)
	err = tx.QueryRowContext(ctx, `
		SELECT q.source_type, q.source_id, q.remaining_seconds, q.expires_at,
		       COALESCE(a.asset_no, ''), COALESCE(a.product_name_snapshot, '')
		FROM quota_buckets q
		LEFT JOIN biz_time_card_assets a ON a.quota_bucket_id=q.id
		WHERE q.tenant_id=?
		  AND q.status='active'
		  AND q.effective_at<=?
		  AND q.expires_at>?
		  AND q.remaining_seconds>0
		ORDER BY q.expires_at ASC, q.priority ASC, q.id ASC
		LIMIT 1
	`, tenantID, now, now).Scan(
		&sourceType,
		&sourceID,
		&remaining,
		&expiresAt,
		&assetNo,
		&productName,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.LiveQuotaSummary{}, err
	}
	if err == nil {
		label := "AI 时长"
		switch sourceType {
		case "membership_cycle":
			label = "会员基础 AI 时长"
		case "time_card_asset", "time_card_purchase":
			label = "时长卡"
			if productName != "" {
				label = productName
			}
		}
		expiry := expiresAt
		summary.Current = &model.LiveQuotaSourceSummary{
			SourceType:       sourceType,
			SourceLabel:      label,
			AssetNo:          assetNo,
			RemainingSeconds: remaining,
			ExpiresAt:        &expiry,
		}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT a.asset_no, a.product_name_snapshot, a.status,
		       a.original_seconds,
		       CASE WHEN a.status='active'
		            THEN COALESCE(q.remaining_seconds, a.remaining_seconds)
		            ELSE a.remaining_seconds END,
		       a.activation_deadline_at, a.activated_at, a.expires_at
		FROM biz_time_card_assets a
		LEFT JOIN quota_buckets q ON q.id=a.quota_bucket_id
		WHERE a.tenant_id=?
		  AND (
		    (a.status='unactivated' AND a.remaining_seconds>0
		      AND (a.activation_deadline_at IS NULL OR a.activation_deadline_at>?))
		    OR
		    (a.status='active' AND COALESCE(q.remaining_seconds, a.remaining_seconds)>0
		      AND a.expires_at>?)
		  )
		ORDER BY CASE WHEN a.status='active' THEN 0 ELSE 1 END ASC,
		         CASE WHEN a.activation_deadline_at IS NULL THEN 1 ELSE 0 END ASC,
		         a.activation_deadline_at ASC,
		         a.id ASC
		LIMIT 20
	`, tenantID, now, now)
	if err != nil {
		return model.LiveQuotaSummary{}, err
	}
	summary.TimeCards = make([]model.LiveTimeCardSummary, 0)
	for rows.Next() {
		var item model.LiveTimeCardSummary
		var activationDeadline sql.NullTime
		var activatedAt sql.NullTime
		var assetExpiresAt sql.NullTime
		if err := rows.Scan(
			&item.AssetNo,
			&item.ProductName,
			&item.Status,
			&item.OriginalSeconds,
			&item.RemainingSeconds,
			&activationDeadline,
			&activatedAt,
			&assetExpiresAt,
		); err != nil {
			rows.Close()
			return model.LiveQuotaSummary{}, err
		}
		if activationDeadline.Valid {
			value := activationDeadline.Time
			item.ActivationDeadlineAt = &value
		}
		if activatedAt.Valid {
			value := activatedAt.Time
			item.ActivatedAt = &value
		}
		if assetExpiresAt.Valid {
			value := assetExpiresAt.Time
			item.ExpiresAt = &value
		}
		summary.TimeCards = append(summary.TimeCards, item)
	}
	if err := rows.Close(); err != nil {
		return model.LiveQuotaSummary{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.LiveQuotaSummary{}, err
	}
	return summary, nil
}

func (s *Store) ActivateLiveTimeCards(
	ctx context.Context,
	tenantID int64,
	count uint32,
	now time.Time,
) (model.LiveQuotaSummary, uint32, error) {
	if tenantID <= 0 {
		return model.LiveQuotaSummary{}, 0, errors.New("invalid tenant")
	}
	if count == 0 {
		count = 1
	}
	if count > 20 {
		count = 20
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveQuotaSummary{}, 0, err
	}
	defer tx.Rollback()

	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return model.LiveQuotaSummary{}, 0, err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return model.LiveQuotaSummary{}, 0, err
	}

	var activated uint32
	for activated < count {
		ok, err := activateNextTimeCardAssetTx(ctx, tx, tenantID, now, false)
		if err != nil {
			return model.LiveQuotaSummary{}, activated, err
		}
		if !ok {
			break
		}
		activated++
	}
	if activated > 0 {
		if err := reconcileCustomerAIResourceToQuotaTx(
			ctx,
			tx,
			tenantID,
			now,
			"time_card_pool_load",
			fmt.Sprintf("一次充入 AI 工作时长池 %d 张时长卡", activated),
		); err != nil {
			return model.LiveQuotaSummary{}, activated, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.LiveQuotaSummary{}, activated, err
	}
	summary, err := s.GetLiveQuotaSummary(ctx, tenantID, now)
	return summary, activated, err
}

func ensureLiveQuotaAvailableTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	required uint64,
	now time.Time,
	reconcileResource bool,
) ([]quotaBucketLock, error) {
	if required == 0 {
		required = 1
	}
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return nil, err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return nil, err
	}
	for attempts := 0; attempts < 128; attempts++ {
		buckets, err := loadActiveQuotaBucketsTx(ctx, tx, tenantID, now)
		if err != nil {
			return nil, err
		}
		var available uint64
		for _, bucket := range buckets {
			available += bucket.Remaining
		}
		if available >= required {
			return buckets, nil
		}
		activated, err := activateNextTimeCardAssetTx(
			ctx,
			tx,
			tenantID,
			now,
			reconcileResource,
		)
		if err != nil {
			return nil, err
		}
		if !activated {
			return buckets, nil
		}
	}
	return loadActiveQuotaBucketsTx(ctx, tx, tenantID, now)
}

func activateNextTimeCardAssetTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	now time.Time,
	reconcileResource bool,
) (bool, error) {
	var (
		assetID        int64
		assetNo        string
		orderID        int64
		productName    string
		original       uint64
		remaining      uint64
		activationMode string
		validityDays   uint32
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, asset_no, source_order_id, product_name_snapshot,
		       original_seconds, remaining_seconds, activation_mode, validity_days
		FROM biz_time_card_assets
		WHERE tenant_id=?
		  AND status='unactivated'
		  AND remaining_seconds>0
		  AND (activation_deadline_at IS NULL OR activation_deadline_at>?)
		ORDER BY
		  CASE WHEN activation_deadline_at IS NULL THEN 1 ELSE 0 END ASC,
		  activation_deadline_at ASC,
		  id ASC
		LIMIT 1
		FOR UPDATE
	`, tenantID, now).Scan(
		&assetID,
		&assetNo,
		&orderID,
		&productName,
		&original,
		&remaining,
		&activationMode,
		&validityDays,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if activationMode != "first_use" || validityDays == 0 || remaining == 0 {
		return false, fmt.Errorf("invalid time card asset activation rule")
	}

	expiresAt := now.AddDate(0, 0, int(validityDays))
	bucketExternalID := fmt.Sprintf("time-card-asset-%d", assetID)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO quota_buckets (
			external_id, tenant_id, source_type, source_id,
			original_seconds, remaining_seconds, effective_at,
			expires_at, priority, status, metadata_json
		)
		VALUES (
			?, ?, 'time_card_asset', ?,
			?, ?, ?, ?, 50, 'active',
			JSON_OBJECT('asset_no', ?, 'order_id', ?, 'product_name', ?)
		)
	`,
		bucketExternalID,
		tenantID,
		assetID,
		original,
		remaining,
		now,
		expiresAt,
		assetNo,
		orderID,
		productName,
	)
	if err != nil {
		return false, err
	}
	bucketID, err := result.LastInsertId()
	if err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_time_card_assets
		SET activated_at=?, expires_at=?, quota_bucket_id=?, status='active'
		WHERE id=? AND status='unactivated'
	`, now, expiresAt, bucketID, assetID); err != nil {
		return false, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quota_ledger (
			external_id, tenant_id, bucket_id,
			change_seconds, remaining_before_seconds, remaining_after_seconds,
			business_type, business_id, operator_user_id,
			reason, idempotency_key, occurred_at
		)
		VALUES (?, ?, ?, ?, 0, ?, 'time_card_activation', ?, NULL, ?, ?, ?)
	`,
		fmt.Sprintf("time-card-activation-%d", assetID),
		tenantID,
		bucketID,
		int64(remaining),
		remaining,
		assetID,
		"时长卡首次使用自动激活 · "+assetNo,
		fmt.Sprintf("time-card-activation-%d", assetID),
		now,
	); err != nil {
		return false, err
	}

	if reconcileResource {
		if err := reconcileCustomerAIResourceToQuotaTx(
			ctx,
			tx,
			tenantID,
			now,
			"time_card_activation",
			"时长卡首次使用自动激活 · "+assetNo,
		); err != nil {
			return false, err
		}
	}
	return true, nil
}

func expireTimeCardAssetsTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	now time.Time,
) (bool, error) {
	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_time_card_assets
		SET status='expired',
		    expires_at=COALESCE(expires_at, activation_deadline_at),
		    remaining_seconds=0
		WHERE tenant_id=?
		  AND status='unactivated'
		  AND activation_deadline_at IS NOT NULL
		  AND activation_deadline_at<=?
	`, tenantID, now); err != nil {
		return false, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT a.id, a.asset_no, a.quota_bucket_id, q.remaining_seconds
		FROM biz_time_card_assets a
		INNER JOIN quota_buckets q ON q.id=a.quota_bucket_id
		WHERE a.tenant_id=?
		  AND a.status='active'
		  AND a.expires_at IS NOT NULL
		  AND a.expires_at<=?
		ORDER BY a.id ASC
		FOR UPDATE
	`, tenantID, now)
	if err != nil {
		return false, err
	}
	type expiredAsset struct {
		id        int64
		assetNo   string
		bucketID  int64
		remaining uint64
	}
	items := make([]expiredAsset, 0)
	for rows.Next() {
		var item expiredAsset
		if err := rows.Scan(&item.id, &item.assetNo, &item.bucketID, &item.remaining); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}

	for _, item := range items {
		if item.remaining > 0 {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO quota_ledger (
					external_id, tenant_id, bucket_id,
					change_seconds, remaining_before_seconds, remaining_after_seconds,
					business_type, business_id, operator_user_id,
					reason, idempotency_key, occurred_at
				)
				VALUES (?, ?, ?, ?, ?, 0, 'time_card_expired', ?, NULL, ?, ?, ?)
			`,
				fmt.Sprintf("time-card-expired-%d", item.id),
				tenantID,
				item.bucketID,
				-int64(item.remaining),
				item.remaining,
				item.id,
				"时长卡有效期届满，未使用时长自动失效 · "+item.assetNo,
				fmt.Sprintf("time-card-expired-%d", item.id),
				now,
			); err != nil {
				return false, err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE quota_buckets
			SET remaining_seconds=0, status='expired', updated_at=?
			WHERE id=?
		`, now, item.bucketID); err != nil {
			return false, err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_time_card_assets
			SET remaining_seconds=0, status='expired', updated_at=?
			WHERE id=?
		`, now, item.id); err != nil {
			return false, err
		}
	}

	if len(items) > 0 {
		if err := reconcileCustomerAIResourceToQuotaTx(
			ctx,
			tx,
			tenantID,
			now,
			"time_card_expired",
			"时长卡有效期届满自动失效",
		); err != nil {
			return false, err
		}
	}
	return len(items) > 0, nil
}

func reconcileCustomerAIResourceToQuotaTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	now time.Time,
	businessType string,
	reason string,
) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, 'ai_seconds', 'seconds', 0, 0, 'active')
	`, tenantID); err != nil {
		return err
	}

	var before int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type='ai_seconds'
		FOR UPDATE
	`, tenantID).Scan(&before); err != nil {
		return err
	}

	var after int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(remaining_seconds), 0)
		FROM quota_buckets
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_at<=?
		  AND expires_at>?
		  AND remaining_seconds>0
	`, tenantID, now, now).Scan(&after); err != nil {
		return err
	}
	if before == after {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='ai_seconds'
	`, after, tenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id, resource_type, change_quantity,
			balance_before, balance_after, business_type,
			operator_user_id, reason, created_at
		)
		VALUES (?, 'ai_seconds', ?, ?, ?, ?, NULL, ?, ?)
	`,
		tenantID,
		after-before,
		before,
		after,
		businessType,
		reason,
		now,
	); err != nil {
		return err
	}
	return nil
}

func refundTimeCardAssetOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	userID int64,
	orderID int64,
	orderNo string,
	paidAmount uint64,
	refundedAmount uint64,
	input model.SandboxRefundOrderInput,
	idempotencyKey string,
) (model.RefundRecord, bool, error) {
	var assetCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM biz_time_card_assets
		WHERE tenant_id=? AND source_order_id=?
	`, tenantID, orderID).Scan(&assetCount); err != nil {
		return model.RefundRecord{}, false, err
	}
	if assetCount == 0 {
		return model.RefundRecord{}, false, nil
	}

	now := time.Now().UTC()
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return model.RefundRecord{}, true, err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return model.RefundRecord{}, true, err
	}

	type refundableAsset struct {
		id        int64
		assetNo   string
		status    string
		original  uint64
		remaining uint64
		bucketID  sql.NullInt64
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, asset_no, status, original_seconds, remaining_seconds, quota_bucket_id
		FROM biz_time_card_assets
		WHERE tenant_id=? AND source_order_id=?
		ORDER BY
		  CASE WHEN status='unactivated' THEN 0 WHEN status='active' THEN 1 ELSE 2 END ASC,
		  id ASC
		FOR UPDATE
	`, tenantID, orderID)
	if err != nil {
		return model.RefundRecord{}, true, err
	}
	assets := make([]refundableAsset, 0, assetCount)
	var originalSeconds uint64
	for rows.Next() {
		var item refundableAsset
		if err := rows.Scan(
			&item.id,
			&item.assetNo,
			&item.status,
			&item.original,
			&item.remaining,
			&item.bucketID,
		); err != nil {
			rows.Close()
			return model.RefundRecord{}, true, err
		}
		originalSeconds += item.original
		assets = append(assets, item)
	}
	if err := rows.Close(); err != nil {
		return model.RefundRecord{}, true, err
	}

	var refundableSeconds uint64
	for index := range assets {
		item := &assets[index]
		switch item.status {
		case "unactivated":
			refundableSeconds += item.remaining
		case "active":
			if !item.bucketID.Valid {
				item.remaining = 0
				continue
			}
			var bucketRemaining uint64
			if err := tx.QueryRowContext(ctx, `
				SELECT remaining_seconds
				FROM quota_buckets
				WHERE id=? AND tenant_id=?
				FOR UPDATE
			`, item.bucketID.Int64, tenantID).Scan(&bucketRemaining); err != nil {
				return model.RefundRecord{}, true, err
			}
			item.remaining = bucketRemaining
			refundableSeconds += bucketRemaining
		default:
			item.remaining = 0
		}
	}

	if originalSeconds == 0 || refundableSeconds == 0 {
		return model.RefundRecord{}, true, ErrInsufficientRefundableQuota
	}
	if input.AmountCents > 0 && originalSeconds > ^uint64(0)/input.AmountCents {
		return model.RefundRecord{}, true, ErrRefundAmountInvalid
	}
	numerator := originalSeconds * input.AmountCents
	secondsToReverse := numerator / paidAmount
	if numerator%paidAmount != 0 {
		secondsToReverse++
	}
	if secondsToReverse == 0 {
		secondsToReverse = 1
	}
	if secondsToReverse > refundableSeconds {
		return model.RefundRecord{}, true, ErrInsufficientRefundableQuota
	}

	refundNo := fmt.Sprintf("REF-SIM-%d", now.UnixNano())
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		reason = "Sandbox 时长卡退款"
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO fin_refund_orders (
			refund_no, tenant_id, source_type, source_id,
			currency, refund_amount_cents, refund_method,
			status, reason, operator_user_id,
			idempotency_key, processed_at
		)
		VALUES (
			?, ?, 'order', ?, 'CNY', ?,
			'sandbox_original', 'success', ?, ?, ?, ?
		)
	`,
		refundNo,
		tenantID,
		orderID,
		input.AmountCents,
		reason,
		userID,
		idempotencyKey,
		now,
	)
	if err != nil {
		return model.RefundRecord{}, true, err
	}
	refundID, err := result.LastInsertId()
	if err != nil {
		return model.RefundRecord{}, true, err
	}

	remainingToReverse := secondsToReverse
	activeQuotaChanged := false
	for _, item := range assets {
		if remainingToReverse == 0 {
			break
		}
		if item.remaining == 0 || (item.status != "unactivated" && item.status != "active") {
			continue
		}
		use := item.remaining
		if use > remainingToReverse {
			use = remainingToReverse
		}
		after := item.remaining - use
		assetStatus := item.status
		if after == 0 {
			assetStatus = "refunded"
		}

		if item.status == "active" {
			if !item.bucketID.Valid {
				return model.RefundRecord{}, true, errors.New("active time card asset has no quota bucket")
			}
			bucketStatus := "active"
			if after == 0 {
				bucketStatus = "refunded"
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE quota_buckets
				SET remaining_seconds=?, status=?, updated_at=?
				WHERE id=? AND tenant_id=?
			`, after, bucketStatus, now, item.bucketID.Int64, tenantID); err != nil {
				return model.RefundRecord{}, true, err
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO quota_ledger (
					external_id, tenant_id, bucket_id,
					change_seconds, remaining_before_seconds,
					remaining_after_seconds, business_type, business_id,
					operator_user_id, reason, idempotency_key, occurred_at
				)
				VALUES (?, ?, ?, ?, ?, ?, 'time_card_refund', ?, ?, ?, ?, ?)
			`,
				fmt.Sprintf("time-card-asset-refund-%d-%d", refundID, item.id),
				tenantID,
				item.bucketID.Int64,
				-int64(use),
				item.remaining,
				after,
				refundID,
				userID,
				"时长卡退款冲减 · "+orderNo+" · "+item.assetNo+" · "+reason,
				fmt.Sprintf("time-card-asset-refund-%d-%d", refundID, item.id),
				now,
			); err != nil {
				return model.RefundRecord{}, true, err
			}
			activeQuotaChanged = true
		}

		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_time_card_assets
			SET remaining_seconds=?, status=?, updated_at=?
			WHERE id=?
		`, after, assetStatus, now, item.id); err != nil {
			return model.RefundRecord{}, true, err
		}
		remainingToReverse -= use
	}
	if remainingToReverse != 0 {
		return model.RefundRecord{}, true, ErrInsufficientRefundableQuota
	}

	if activeQuotaChanged {
		if err := reconcileCustomerAIResourceToQuotaTx(
			ctx,
			tx,
			tenantID,
			now,
			"time_card_refund",
			"时长卡退款 · "+orderNo,
		); err != nil {
			return model.RefundRecord{}, true, err
		}
	}

	newRefundedAmount := refundedAmount + input.AmountCents
	newOrderStatus := "partially_refunded"
	if newRefundedAmount >= paidAmount {
		newOrderStatus = "refunded"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_orders
		SET refunded_amount_cents=?, status=?
		WHERE id=? AND tenant_id=?
	`, newRefundedAmount, newOrderStatus, orderID, tenantID); err != nil {
		return model.RefundRecord{}, true, err
	}

	processedAt := now
	return model.RefundRecord{
		ID:                refundID,
		RefundNo:          refundNo,
		SourceType:        "order",
		SourceID:          orderID,
		RefundAmountCents: input.AmountCents,
		RefundMethod:      "sandbox_original",
		Status:            "success",
		Reason:            reason,
		ProcessedAt:       &processedAt,
		CreatedAt:         now,
	}, true, nil
}

// All activation/expiry/refund mutations for one tenant serialize on the
// tenant AI resource account. This prevents two rooms starting at the same
// time from activating two reserve cards before either transaction can see
// the other's newly created quota bucket.
func lockTenantAIResourceTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, 'ai_seconds', 'seconds', 0, 0, 'active')
	`, tenantID); err != nil {
		return err
	}
	var accountID int64
	return tx.QueryRowContext(ctx, `
		SELECT id
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type='ai_seconds'
		FOR UPDATE
	`, tenantID).Scan(&accountID)
}
