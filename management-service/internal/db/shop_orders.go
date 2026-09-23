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

var (
	ErrShopOrderCancelled          = errors.New("shop order cancelled")
	ErrShopOrderAlreadyPaid        = errors.New("shop order already paid")
	ErrSandboxAmountMismatch       = errors.New("sandbox payment amount mismatch")
	ErrSandboxPaymentFailed        = errors.New("sandbox payment failed")
	ErrUnsupportedShopProduct      = errors.New("unsupported shop product")
	ErrShopOrderNotPaid            = errors.New("shop order not paid")
	ErrShopOrderFullyRefunded      = errors.New("shop order fully refunded")
	ErrRefundAmountInvalid         = errors.New("refund amount invalid")
	ErrInsufficientRefundableQuota = errors.New("insufficient refundable quota")
	ErrInsufficientDeviceStock     = errors.New("insufficient device stock")
	ErrDeviceShippingRequired      = errors.New("device shipping required")
)

func (s *Store) ListCustomerShopOrders(
	ctx context.Context,
	tenantID int64,
	limit int,
) ([]model.CustomerShopOrder, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM biz_orders
		WHERE tenant_id=? AND order_type IN ('time_card', 'device')
		ORDER BY created_at DESC, id DESC
		LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]model.CustomerShopOrder, 0, len(ids))
	for _, id := range ids {
		item, err := s.GetCustomerShopOrder(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) GetCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	orderID int64,
) (model.CustomerShopOrder, error) {
	var item model.CustomerShopOrder
	err := s.db.QueryRowContext(ctx, `
		SELECT id, order_no, tenant_id, order_type, status, currency,
		       list_amount_cents, discount_amount_cents, payable_amount_cents,
		       paid_amount_cents, refunded_amount_cents,
		       paid_at, cancelled_at, created_at, updated_at
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		LIMIT 1
	`, orderID, tenantID).Scan(
		&item.ID,
		&item.OrderNo,
		&item.TenantID,
		&item.OrderType,
		&item.Status,
		&item.Currency,
		&item.ListAmountCents,
		&item.DiscountAmountCents,
		&item.PayableAmountCents,
		&item.PaidAmountCents,
		&item.RefundedAmountCents,
		&item.PaidAt,
		&item.CancelledAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	item.Items, err = s.listCustomerShopOrderItems(ctx, orderID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	item.Payments, err = s.listSandboxPayments(ctx, tenantID, orderID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	if item.OrderType == "device" {
		item.Shipping, err = s.loadCustomerOrderShipping(ctx, orderID)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
		item.Devices, err = s.listCustomerOrderDevices(ctx, orderID)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
		item.Shipments, err = s.listOrderShipments(ctx, orderID)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
	}

	switch item.Status {
	case "paid", "fulfilled", "completed", "partially_refunded", "refunded":
		item.PaymentStatus = "paid"
	case "cancelled":
		item.PaymentStatus = "cancelled"
	default:
		item.PaymentStatus = "pending"
	}

	item.FulfillmentStatus = "pending"
	if item.Status == "cancelled" {
		item.FulfillmentStatus = "cancelled"
	} else if item.PaymentStatus == "paid" {
		if item.OrderType == "time_card" {
			var (
				bucketCount      int
				originalSeconds  uint64
				remainingSeconds uint64
			)
			err := s.db.QueryRowContext(ctx, `
				SELECT COUNT(*),
				       COALESCE(SUM(original_seconds), 0),
				       COALESCE(SUM(remaining_seconds), 0)
				FROM quota_buckets
				WHERE tenant_id=? AND source_type='time_card_purchase' AND source_id=?
			`, tenantID, orderID).Scan(
				&bucketCount,
				&originalSeconds,
				&remainingSeconds,
			)
			if err != nil {
				return model.CustomerShopOrder{}, err
			}
			if bucketCount > 0 {
				item.FulfillmentStatus = "fulfilled"
				item.RefundableSeconds = remainingSeconds
				if item.PaidAmountCents > item.RefundedAmountCents &&
					originalSeconds > 0 {
					remainingMoney := item.PaidAmountCents - item.RefundedAmountCents
					quotaMoney := item.PaidAmountCents * remainingSeconds / originalSeconds
					if quotaMoney < remainingMoney {
						item.RefundableAmountCents = quotaMoney
					} else {
						item.RefundableAmountCents = remainingMoney
					}
				}
			} else {
				item.FulfillmentStatus = "pending_fulfillment"
			}
		} else if item.OrderType == "device" {
			item.FulfillmentStatus = "pending_fulfillment"
			if len(item.Devices) > 0 {
				item.FulfillmentStatus = "reserved"
			}
			if len(item.Shipments) > 0 {
				latest := item.Shipments[len(item.Shipments)-1]
				switch latest.Status {
				case "pending", "ready_to_ship":
					item.FulfillmentStatus = "pending_shipment"
				case "shipped", "in_transit":
					item.FulfillmentStatus = "shipping"
				case "delivered":
					item.FulfillmentStatus = "fulfilled"
				case "exception":
					item.FulfillmentStatus = "logistics_exception"
				case "returned":
					item.FulfillmentStatus = "returned"
				case "cancelled":
					item.FulfillmentStatus = "shipment_cancelled"
				}
			}
		} else {
			item.FulfillmentStatus = "pending_fulfillment"
		}
	}
	return item, nil
}

func (s *Store) listCustomerShopOrderItems(
	ctx context.Context,
	orderID int64,
) ([]model.CustomerShopOrderItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, order_id, product_type, product_id, product_version_id,
		       product_name_snapshot, quantity,
		       COALESCE(duration_seconds_snapshot, 0),
		       COALESCE(validity_days_snapshot, 0),
		       unit_list_price_cents, unit_paid_price_cents,
		       discount_bps_snapshot, created_at
		FROM biz_order_items
		WHERE order_id=?
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CustomerShopOrderItem, 0)
	for rows.Next() {
		var item model.CustomerShopOrderItem
		if err := rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.ProductType,
			&item.ProductID,
			&item.ProductVersionID,
			&item.ProductName,
			&item.Quantity,
			&item.DurationSeconds,
			&item.ValidityDays,
			&item.UnitListPriceCents,
			&item.UnitPaidPriceCents,
			&item.DiscountBPS,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listSandboxPayments(
	ctx context.Context,
	tenantID int64,
	orderID int64,
) ([]model.SandboxPaymentRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, payment_no, tenant_id, order_id, order_no,
		       channel, payment_method, currency, expected_amount_cents,
		       input_amount_cents, paid_amount_cents, status, failure_reason,
		       external_trade_no, operator_user_id, idempotency_key,
		       paid_at, created_at, updated_at
		FROM fin_payment_transactions
		WHERE tenant_id=? AND order_id=?
		ORDER BY id DESC
	`, tenantID, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.SandboxPaymentRecord, 0)
	for rows.Next() {
		var item model.SandboxPaymentRecord
		if err := rows.Scan(
			&item.ID,
			&item.PaymentNo,
			&item.TenantID,
			&item.OrderID,
			&item.OrderNo,
			&item.Channel,
			&item.PaymentMethod,
			&item.Currency,
			&item.ExpectedAmountCents,
			&item.InputAmountCents,
			&item.PaidAmountCents,
			&item.Status,
			&item.FailureReason,
			&item.ExternalTradeNo,
			&item.OperatorUserID,
			&item.IdempotencyKey,
			&item.PaidAt,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	userID int64,
	input model.CreateCustomerShopOrderInput,
) (model.CustomerShopOrder, error) {
	if input.ProductType == "device" {
		return s.createDeviceCustomerShopOrder(ctx, tenantID, userID, input)
	}
	if input.ProductType != "time_card" {
		return model.CustomerShopOrder{}, ErrUnsupportedShopProduct
	}
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.Quantity > 100 {
		return model.CustomerShopOrder{}, fmt.Errorf("quantity too large")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey != "" {
		var existingID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM biz_orders
			WHERE tenant_id=? AND idempotency_key=?
			LIMIT 1
		`, tenantID, idempotencyKey).Scan(&existingID)
		if err == nil {
			_ = tx.Rollback()
			return s.GetCustomerShopOrder(ctx, tenantID, existingID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.CustomerShopOrder{}, err
		}
	}

	var (
		productName      string
		productVersionID int64
		versionNo        uint32
		listPrice        uint64
		durationSeconds  uint64
		validityDays     uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT p.name, v.id, v.version_no, v.price_cents,
		       v.duration_seconds, v.validity_days
		FROM catalog_time_card_products p
		INNER JOIN catalog_time_card_versions v ON v.product_id=p.id
		WHERE p.id=? AND p.status='active'
		  AND v.lifecycle_status='published'
		  AND (v.effective_from IS NULL OR v.effective_from <= CURRENT_TIMESTAMP(3))
		  AND (v.effective_to IS NULL OR v.effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY v.version_no DESC
		LIMIT 1
		FOR UPDATE
	`, input.ProductID).Scan(
		&productName,
		&productVersionID,
		&versionNo,
		&listPrice,
		&durationSeconds,
		&validityDays,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	discountBPS := uint32(10000)
	var membershipID sql.NullInt64
	var membershipPlanVersionID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT m.id, m.plan_version_id, v.default_time_card_discount_bps
		FROM biz_memberships m
		INNER JOIN catalog_membership_plan_versions v ON v.id=m.plan_version_id
		WHERE m.tenant_id=? AND m.status='active'
		  AND m.cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY m.cycle_end_at DESC, m.id DESC
		LIMIT 1
	`, tenantID).Scan(
		&membershipID,
		&membershipPlanVersionID,
		&discountBPS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		discountBPS = 10000
		membershipID = sql.NullInt64{}
		membershipPlanVersionID = sql.NullInt64{}
	} else if err != nil {
		return model.CustomerShopOrder{}, err
	}
	if discountBPS == 0 || discountBPS > 10000 {
		discountBPS = 10000
	}

	relations, err := loadOrderRelationSnapshotTx(ctx, tx, tenantID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	unitPaidPrice := listPrice * uint64(discountBPS) / 10000
	quantity := uint64(input.Quantity)
	listAmount := listPrice * quantity
	payableAmount := unitPaidPrice * quantity
	discountAmount := listAmount - payableAmount
	orderNo := fmt.Sprintf("ORD-%d", time.Now().UTC().UnixNano())
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("shop-order-%d-%d", tenantID, time.Now().UTC().UnixNano())
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO biz_orders (
			order_no, tenant_id, order_type, status, currency,
			list_amount_cents, discount_amount_cents, payable_amount_cents,
			paid_amount_cents, refunded_amount_cents,
			membership_id_snapshot, membership_plan_version_id_snapshot,
			sales_assignment_id_snapshot, sales_staff_id_snapshot,
			agent_relation_id_snapshot, agent_org_id_snapshot,
			referral_relation_id_snapshot, referrer_tenant_id_snapshot,
			pricing_snapshot_json, relation_snapshot_json, idempotency_key
		)
		VALUES (
			?, ?, 'time_card', 'pending', 'CNY',
			?, ?, ?, 0, 0, ?, ?,
			?, ?, ?, ?, ?, ?,
			JSON_OBJECT(
				'product_id', ?,
				'product_version_id', ?,
				'version_no', ?,
				'discount_bps', ?,
				'quantity', ?
			),
			JSON_OBJECT(
				'sales_assignment_id', ?,
				'sales_staff_id', ?,
				'agent_relation_id', ?,
				'agent_org_id', ?,
				'referral_relation_id', ?,
				'referrer_tenant_id', ?
			),
			?
		)
	`,
		orderNo,
		tenantID,
		listAmount,
		discountAmount,
		payableAmount,
		nullableInt64Value(membershipID),
		nullableInt64Value(membershipPlanVersionID),
		nullableInt64Value(relations.SalesAssignmentID),
		nullableInt64Value(relations.SalesStaffID),
		nullableInt64Value(relations.AgentRelationID),
		nullableInt64Value(relations.AgentOrgID),
		nullableInt64Value(relations.ReferralRelationID),
		nullableInt64Value(relations.ReferrerTenantID),
		input.ProductID,
		productVersionID,
		versionNo,
		discountBPS,
		input.Quantity,
		nullableInt64Value(relations.SalesAssignmentID),
		nullableInt64Value(relations.SalesStaffID),
		nullableInt64Value(relations.AgentRelationID),
		nullableInt64Value(relations.AgentOrgID),
		nullableInt64Value(relations.ReferralRelationID),
		nullableInt64Value(relations.ReferrerTenantID),
		idempotencyKey,
	)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO biz_order_items (
			order_id, product_type, product_id, product_version_id,
			product_name_snapshot, quantity, duration_seconds_snapshot,
			validity_days_snapshot, unit_list_price_cents,
			unit_paid_price_cents, discount_bps_snapshot,
			metadata_json
		)
		VALUES (
			?, 'time_card', ?, ?, ?, ?, ?, ?, ?, ?, ?,
			JSON_OBJECT('source', 'customer_shop')
		)
	`,
		orderID,
		input.ProductID,
		productVersionID,
		productName,
		input.Quantity,
		durationSeconds,
		validityDays,
		listPrice,
		unitPaidPrice,
		discountBPS,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

func (s *Store) SandboxPayCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	userID int64,
	orderID int64,
	input model.SandboxPayOrderInput,
) (model.CustomerShopOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()

	var (
		orderNo       string
		orderType     string
		status        string
		currency      string
		payableAmount uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT order_no, order_type, status, currency, payable_amount_cents
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		FOR UPDATE
	`, orderID, tenantID).Scan(
		&orderNo,
		&orderType,
		&status,
		&currency,
		&payableAmount,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if status == "paid" || status == "fulfilled" || status == "completed" {
		_ = tx.Rollback()
		return s.GetCustomerShopOrder(ctx, tenantID, orderID)
	}
	if status == "cancelled" {
		return model.CustomerShopOrder{}, ErrShopOrderCancelled
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf(
			"sandbox-pay-%d-%d",
			orderID,
			time.Now().UTC().UnixNano(),
		)
	}
	var existingPaymentID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM fin_payment_transactions
		WHERE idempotency_key=?
		LIMIT 1
	`, idempotencyKey).Scan(&existingPaymentID)
	if err == nil {
		_ = tx.Rollback()
		return s.GetCustomerShopOrder(ctx, tenantID, orderID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.CustomerShopOrder{}, err
	}

	paymentNo := fmt.Sprintf("PAY-SIM-%d", time.Now().UTC().UnixNano())
	externalTradeNo := fmt.Sprintf("SIM-%d", time.Now().UTC().UnixNano())
	simulateResult := strings.ToLower(strings.TrimSpace(input.SimulateResult))
	if simulateResult == "" {
		simulateResult = "success"
	}

	paymentStatus := "paid"
	failureReason := ""
	paidAmount := input.AmountCents
	var paidAt any = time.Now().UTC()

	switch {
	case simulateResult == "failure":
		paymentStatus = "failed"
		failureReason = "simulated_failure"
		paidAmount = 0
		paidAt = nil
	case input.AmountCents != payableAmount:
		paymentStatus = "failed"
		failureReason = "amount_mismatch"
		paidAmount = 0
		paidAt = nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fin_payment_transactions (
			payment_no, tenant_id, order_id, order_no,
			channel, payment_method, currency,
			expected_amount_cents, input_amount_cents, paid_amount_cents,
			status, failure_reason, external_trade_no,
			operator_user_id, idempotency_key, paid_at
		)
		VALUES (
			?, ?, ?, ?, 'sandbox', 'sandbox_manual', ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`,
		paymentNo,
		tenantID,
		orderID,
		orderNo,
		currency,
		payableAmount,
		input.AmountCents,
		paidAmount,
		paymentStatus,
		failureReason,
		externalTradeNo,
		userID,
		idempotencyKey,
		paidAt,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if paymentStatus != "paid" {
		if err := tx.Commit(); err != nil {
			return model.CustomerShopOrder{}, err
		}
		if failureReason == "amount_mismatch" {
			return model.CustomerShopOrder{}, ErrSandboxAmountMismatch
		}
		return model.CustomerShopOrder{}, ErrSandboxPaymentFailed
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_orders
		SET status='paid',
		    paid_amount_cents=?,
		    paid_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND status='pending'
	`, paidAmount, orderID, tenantID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	switch orderType {
	case "time_card":
		if err := fulfillTimeCardOrderTx(
			ctx,
			tx,
			tenantID,
			userID,
			orderID,
			orderNo,
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
	case "device":
		if err := fulfillDeviceOrderTx(
			ctx,
			tx,
			tenantID,
			userID,
			orderID,
			orderNo,
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
	default:
		return model.CustomerShopOrder{}, ErrUnsupportedShopProduct
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

func fulfillTimeCardOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	userID int64,
	orderID int64,
	orderNo string,
) error {
	var (
		quantity        uint64
		durationSeconds uint64
		validityDays    uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT quantity, COALESCE(duration_seconds_snapshot, 0),
		       COALESCE(validity_days_snapshot, 0)
		FROM biz_order_items
		WHERE order_id=? AND product_type='time_card'
		ORDER BY id ASC
		LIMIT 1
	`, orderID).Scan(
		&quantity,
		&durationSeconds,
		&validityDays,
	); err != nil {
		return err
	}

	totalSeconds := durationSeconds * quantity
	if totalSeconds == 0 || validityDays == 0 {
		return fmt.Errorf("invalid time card fulfillment snapshot")
	}

	now := time.Now().UTC()
	expiresAt := now.AddDate(0, 0, int(validityDays))
	bucketExternalID := fmt.Sprintf("time-card-order-%d", orderID)
	result, err := tx.ExecContext(ctx, `
		INSERT INTO quota_buckets (
			external_id, tenant_id, source_type, source_id,
			original_seconds, remaining_seconds, effective_at,
			expires_at, priority, status,
			metadata_json
		)
		VALUES (
			?, ?, 'time_card_purchase', ?,
			?, ?, ?, ?, 50, 'active',
			JSON_OBJECT('order_no', ?, 'sandbox_payment', true)
		)
	`,
		bucketExternalID,
		tenantID,
		orderID,
		totalSeconds,
		totalSeconds,
		now,
		expiresAt,
		orderNo,
	)
	if err != nil {
		return err
	}
	bucketID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quota_ledger (
			external_id, tenant_id, bucket_id,
			change_seconds, remaining_before_seconds,
			remaining_after_seconds, business_type, business_id,
			operator_user_id, reason, idempotency_key, occurred_at
		)
		VALUES (
			?, ?, ?, ?, 0, ?,
			'time_card_purchase', ?, ?, ?, ?, ?
		)
	`,
		fmt.Sprintf("time-card-credit-%d", orderID),
		tenantID,
		bucketID,
		int64(totalSeconds),
		totalSeconds,
		orderID,
		userID,
		"时长卡模拟支付入账 · "+orderNo,
		fmt.Sprintf("time-card-credit-%d", orderID),
		now,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO org_resource_accounts (
			organization_id, resource_type, unit, balance, reserved, status
		)
		VALUES (?, 'ai_seconds', 'seconds', 0, 0, 'active')
	`, tenantID); err != nil {
		return err
	}
	var resourceBefore int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=? AND resource_type='ai_seconds'
		FOR UPDATE
	`, tenantID).Scan(&resourceBefore); err != nil {
		return err
	}
	resourceAfter := resourceBefore + int64(totalSeconds)
	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='ai_seconds'
	`, resourceAfter, tenantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id, resource_type, change_quantity,
			balance_before, balance_after, business_type,
			operator_user_id, reason
		)
		VALUES (?, 'ai_seconds', ?, ?, ?, 'time_card_purchase', ?, ?)
	`,
		tenantID,
		int64(totalSeconds),
		resourceBefore,
		resourceAfter,
		userID,
		"时长卡订单 "+orderNo+" 模拟支付入账",
	); err != nil {
		return err
	}
	return nil
}

func (s *Store) SandboxRefundCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	userID int64,
	orderID int64,
	input model.SandboxRefundOrderInput,
) (model.CustomerShopOrder, model.RefundRecord, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	defer tx.Rollback()

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf(
			"sandbox-refund-%d-%d",
			orderID,
			time.Now().UTC().UnixNano(),
		)
	}

	var existing model.RefundRecord
	err = tx.QueryRowContext(ctx, `
		SELECT id, refund_no, source_type, source_id,
		       refund_amount_cents, refund_method, status,
		       reason, processed_at, created_at
		FROM fin_refund_orders
		WHERE tenant_id=? AND idempotency_key=?
		LIMIT 1
	`, tenantID, idempotencyKey).Scan(
		&existing.ID,
		&existing.RefundNo,
		&existing.SourceType,
		&existing.SourceID,
		&existing.RefundAmountCents,
		&existing.RefundMethod,
		&existing.Status,
		&existing.Reason,
		&existing.ProcessedAt,
		&existing.CreatedAt,
	)
	if err == nil {
		_ = tx.Rollback()
		order, orderErr := s.GetCustomerShopOrder(ctx, tenantID, orderID)
		return order, existing, orderErr
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	var (
		orderNo        string
		orderType      string
		status         string
		paidAmount     uint64
		refundedAmount uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT order_no, order_type, status,
		       paid_amount_cents, refunded_amount_cents
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		FOR UPDATE
	`, orderID, tenantID).Scan(
		&orderNo,
		&orderType,
		&status,
		&paidAmount,
		&refundedAmount,
	); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	if orderType != "time_card" {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrUnsupportedShopProduct
	}
	switch status {
	case "paid", "partially_refunded":
	default:
		if status == "refunded" {
			return model.CustomerShopOrder{}, model.RefundRecord{}, ErrShopOrderFullyRefunded
		}
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrShopOrderNotPaid
	}
	if paidAmount == 0 || refundedAmount >= paidAmount {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrShopOrderFullyRefunded
	}

	outstandingMoney := paidAmount - refundedAmount
	if input.AmountCents == 0 || input.AmountCents > outstandingMoney {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrRefundAmountInvalid
	}

	var (
		bucketID         int64
		originalSeconds  uint64
		remainingSeconds uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, original_seconds, remaining_seconds
		FROM quota_buckets
		WHERE tenant_id=?
		  AND source_type='time_card_purchase'
		  AND source_id=?
		ORDER BY id ASC
		LIMIT 1
		FOR UPDATE
	`, tenantID, orderID).Scan(
		&bucketID,
		&originalSeconds,
		&remainingSeconds,
	); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	if originalSeconds == 0 || remainingSeconds == 0 {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrInsufficientRefundableQuota
	}
	if input.AmountCents > 0 &&
		originalSeconds > ^uint64(0)/input.AmountCents {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrRefundAmountInvalid
	}

	numerator := originalSeconds * input.AmountCents
	secondsToReverse := numerator / paidAmount
	if numerator%paidAmount != 0 {
		secondsToReverse++
	}
	if secondsToReverse == 0 {
		secondsToReverse = 1
	}
	if secondsToReverse > remainingSeconds {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrInsufficientRefundableQuota
	}

	var resourceBefore int64
	if err := tx.QueryRowContext(ctx, `
		SELECT balance
		FROM org_resource_accounts
		WHERE organization_id=?
		  AND resource_type='ai_seconds'
		  AND status='active'
		FOR UPDATE
	`, tenantID).Scan(&resourceBefore); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	if resourceBefore < int64(secondsToReverse) {
		return model.CustomerShopOrder{}, model.RefundRecord{}, ErrInsufficientRefundableQuota
	}

	now := time.Now().UTC()
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
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	refundID, err := result.LastInsertId()
	if err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	remainingAfter := remainingSeconds - secondsToReverse
	bucketStatus := "active"
	if remainingAfter == 0 {
		bucketStatus = "refunded"
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE quota_buckets
		SET remaining_seconds=?, status=?
		WHERE id=?
	`, remainingAfter, bucketStatus, bucketID); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	var originalLedgerID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM quota_ledger
		WHERE tenant_id=?
		  AND bucket_id=?
		  AND business_type='time_card_purchase'
		  AND business_id=?
		ORDER BY id ASC
		LIMIT 1
	`, tenantID, bucketID, orderID).Scan(&originalLedgerID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO quota_ledger (
			external_id, tenant_id, bucket_id,
			change_seconds, remaining_before_seconds,
			remaining_after_seconds, business_type, business_id,
			reversal_of_entry_id, operator_user_id, reason,
			idempotency_key, occurred_at
		)
		VALUES (
			?, ?, ?, ?, ?, ?,
			'time_card_refund', ?, ?, ?, ?, ?, ?
		)
	`,
		fmt.Sprintf("time-card-refund-%d", refundID),
		tenantID,
		bucketID,
		-int64(secondsToReverse),
		remainingSeconds,
		remainingAfter,
		refundID,
		nullableInt64Value(originalLedgerID),
		userID,
		"时长卡退款冲减 · "+orderNo+" · "+reason,
		fmt.Sprintf("time-card-refund-%d", refundID),
		now,
	); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	resourceAfter := resourceBefore - int64(secondsToReverse)
	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='ai_seconds'
	`, resourceAfter, tenantID); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO org_resource_ledger (
			organization_id, resource_type, change_quantity,
			balance_before, balance_after, business_type,
			operator_user_id, reason
		)
		VALUES (?, 'ai_seconds', ?, ?, ?, 'time_card_refund', ?, ?)
	`,
		tenantID,
		-int64(secondsToReverse),
		resourceBefore,
		resourceAfter,
		userID,
		"时长卡退款 "+refundNo+" · 原订单 "+orderNo,
	); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
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
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}

	order, err := s.GetCustomerShopOrder(ctx, tenantID, orderID)
	if err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	processedAt := now
	refund := model.RefundRecord{
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
	}
	return order, refund, nil
}

func (s *Store) CancelCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	orderID int64,
) (model.CustomerShopOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `
		SELECT status
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		FOR UPDATE
	`, orderID, tenantID).Scan(&status); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if status == "paid" || status == "fulfilled" || status == "completed" {
		return model.CustomerShopOrder{}, ErrShopOrderAlreadyPaid
	}
	if status == "cancelled" {
		_ = tx.Rollback()
		return s.GetCustomerShopOrder(ctx, tenantID, orderID)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_orders
		SET status='cancelled', cancelled_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=?
	`, orderID, tenantID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

type orderRelationSnapshot struct {
	SalesAssignmentID  sql.NullInt64
	SalesStaffID       sql.NullInt64
	AgentRelationID    sql.NullInt64
	AgentOrgID         sql.NullInt64
	ReferralRelationID sql.NullInt64
	ReferrerTenantID   sql.NullInt64
}

func loadOrderRelationSnapshotTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
) (orderRelationSnapshot, error) {
	var result orderRelationSnapshot

	err := tx.QueryRowContext(ctx, `
		SELECT id, sales_staff_id
		FROM crm_customer_sales_assignments
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_from <= CURRENT_TIMESTAMP(3)
		  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY effective_from DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(
		&result.SalesAssignmentID,
		&result.SalesStaffID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return orderRelationSnapshot{}, err
	}

	err = tx.QueryRowContext(ctx, `
		SELECT id, agent_org_id
		FROM crm_customer_agent_relations
		WHERE tenant_id=?
		  AND status='active'
		  AND effective_from <= CURRENT_TIMESTAMP(3)
		  AND (effective_to IS NULL OR effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY effective_from DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(
		&result.AgentRelationID,
		&result.AgentOrgID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return orderRelationSnapshot{}, err
	}

	err = tx.QueryRowContext(ctx, `
		SELECT id, referrer_tenant_id
		FROM crm_referral_relations
		WHERE referred_tenant_id=?
		  AND status='active'
		  AND (commission_start_at IS NULL OR commission_start_at <= CURRENT_TIMESTAMP(3))
		  AND (commission_end_at IS NULL OR commission_end_at > CURRENT_TIMESTAMP(3))
		ORDER BY bound_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(
		&result.ReferralRelationID,
		&result.ReferrerTenantID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return orderRelationSnapshot{}, err
	}

	return result, nil
}

func nullableInt64Value(value sql.NullInt64) any {
	if value.Valid {
		return value.Int64
	}
	return nil
}

func (s *Store) createDeviceCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	userID int64,
	input model.CreateCustomerShopOrderInput,
) (model.CustomerShopOrder, error) {
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.Quantity > 100 {
		return model.CustomerShopOrder{}, fmt.Errorf("quantity too large")
	}

	input.RecipientName = strings.TrimSpace(input.RecipientName)
	input.RecipientPhone = strings.TrimSpace(input.RecipientPhone)
	input.Province = strings.TrimSpace(input.Province)
	input.City = strings.TrimSpace(input.City)
	input.District = strings.TrimSpace(input.District)
	input.Address = strings.TrimSpace(input.Address)
	if input.RecipientName == "" ||
		input.RecipientPhone == "" ||
		input.Province == "" ||
		input.City == "" ||
		input.District == "" ||
		input.Address == "" {
		return model.CustomerShopOrder{}, ErrDeviceShippingRequired
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey != "" {
		var existingID int64
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM biz_orders
			WHERE tenant_id=? AND idempotency_key=?
			LIMIT 1
		`, tenantID, idempotencyKey).Scan(&existingID)
		if err == nil {
			_ = tx.Rollback()
			return s.GetCustomerShopOrder(ctx, tenantID, existingID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.CustomerShopOrder{}, err
		}
	}

	var (
		productName      string
		skuCode          string
		productVersionID int64
		versionNo        uint32
		listPrice        uint64
		salePrice        uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT p.name, p.sku_code, v.id, v.version_no,
		       v.list_price_cents, v.sale_price_cents
		FROM catalog_device_products p
		INNER JOIN catalog_device_versions v ON v.product_id=p.id
		WHERE p.id=? AND p.status='active'
		  AND v.lifecycle_status='published'
		  AND (v.effective_from IS NULL OR v.effective_from <= CURRENT_TIMESTAMP(3))
		  AND (v.effective_to IS NULL OR v.effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY v.version_no DESC
		LIMIT 1
		FOR UPDATE
	`, input.ProductID).Scan(
		&productName,
		&skuCode,
		&productVersionID,
		&versionNo,
		&listPrice,
		&salePrice,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if salePrice == 0 || listPrice == 0 || salePrice > listPrice {
		return model.CustomerShopOrder{}, fmt.Errorf("invalid device product pricing")
	}

	var stockCount uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_devices
		WHERE sku_code=? AND lifecycle_status='IN_STOCK'
	`, skuCode).Scan(&stockCount); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if stockCount < input.Quantity {
		return model.CustomerShopOrder{}, ErrInsufficientDeviceStock
	}

	var membershipID sql.NullInt64
	var membershipPlanVersionID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT id, plan_version_id
		FROM biz_memberships
		WHERE tenant_id=? AND status='active'
		  AND cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY cycle_end_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&membershipID, &membershipPlanVersionID)
	if errors.Is(err, sql.ErrNoRows) {
		membershipID = sql.NullInt64{}
		membershipPlanVersionID = sql.NullInt64{}
	} else if err != nil {
		return model.CustomerShopOrder{}, err
	}

	relations, err := loadOrderRelationSnapshotTx(ctx, tx, tenantID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	discountBPS := uint32(salePrice * 10000 / listPrice)
	if discountBPS == 0 {
		discountBPS = 1
	}
	quantity := uint64(input.Quantity)
	listAmount := listPrice * quantity
	payableAmount := salePrice * quantity
	discountAmount := listAmount - payableAmount
	orderNo := fmt.Sprintf("ORD-DEV-%d", time.Now().UTC().UnixNano())
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf(
			"shop-device-order-%d-%d",
			tenantID,
			time.Now().UTC().UnixNano(),
		)
	}

	result, err := tx.ExecContext(ctx, `
		INSERT INTO biz_orders (
			order_no, tenant_id, order_type, status, currency,
			list_amount_cents, discount_amount_cents, payable_amount_cents,
			paid_amount_cents, refunded_amount_cents,
			membership_id_snapshot, membership_plan_version_id_snapshot,
			sales_assignment_id_snapshot, sales_staff_id_snapshot,
			agent_relation_id_snapshot, agent_org_id_snapshot,
			referral_relation_id_snapshot, referrer_tenant_id_snapshot,
			pricing_snapshot_json, relation_snapshot_json, idempotency_key
		)
		VALUES (
			?, ?, 'device', 'pending', 'CNY',
			?, ?, ?, 0, 0, ?, ?,
			?, ?, ?, ?, ?, ?,
			JSON_OBJECT(
				'product_id', ?,
				'product_version_id', ?,
				'version_no', ?,
				'sku_code', ?,
				'list_price_cents', ?,
				'sale_price_cents', ?,
				'discount_bps', ?,
				'quantity', ?
			),
			JSON_OBJECT(
				'sales_assignment_id', ?,
				'sales_staff_id', ?,
				'agent_relation_id', ?,
				'agent_org_id', ?,
				'referral_relation_id', ?,
				'referrer_tenant_id', ?
			),
			?
		)
	`,
		orderNo,
		tenantID,
		listAmount,
		discountAmount,
		payableAmount,
		nullableInt64Value(membershipID),
		nullableInt64Value(membershipPlanVersionID),
		nullableInt64Value(relations.SalesAssignmentID),
		nullableInt64Value(relations.SalesStaffID),
		nullableInt64Value(relations.AgentRelationID),
		nullableInt64Value(relations.AgentOrgID),
		nullableInt64Value(relations.ReferralRelationID),
		nullableInt64Value(relations.ReferrerTenantID),
		input.ProductID,
		productVersionID,
		versionNo,
		skuCode,
		listPrice,
		salePrice,
		discountBPS,
		input.Quantity,
		nullableInt64Value(relations.SalesAssignmentID),
		nullableInt64Value(relations.SalesStaffID),
		nullableInt64Value(relations.AgentRelationID),
		nullableInt64Value(relations.AgentOrgID),
		nullableInt64Value(relations.ReferralRelationID),
		nullableInt64Value(relations.ReferrerTenantID),
		idempotencyKey,
	)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	orderID, err := result.LastInsertId()
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	itemResult, err := tx.ExecContext(ctx, `
		INSERT INTO biz_order_items (
			order_id, product_type, product_id, product_version_id,
			product_name_snapshot, quantity, duration_seconds_snapshot,
			validity_days_snapshot, unit_list_price_cents,
			unit_paid_price_cents, discount_bps_snapshot,
			metadata_json
		)
		VALUES (
			?, 'device', ?, ?, ?, ?, NULL, NULL, ?, ?, ?,
			JSON_OBJECT(
				'source', 'customer_shop',
				'sku_code', ?
			)
		)
	`,
		orderID,
		input.ProductID,
		productVersionID,
		productName,
		input.Quantity,
		listPrice,
		salePrice,
		discountBPS,
		skuCode,
	)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	if _, err := itemResult.LastInsertId(); err != nil {
		return model.CustomerShopOrder{}, err
	}

	fullAddress := strings.TrimSpace(strings.Join(
		[]string{input.Province, input.City, input.District, input.Address},
		" ",
	))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO biz_order_shipping (
			order_id, recipient_name, recipient_phone,
			province, city, district, address, full_address
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orderID,
		input.RecipientName,
		input.RecipientPhone,
		input.Province,
		input.City,
		input.District,
		input.Address,
		fullAddress,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

func fulfillDeviceOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	userID int64,
	orderID int64,
	orderNo string,
) error {
	var (
		orderItemID int64
		quantity    uint32
		skuCode     string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT i.id, i.quantity, p.sku_code
		FROM biz_order_items i
		INNER JOIN catalog_device_products p ON p.id=i.product_id
		WHERE i.order_id=? AND i.product_type='device'
		ORDER BY i.id ASC
		LIMIT 1
	`, orderID).Scan(&orderItemID, &quantity, &skuCode); err != nil {
		return err
	}
	if quantity == 0 {
		return ErrInsufficientDeviceStock
	}

	var warehouseID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT custody_warehouse_id
		FROM inv_devices
		WHERE sku_code=?
		  AND lifecycle_status='IN_STOCK'
		  AND custody_warehouse_id IS NOT NULL
		GROUP BY custody_warehouse_id
		HAVING COUNT(*) >= ?
		ORDER BY COUNT(*) DESC, custody_warehouse_id ASC
		LIMIT 1
	`, skuCode, quantity).Scan(&warehouseID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInsufficientDeviceStock
		}
		return err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, sn, sku_code, lifecycle_status, custody_warehouse_id,
		       owner_org_id, current_customer_id
		FROM inv_devices
		WHERE sku_code=? AND lifecycle_status='IN_STOCK'
		  AND custody_warehouse_id=?
		ORDER BY id ASC
		LIMIT ?
		FOR UPDATE
	`, skuCode, warehouseID, quantity)
	if err != nil {
		return err
	}
	snapshots := make([]shipmentDeviceSnapshot, 0, quantity)
	deviceIDs := make([]int64, 0, quantity)
	for rows.Next() {
		var snapshot shipmentDeviceSnapshot
		if err := rows.Scan(
			&snapshot.ID,
			&snapshot.SN,
			&snapshot.SKU,
			&snapshot.Status,
			&snapshot.WarehouseID,
			&snapshot.OwnerOrgID,
			&snapshot.CustomerID,
		); err != nil {
			rows.Close()
			return err
		}
		snapshots = append(snapshots, snapshot)
		deviceIDs = append(deviceIDs, snapshot.ID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(deviceIDs) != int(quantity) {
		return ErrInsufficientDeviceStock
	}

	warehousePtr := warehouseID
	if err := transitionShipmentDevicesTx(
		ctx,
		tx,
		userID,
		deviceIDs,
		"RESERVED",
		&warehousePtr,
		nil,
		"设备订单支付成功自动锁库 · "+orderNo,
	); err != nil {
		return err
	}

	for _, snapshot := range snapshots {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO biz_order_devices (
				order_id, order_item_id, device_id,
				sn_snapshot, sku_snapshot, status, reserved_at
			)
			VALUES (?, ?, ?, ?, ?, 'reserved', CURRENT_TIMESTAMP(3))
		`,
			orderID,
			orderItemID,
			snapshot.ID,
			snapshot.SN,
			snapshot.SKU,
		); err != nil {
			return err
		}
	}

	var shipping model.CustomerOrderShipping
	if err := tx.QueryRowContext(ctx, `
		SELECT recipient_name, recipient_phone, province, city,
		       district, address, full_address
		FROM biz_order_shipping
		WHERE order_id=?
	`, orderID).Scan(
		&shipping.RecipientName,
		&shipping.RecipientPhone,
		&shipping.Province,
		&shipping.City,
		&shipping.District,
		&shipping.Address,
		&shipping.FullAddress,
	); err != nil {
		return err
	}

	shipmentNo := nextInventoryNo("SHP")
	trackingNo := nextInventoryNo("SIM")
	result, err := tx.ExecContext(ctx, `
		INSERT INTO inv_shipments (
			shipment_no, shipment_type, business_type, business_id, business_no,
			from_warehouse_id, to_warehouse_id, recipient_customer_id,
			recipient_name, recipient_phone, recipient_address,
			carrier_code, carrier_name, tracking_no, status, note,
			operator_user_id
		)
		VALUES (
			?, 'outbound', 'order', ?, ?,
			?, NULL, ?,
			?, ?, ?,
			'simulated', '模拟物流', ?, 'ready_to_ship',
			?, ?
		)
	`,
		shipmentNo,
		orderID,
		orderNo,
		warehouseID,
		tenantID,
		shipping.RecipientName,
		shipping.RecipientPhone,
		shipping.FullAddress,
		trackingNo,
		"设备订单支付成功自动生成待出库物流 · "+orderNo,
		userID,
	)
	if err != nil {
		return err
	}
	shipmentID, err := result.LastInsertId()
	if err != nil {
		return err
	}

	for _, snapshot := range snapshots {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_shipment_items (
				shipment_id, device_id, sn_snapshot, sku_snapshot
			)
			VALUES (?, ?, ?, ?)
		`,
			shipmentID,
			snapshot.ID,
			snapshot.SN,
			snapshot.SKU,
		); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO inv_logistics_events (
			shipment_id, event_code, status, location,
			description, operator_user_id, occurred_at
		)
		VALUES (?, 'ready_to_ship', 'ready_to_ship', '', ?, ?, ?)
	`,
		shipmentID,
		"设备订单支付成功，SN 已锁定并进入待出库 · "+orderNo,
		userID,
		time.Now().UTC(),
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_order_devices
		SET shipment_id=?
		WHERE order_id=?
	`, shipmentID, orderID); err != nil {
		return err
	}
	return nil
}

func (s *Store) loadCustomerOrderShipping(
	ctx context.Context,
	orderID int64,
) (*model.CustomerOrderShipping, error) {
	var item model.CustomerOrderShipping
	err := s.db.QueryRowContext(ctx, `
		SELECT recipient_name, recipient_phone, province, city,
		       district, address, full_address
		FROM biz_order_shipping
		WHERE order_id=?
	`, orderID).Scan(
		&item.RecipientName,
		&item.RecipientPhone,
		&item.Province,
		&item.City,
		&item.District,
		&item.Address,
		&item.FullAddress,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) listCustomerOrderDevices(
	ctx context.Context,
	orderID int64,
) ([]model.CustomerOrderDevice, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, order_id, order_item_id, device_id,
		       sn_snapshot, sku_snapshot, status, shipment_id,
		       reserved_at, shipped_at, delivered_at, returned_at
		FROM biz_order_devices
		WHERE order_id=?
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CustomerOrderDevice, 0)
	for rows.Next() {
		var item model.CustomerOrderDevice
		if err := rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.OrderItemID,
			&item.DeviceID,
			&item.SN,
			&item.SKUCode,
			&item.Status,
			&item.ShipmentID,
			&item.ReservedAt,
			&item.ShippedAt,
			&item.DeliveredAt,
			&item.ReturnedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) listOrderShipments(
	ctx context.Context,
	orderID int64,
) ([]model.Shipment, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM inv_shipments
		WHERE business_type='order' AND business_id=?
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items := make([]model.Shipment, 0, len(ids))
	for _, id := range ids {
		item, err := s.GetShipment(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}
