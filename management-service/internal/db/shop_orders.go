package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
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
	ErrShopOrderExpired            = errors.New("shop order expired")
)

const defaultDeviceOrderHoldMinutes = 15

func (s *Store) deviceOrderHoldDuration(ctx context.Context) time.Duration {
	var raw string
	if err := s.db.QueryRowContext(ctx, `
		SELECT value_text
		FROM mgmt_system_settings
		WHERE setting_key='device_order_hold_minutes'
		LIMIT 1
	`).Scan(&raw); err != nil {
		return defaultDeviceOrderHoldMinutes * time.Minute
	}
	minutes, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || minutes < 1 || minutes > 120 {
		minutes = defaultDeviceOrderHoldMinutes
	}
	return time.Duration(minutes) * time.Minute
}

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
		WHERE tenant_id=? AND order_type IN ('time_card', 'device', 'membership')
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
				assetCount       int
				originalSeconds  uint64
				remainingSeconds uint64
			)
			err := s.db.QueryRowContext(ctx, `
				SELECT COUNT(*),
				       COALESCE(SUM(a.original_seconds), 0),
				       COALESCE(SUM(
				         CASE
				           WHEN a.status='unactivated' THEN a.remaining_seconds
				           WHEN a.status='active' THEN COALESCE(q.remaining_seconds, a.remaining_seconds)
				           ELSE 0
				         END
				       ), 0)
				FROM biz_time_card_assets a
				LEFT JOIN quota_buckets q ON q.id=a.quota_bucket_id
				WHERE a.tenant_id=? AND a.source_order_id=?
			`, tenantID, orderID).Scan(
				&assetCount,
				&originalSeconds,
				&remainingSeconds,
			)
			if err != nil {
				return model.CustomerShopOrder{}, err
			}
			if assetCount == 0 {
				// Backward compatibility for orders fulfilled before time-card assets
				// were introduced. Those orders used one merged quota bucket.
				err = s.db.QueryRowContext(ctx, `
					SELECT COUNT(*),
					       COALESCE(SUM(original_seconds), 0),
					       COALESCE(SUM(remaining_seconds), 0)
					FROM quota_buckets
					WHERE tenant_id=? AND source_type='time_card_purchase' AND source_id=?
				`, tenantID, orderID).Scan(
					&assetCount,
					&originalSeconds,
					&remainingSeconds,
				)
				if err != nil {
					return model.CustomerShopOrder{}, err
				}
			}
			if assetCount > 0 {
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
		} else if item.OrderType == "membership" {
			// A paid membership order is fulfilled in the same transaction that
			// activates or extends the membership. Do not rely on source_order_id
			// here because a later renewal legitimately replaces that reference.
			item.FulfillmentStatus = "fulfilled"
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
	if input.ProductType == "membership" {
		return s.createMembershipCustomerShopOrder(ctx, tenantID, userID, input)
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
		productName            string
		productVersionID       int64
		versionNo              uint32
		listPrice              uint64
		durationSeconds        uint64
		validityDays           uint32
		activationMode         string
		activationDeadlineDays uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT p.name, v.id, v.version_no, v.price_cents,
		       v.duration_seconds, v.validity_days,
		       v.activation_mode, v.activation_deadline_days
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
		&activationMode,
		&activationDeadlineDays,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	membershipDiscountBPS := uint32(10000)
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
		&membershipDiscountBPS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		membershipDiscountBPS = 10000
		membershipID = sql.NullInt64{}
		membershipPlanVersionID = sql.NullInt64{}
	} else if err != nil {
		return model.CustomerShopOrder{}, err
	}
	membershipDiscountBPS = normalizeMembershipDiscountBPS(membershipDiscountBPS)

	discountBPS := membershipDiscountBPS
	marketingCampaignID := int64(0)
	marketingCampaignName := ""
	var selectedMarketingCampaign model.MarketingCampaign
	var selectedMarketingItem model.MarketingCampaignItem
	if input.MarketingCampaignID > 0 {
		campaigns, err := s.ListMarketingCampaigns(
			ctx,
			"time_card",
			input.ProductID,
			true,
			normalizeCustomerMarketingPlacement(input.MarketingPlacement),
		)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
		var matched *model.MarketingCampaign
		for i := range campaigns {
			if campaigns[i].ID == input.MarketingCampaignID {
				matched = &campaigns[i]
				break
			}
		}
		if matched == nil {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign unavailable")
		}
		campaignItem, ok := marketingCampaignTargetItem(*matched, "time_card", input.ProductID)
		if !ok {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign target unavailable")
		}
		if campaignItem.Quantity == 0 {
			campaignItem.Quantity = 1
		}
		if campaignItem.Quantity > 100 {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing quantity too large")
		}
		input.Quantity = campaignItem.Quantity
		discountBPS = normalizeMarketingDiscountBPS(campaignItem.DiscountBPS)
		marketingCampaignID = matched.ID
		marketingCampaignName = matched.Name
		selectedMarketingCampaign = *matched
		selectedMarketingItem = campaignItem
	}

	relations, err := loadOrderRelationSnapshotTx(ctx, tx, tenantID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	quantity := uint64(input.Quantity)
	listAmount := listPrice * quantity
	payableAmount := uint64(0)
	if marketingCampaignID > 0 {
		listAmount = floorMarketingYuanCents(listAmount)
		payableAmount = calculateMarketingPayableCents(listAmount, discountBPS)
	} else {
		payableAmount = (listPrice * uint64(discountBPS) / 10000) * quantity
	}
	unitPaidPrice := uint64(0)
	if quantity > 0 {
		unitPaidPrice = payableAmount / quantity
	}
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
				'quantity', ?,
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?
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
		marketingCampaignID,
		marketingCampaignName,
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
			validity_days_snapshot, activation_mode_snapshot,
			activation_deadline_days_snapshot, unit_list_price_cents,
			unit_paid_price_cents, discount_bps_snapshot,
			metadata_json
		)
		VALUES (
			?, 'time_card', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			JSON_OBJECT(
				'source', 'customer_shop',
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?
			)
		)
	`,
		orderID,
		input.ProductID,
		productVersionID,
		productName,
		input.Quantity,
		durationSeconds,
		validityDays,
		activationMode,
		activationDeadlineDays,
		listPrice,
		unitPaidPrice,
		discountBPS,
		marketingCampaignID,
		marketingCampaignName,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if marketingCampaignID > 0 {
		if err := reserveMarketingCampaignOrderTx(
			ctx,
			tx,
			tenantID,
			orderID,
			selectedMarketingCampaign,
			selectedMarketingItem,
			listAmount,
			payableAmount,
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

func (s *Store) createMembershipCustomerShopOrder(
	ctx context.Context,
	tenantID int64,
	userID int64,
	input model.CreateCustomerShopOrderInput,
) (model.CustomerShopOrder, error) {
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
		planName                  string
		planVersionID             int64
		versionNo                 uint32
		monthlyPrice              uint64
		recurringMonthDiscountBPS uint32
		quarterDiscountBPS        uint32
		annualDiscountBPS         uint32
		includedSeconds           uint64
		allowAutoRenew            bool
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT p.name, v.id, v.version_no, v.price_cents,
		       v.recurring_month_discount_bps,
		       v.recurring_quarter_discount_bps,
		       v.annual_discount_bps,
		       v.included_seconds,
		       v.allow_auto_renew
		FROM catalog_membership_plans p
		INNER JOIN catalog_membership_plan_versions v ON v.plan_id=p.id
		WHERE p.id=? AND p.status='active'
		  AND v.lifecycle_status='active'
		  AND (v.effective_from IS NULL OR v.effective_from <= CURRENT_TIMESTAMP(3))
		  AND (v.effective_to IS NULL OR v.effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY v.version_no DESC
		LIMIT 1
		FOR UPDATE
	`, input.ProductID).Scan(
		&planName,
		&planVersionID,
		&versionNo,
		&monthlyPrice,
		&recurringMonthDiscountBPS,
		&quarterDiscountBPS,
		&annualDiscountBPS,
		&includedSeconds,
		&allowAutoRenew,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	months := uint32(1)
	discountBPS := uint32(10000)
	autoRenew := false
	cycleKey := strings.TrimSpace(input.MembershipCycle)
	marketingCampaignID := int64(0)
	marketingCampaignName := ""
	var selectedMarketingCampaign model.MarketingCampaign
	var selectedMarketingItem model.MarketingCampaignItem
	if input.MarketingCampaignID > 0 {
		campaigns, err := s.ListMarketingCampaigns(
			ctx,
			"membership",
			input.ProductID,
			true,
			normalizeCustomerMarketingPlacement(input.MarketingPlacement),
		)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
		var matched *model.MarketingCampaign
		for i := range campaigns {
			if campaigns[i].ID == input.MarketingCampaignID {
				matched = &campaigns[i]
				break
			}
		}
		if matched == nil {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign unavailable")
		}
		campaignItem, ok := marketingCampaignTargetItem(*matched, "membership", input.ProductID)
		if !ok {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign target unavailable")
		}
		itemQuantity := campaignItem.Quantity
		if itemQuantity == 0 {
			itemQuantity = 1
		}
		months = campaignItem.PackageMonths
		if months == 0 {
			months = 1
		}
		if uint64(months)*uint64(itemQuantity) > 120 {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing membership period too large")
		}
		months *= itemQuantity
		discountBPS = campaignItem.DiscountBPS
		autoRenew = allowAutoRenew && campaignItem.PricingMode == "package"
		marketingCampaignID = matched.ID
		marketingCampaignName = matched.Name
		selectedMarketingCampaign = *matched
		selectedMarketingItem = campaignItem
		cycleKey = marketingCycleKey(*matched)
	} else {
		switch cycleKey {
		case "", "single_month":
			cycleKey = "single_month"
		case "recurring_month":
			discountBPS = recurringMonthDiscountBPS
			autoRenew = allowAutoRenew
		case "quarter":
			months = 3
			discountBPS = quarterDiscountBPS
			autoRenew = allowAutoRenew
		case "half_year":
			months = 6
		case "annual":
			months = 12
			discountBPS = annualDiscountBPS
			autoRenew = allowAutoRenew
		default:
			return model.CustomerShopOrder{}, ErrUnsupportedShopProduct
		}
	}
	if marketingCampaignID > 0 {
		discountBPS = normalizeMarketingDiscountBPS(discountBPS)
	} else {
		discountBPS = normalizeMembershipDiscountBPS(discountBPS)
	}

	rawListAmount := monthlyPrice * uint64(months)
	var listAmount uint64
	var payableAmount uint64
	if marketingCampaignID > 0 {
		listAmount = floorMarketingYuanCents(rawListAmount)
		payableAmount = calculateMarketingPayableCents(listAmount, discountBPS)
	} else {
		listAmount = ((rawListAmount + 50) / 100) * 100
		rawPayableNumerator := listAmount * uint64(discountBPS)
		payableAmount = ((rawPayableNumerator + 500000) / 1000000) * 100
		if listAmount > 0 && payableAmount == 0 && discountBPS > 0 {
			payableAmount = 100
		}
	}
	if payableAmount > listAmount {
		payableAmount = listAmount
	}
	discountAmount := listAmount - payableAmount
	totalIncludedSeconds := includedSeconds * uint64(months)

	var currentMembershipID sql.NullInt64
	_ = tx.QueryRowContext(ctx, `
		SELECT id
		FROM biz_memberships
		WHERE tenant_id=? AND status='active'
		  AND cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY cycle_end_at DESC, id DESC
		LIMIT 1
	`, tenantID).Scan(&currentMembershipID)

	relations, err := loadOrderRelationSnapshotTx(ctx, tx, tenantID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	orderNo := fmt.Sprintf("ORD-MEM-%d", time.Now().UTC().UnixNano())
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf(
			"shop-membership-order-%d-%d",
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
			?, ?, 'membership', 'pending', 'CNY',
			?, ?, ?, 0, 0, ?, ?,
			?, ?, ?, ?, ?, ?,
			JSON_OBJECT(
				'plan_id', ?,
				'plan_version_id', ?,
				'version_no', ?,
				'cycle', ?,
				'months', ?,
				'monthly_price_cents', ?,
				'discount_bps', ?,
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?,
				'pricing_rule', 'floor_yuan',
				'included_seconds_per_month', ?,
				'auto_renew', ?
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
		nullableInt64Value(currentMembershipID),
		planVersionID,
		nullableInt64Value(relations.SalesAssignmentID),
		nullableInt64Value(relations.SalesStaffID),
		nullableInt64Value(relations.AgentRelationID),
		nullableInt64Value(relations.AgentOrgID),
		nullableInt64Value(relations.ReferralRelationID),
		nullableInt64Value(relations.ReferrerTenantID),
		input.ProductID,
		planVersionID,
		versionNo,
		cycleKey,
		months,
		monthlyPrice,
		discountBPS,
		marketingCampaignID,
		marketingCampaignName,
		includedSeconds,
		autoRenew,
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
			unit_paid_price_cents, discount_bps_snapshot, metadata_json
		)
		VALUES (
			?, 'membership', ?, ?, ?, 1, ?, NULL, ?, ?, ?,
			JSON_OBJECT(
				'source', 'membership_center',
				'cycle', ?,
				'months', ?,
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?,
				'auto_renew', ?
			)
		)
	`,
		orderID,
		input.ProductID,
		planVersionID,
		planName,
		totalIncludedSeconds,
		listAmount,
		payableAmount,
		discountBPS,
		cycleKey,
		months,
		marketingCampaignID,
		marketingCampaignName,
		autoRenew,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if marketingCampaignID > 0 {
		if err := reserveMarketingCampaignOrderTx(
			ctx,
			tx,
			tenantID,
			orderID,
			selectedMarketingCampaign,
			selectedMarketingItem,
			listAmount,
			payableAmount,
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
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
	if orderType == "device" {
		var expiredHoldCount int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM biz_order_devices
			WHERE order_id=?
			  AND status='payment_hold'
			  AND hold_expires_at IS NOT NULL
			  AND hold_expires_at<=CURRENT_TIMESTAMP(3)
		`, orderID).Scan(&expiredHoldCount); err != nil {
			return model.CustomerShopOrder{}, err
		}
		if expiredHoldCount > 0 {
			if _, err := releaseDeviceOrderHoldTx(
				ctx,
				tx,
				userID,
				orderID,
				"设备订单支付超时，释放待支付锁库",
			); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if _, err := tx.ExecContext(ctx, `
				UPDATE biz_orders
				SET status='cancelled', cancelled_at=CURRENT_TIMESTAMP(3)
				WHERE id=? AND tenant_id=? AND status='pending'
			`, orderID, tenantID); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if err := releaseMarketingCampaignOrderTx(ctx, tx, orderID, "expired"); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if err := tx.Commit(); err != nil {
				return model.CustomerShopOrder{}, err
			}
			return model.CustomerShopOrder{}, ErrShopOrderExpired
		}
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
	case "membership":
		if err := fulfillMembershipOrderTx(
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

	if err := consumeMarketingCampaignOrderTx(ctx, tx, orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := accrueReferralRewardForPaidOrderTx(ctx, tx, orderID); err != nil {
		return model.CustomerShopOrder{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenantID, orderID)
}

func fulfillMembershipOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	userID int64,
	orderID int64,
	orderNo string,
) error {
	var (
		planID         int64
		planVersionID  int64
		totalSeconds   uint64
		cycle          string
		metadataMonths uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT product_id, product_version_id,
		       COALESCE(duration_seconds_snapshot, 0),
		       COALESCE(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.cycle')), 'single_month'),
		       COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.months')) AS UNSIGNED), 0)
		FROM biz_order_items
		WHERE order_id=? AND product_type='membership'
		ORDER BY id ASC
		LIMIT 1
	`, orderID).Scan(
		&planID,
		&planVersionID,
		&totalSeconds,
		&cycle,
		&metadataMonths,
	); err != nil {
		return err
	}

	months := int(metadataMonths)
	if months <= 0 {
		switch cycle {
		case "single_month", "recurring_month":
			months = 1
		case "quarter":
			months = 3
		case "half_year":
			months = 6
		case "annual":
			months = 12
		default:
			if marketingCampaignIDFromCycle(cycle) > 0 {
				return fmt.Errorf("membership campaign order missing package months")
			}
			return fmt.Errorf("invalid membership cycle")
		}
	}

	var allowAutoRenew bool
	var monthlyIncludedSeconds uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT allow_auto_renew, included_seconds
		FROM catalog_membership_plan_versions
		WHERE id=? AND plan_id=?
		LIMIT 1
	`, planVersionID, planID).Scan(&allowAutoRenew, &monthlyIncludedSeconds); err != nil {
		return err
	}
	autoRenew := allowAutoRenew && cycle != "single_month"

	now := time.Now().UTC()
	entitlementStart := now
	cycleEnd := entitlementStart.AddDate(0, months, 0)

	var (
		currentMembershipID int64
		currentPlanID       int64
		currentCycleEnd     time.Time
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id, plan_id, cycle_end_at
		FROM biz_memberships
		WHERE tenant_id=? AND status='active'
		  AND cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY cycle_end_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, tenantID).Scan(
		&currentMembershipID,
		&currentPlanID,
		&currentCycleEnd,
	)
	switch {
	case err == nil && currentPlanID == planID:
		if currentCycleEnd.After(now) {
			entitlementStart = currentCycleEnd
			cycleEnd = entitlementStart.AddDate(0, months, 0)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_memberships
			SET plan_version_id=?,
			    cycle_end_at=?,
			    auto_renew=?,
			    source_order_id=?
			WHERE id=?
		`, planVersionID, cycleEnd, autoRenew, orderID, currentMembershipID); err != nil {
			return err
		}
	case err == nil:
		if _, err := tx.ExecContext(ctx, `
			UPDATE quota_buckets
			SET remaining_seconds=0, status='replaced', updated_at=CURRENT_TIMESTAMP(3)
			WHERE tenant_id=? AND source_type='membership_cycle'
			  AND status='active' AND expires_at>?
		`, tenantID, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_memberships
			SET status='replaced'
			WHERE tenant_id=? AND status='active'
			  AND cycle_start_at <= CURRENT_TIMESTAMP(3)
			  AND cycle_end_at > CURRENT_TIMESTAMP(3)
		`, tenantID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO biz_memberships (
				tenant_id, plan_id, plan_version_id, status,
				cycle_start_at, cycle_end_at, auto_renew, source_order_id
			)
			VALUES (?, ?, ?, 'active', ?, ?, ?, ?)
		`, tenantID, planID, planVersionID, now, cycleEnd, autoRenew, orderID)
		if err != nil {
			return err
		}
		if _, err := result.LastInsertId(); err != nil {
			return err
		}
	case errors.Is(err, sql.ErrNoRows):
		result, err := tx.ExecContext(ctx, `
			INSERT INTO biz_memberships (
				tenant_id, plan_id, plan_version_id, status,
				cycle_start_at, cycle_end_at, auto_renew, source_order_id
			)
			VALUES (?, ?, ?, 'active', ?, ?, ?, ?)
		`, tenantID, planID, planVersionID, now, cycleEnd, autoRenew, orderID)
		if err != nil {
			return err
		}
		if _, err := result.LastInsertId(); err != nil {
			return err
		}
	default:
		return err
	}

	if monthlyIncludedSeconds == 0 {
		return nil
	}

	// Membership base AI time is a monthly allowance. A quarterly/yearly
	// purchase pays once, but later months do not become consumable early.
	for monthIndex := 0; monthIndex < months; monthIndex++ {
		monthStart := entitlementStart.AddDate(0, monthIndex, 0)
		monthEnd := entitlementStart.AddDate(0, monthIndex+1, 0)
		bucketExternalID := fmt.Sprintf("membership-order-%d-%d", orderID, monthIndex+1)
		result, err := tx.ExecContext(ctx, `
			INSERT INTO quota_buckets (
				external_id, tenant_id, source_type, source_id,
				original_seconds, remaining_seconds, effective_at,
				expires_at, priority, status, metadata_json
			)
			VALUES (
				?, ?, 'membership_cycle', ?,
				?, ?, ?, ?, 40, 'active',
				JSON_OBJECT(
					'order_no', ?,
					'plan_id', ?,
					'plan_version_id', ?,
					'cycle', ?,
					'month_index', ?,
					'sandbox_payment', true
				)
			)
		`,
			bucketExternalID,
			tenantID,
			orderID,
			monthlyIncludedSeconds,
			monthlyIncludedSeconds,
			monthStart,
			monthEnd,
			orderNo,
			planID,
			planVersionID,
			cycle,
			monthIndex+1,
		)
		if err != nil {
			return err
		}
		bucketID, err := result.LastInsertId()
		if err != nil {
			return err
		}

		ledgerExternalID := fmt.Sprintf("membership-credit-%d-%d", orderID, monthIndex+1)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO quota_ledger (
				external_id, tenant_id, bucket_id,
				change_seconds, remaining_before_seconds,
				remaining_after_seconds, business_type, business_id,
				operator_user_id, reason, idempotency_key, occurred_at
			)
			VALUES (
				?, ?, ?, ?, 0, ?,
				'membership_purchase', ?, ?, ?, ?, ?
			)
		`,
			ledgerExternalID,
			tenantID,
			bucketID,
			int64(monthlyIncludedSeconds),
			monthlyIncludedSeconds,
			orderID,
			userID,
			fmt.Sprintf("会员购买第%d月基础时长 · %s", monthIndex+1, orderNo),
			ledgerExternalID,
			now,
		); err != nil {
			return err
		}
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

	var quotaAfter int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(remaining_seconds), 0)
		FROM quota_buckets
		WHERE tenant_id=? AND status='active'
		  AND effective_at<=? AND expires_at>?
		  AND remaining_seconds>0
	`, tenantID, now, now).Scan(&quotaAfter); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE org_resource_accounts
		SET balance=?
		WHERE organization_id=? AND resource_type='ai_seconds'
	`, quotaAfter, tenantID); err != nil {
		return err
	}

	changeQuantity := quotaAfter - resourceBefore
	if changeQuantity != 0 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO org_resource_ledger (
				organization_id, resource_type, change_quantity,
				balance_before, balance_after, business_type,
				operator_user_id, reason
			)
			VALUES (?, 'ai_seconds', ?, ?, ?, 'membership_purchase', ?, ?)
		`,
			tenantID,
			changeQuantity,
			resourceBefore,
			quotaAfter,
			userID,
			"会员订单 "+orderNo+" 权益生效后同步当前可用AI时长",
		); err != nil {
			return err
		}
	}

	_ = totalSeconds // order snapshot keeps the total package allowance for traceability.
	return nil
}

func fulfillTimeCardOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	userID int64,
	orderID int64,
	orderNo string,
) error {
	_ = userID
	var (
		orderItemID            int64
		productID              int64
		productVersionID       int64
		productName            string
		quantity               uint32
		durationSeconds        uint64
		validityDays           uint32
		activationMode         string
		activationDeadlineDays uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT id, product_id, product_version_id, product_name_snapshot,
		       quantity, COALESCE(duration_seconds_snapshot, 0),
		       COALESCE(validity_days_snapshot, 0),
		       COALESCE(NULLIF(activation_mode_snapshot, ''), 'first_use'),
		       COALESCE(activation_deadline_days_snapshot, 0)
		FROM biz_order_items
		WHERE order_id=? AND product_type='time_card'
		ORDER BY id ASC
		LIMIT 1
	`, orderID).Scan(
		&orderItemID,
		&productID,
		&productVersionID,
		&productName,
		&quantity,
		&durationSeconds,
		&validityDays,
		&activationMode,
		&activationDeadlineDays,
	); err != nil {
		return err
	}

	if quantity == 0 || durationSeconds == 0 || validityDays == 0 || activationMode != "first_use" {
		return fmt.Errorf("invalid time card fulfillment snapshot")
	}

	now := time.Now().UTC()
	var activationDeadline any
	if activationDeadlineDays > 0 {
		activationDeadline = now.AddDate(0, 0, int(activationDeadlineDays))
	}
	for sequence := uint32(1); sequence <= quantity; sequence++ {
		assetNo := fmt.Sprintf("TC-%d-%03d", orderID, sequence)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO biz_time_card_assets (
				asset_no, tenant_id, source_order_id, source_order_item_id,
				asset_sequence, product_id, product_version_id, product_name_snapshot,
				original_seconds, remaining_seconds, activation_mode,
				activation_deadline_at, validity_days, status
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'unactivated')
		`,
			assetNo,
			tenantID,
			orderID,
			orderItemID,
			sequence,
			productID,
			productVersionID,
			productName,
			durationSeconds,
			durationSeconds,
			activationMode,
			activationDeadline,
			validityDays,
		); err != nil {
			return err
		}
	}
	// Payment grants ownership of each card. It does not start validity or
	// increase active AI quota until that individual card is first used.
	_ = orderNo
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

	assetRefund, handledByAssets, err := refundTimeCardAssetOrderTx(
		ctx,
		tx,
		tenantID,
		userID,
		orderID,
		orderNo,
		paidAmount,
		refundedAmount,
		input,
		idempotencyKey,
	)
	if err != nil {
		return model.CustomerShopOrder{}, model.RefundRecord{}, err
	}
	if handledByAssets {
		if err := reverseOrderIncentivesForRefundTx(
			ctx,
			tx,
			orderID,
			assetRefund.ID,
			assetRefund.RefundNo,
			assetRefund.RefundAmountCents,
			paidAmount,
		); err != nil {
			return model.CustomerShopOrder{}, model.RefundRecord{}, err
		}
		if err := tx.Commit(); err != nil {
			return model.CustomerShopOrder{}, model.RefundRecord{}, err
		}
		order, err := s.GetCustomerShopOrder(ctx, tenantID, orderID)
		if err != nil {
			return model.CustomerShopOrder{}, model.RefundRecord{}, err
		}
		return order, assetRefund, nil
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

	if err := reverseOrderIncentivesForRefundTx(
		ctx,
		tx,
		orderID,
		refundID,
		refundNo,
		input.AmountCents,
		paidAmount,
	); err != nil {
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

	var status, orderType string
	if err := tx.QueryRowContext(ctx, `
		SELECT status, order_type
		FROM biz_orders
		WHERE id=? AND tenant_id=?
		FOR UPDATE
	`, orderID, tenantID).Scan(&status, &orderType); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if status == "paid" || status == "fulfilled" || status == "completed" {
		return model.CustomerShopOrder{}, ErrShopOrderAlreadyPaid
	}
	if status == "cancelled" {
		_ = tx.Rollback()
		return s.GetCustomerShopOrder(ctx, tenantID, orderID)
	}

	if orderType == "device" {
		if _, err := releaseDeviceOrderHoldTx(
			ctx,
			tx,
			0,
			orderID,
			"用户取消未支付设备订单，释放锁库",
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
	}

	if err := releaseMarketingCampaignOrderTx(ctx, tx, orderID, "cancelled"); err != nil {
		return model.CustomerShopOrder{}, err
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

func holdPendingDeviceOrderStockTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	orderID int64,
	orderItemID int64,
	productID int64,
	skuCode string,
	quantity uint32,
	orderNo string,
	holdExpiresAt time.Time,
) error {
	if quantity == 0 {
		return ErrInsufficientDeviceStock
	}

	var salesStock uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT sales_stock
		FROM catalog_device_products
		WHERE id=?
		FOR UPDATE
	`, productID).Scan(&salesStock); err != nil {
		return err
	}
	if salesStock < quantity {
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
		"设备订单待支付锁库 · "+orderNo,
	); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET sales_stock=sales_stock-?
		WHERE id=? AND sales_stock>=?
	`, quantity, productID, quantity)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrInsufficientDeviceStock
	}

	for _, snapshot := range snapshots {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO biz_order_devices (
				order_id, order_item_id, device_id,
				sn_snapshot, sku_snapshot, status,
				reserved_at, hold_expires_at
			)
			VALUES (?, ?, ?, ?, ?, 'payment_hold', CURRENT_TIMESTAMP(3), ?)
		`,
			orderID,
			orderItemID,
			snapshot.ID,
			snapshot.SN,
			snapshot.SKU,
			holdExpiresAt,
		); err != nil {
			return err
		}
	}
	return nil
}

func releaseDeviceOrderHoldTx(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	orderID int64,
	reason string,
) (int, error) {
	var (
		productID int64
		skuCode   string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT i.product_id, p.sku_code
		FROM biz_order_items i
		INNER JOIN catalog_device_products p ON p.id=i.product_id
		WHERE i.order_id=? AND i.product_type='device'
		ORDER BY i.id ASC
		LIMIT 1
	`, orderID).Scan(&productID, &skuCode); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT od.device_id, d.custody_warehouse_id
		FROM biz_order_devices od
		INNER JOIN inv_devices d ON d.id=od.device_id
		WHERE od.order_id=? AND od.status='payment_hold'
		ORDER BY od.id ASC
		FOR UPDATE
	`, orderID)
	if err != nil {
		return 0, err
	}
	type heldDevice struct {
		id          int64
		warehouseID sql.NullInt64
	}
	held := make([]heldDevice, 0)
	for rows.Next() {
		var item heldDevice
		if err := rows.Scan(&item.id, &item.warehouseID); err != nil {
			rows.Close()
			return 0, err
		}
		held = append(held, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(held) == 0 {
		return 0, nil
	}

	byWarehouse := make(map[int64][]int64)
	for _, item := range held {
		if !item.warehouseID.Valid {
			return 0, fmt.Errorf("held device has no warehouse")
		}
		byWarehouse[item.warehouseID.Int64] = append(byWarehouse[item.warehouseID.Int64], item.id)
	}
	for warehouseID, ids := range byWarehouse {
		targetWarehouseID := warehouseID
		if err := transitionShipmentDevicesTx(
			ctx,
			tx,
			userID,
			ids,
			"IN_STOCK",
			&targetWarehouseID,
			nil,
			reason,
		); err != nil {
			return 0, err
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE biz_order_devices
		SET status='released', hold_expires_at=NULL
		WHERE order_id=? AND status='payment_hold'
	`, orderID); err != nil {
		return 0, err
	}

	realStock, err := inventorySKUInStockCountTx(ctx, tx, skuCode)
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET sales_stock=LEAST(sales_stock+?, ?)
		WHERE id=?
	`, len(held), realStock, productID); err != nil {
		return 0, err
	}
	return len(held), nil
}

func (s *Store) ReleaseExpiredDeviceOrderHolds(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT o.id
		FROM biz_orders o
		INNER JOIN biz_order_devices od ON od.order_id=o.id
		WHERE o.order_type='device'
		  AND o.status='pending'
		  AND od.status='payment_hold'
		  AND od.hold_expires_at IS NOT NULL
		  AND od.hold_expires_at<=CURRENT_TIMESTAMP(3)
		ORDER BY o.id ASC
		LIMIT 500
	`)
	if err != nil {
		return 0, err
	}
	orderIDs := make([]int64, 0)
	for rows.Next() {
		var orderID int64
		if err := rows.Scan(&orderID); err != nil {
			rows.Close()
			return 0, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	releasedOrders := 0
	for _, orderID := range orderIDs {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return releasedOrders, err
		}

		var status string
		if err := tx.QueryRowContext(ctx, `
			SELECT status
			FROM biz_orders
			WHERE id=?
			FOR UPDATE
		`, orderID).Scan(&status); err != nil {
			_ = tx.Rollback()
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return releasedOrders, err
		}
		if status != "pending" {
			_ = tx.Rollback()
			continue
		}

		released, err := releaseDeviceOrderHoldTx(
			ctx,
			tx,
			0,
			orderID,
			"设备订单未支付超时，自动释放锁库",
		)
		if err != nil {
			_ = tx.Rollback()
			return releasedOrders, err
		}
		if released == 0 {
			_ = tx.Rollback()
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_orders
			SET status='cancelled', cancelled_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND status='pending'
		`, orderID); err != nil {
			_ = tx.Rollback()
			return releasedOrders, err
		}
		if err := releaseMarketingCampaignOrderTx(ctx, tx, orderID, "expired"); err != nil {
			_ = tx.Rollback()
			return releasedOrders, err
		}
		if err := tx.Commit(); err != nil {
			return releasedOrders, err
		}
		releasedOrders++
	}
	return releasedOrders, nil
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
		salesStock       uint32
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT p.name, p.sku_code, v.id, v.version_no,
		       v.list_price_cents, v.sale_price_cents, p.sales_stock
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
		&salesStock,
	); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if salePrice == 0 || listPrice == 0 || salePrice > listPrice {
		return model.CustomerShopOrder{}, fmt.Errorf("invalid device product pricing")
	}

	marketingCampaignID := int64(0)
	marketingCampaignName := ""
	var selectedMarketingCampaign model.MarketingCampaign
	var selectedMarketingItem model.MarketingCampaignItem
	if input.MarketingCampaignID > 0 {
		campaigns, err := s.ListMarketingCampaigns(
			ctx,
			"device_product",
			input.ProductID,
			true,
			normalizeCustomerMarketingPlacement(input.MarketingPlacement),
		)
		if err != nil {
			return model.CustomerShopOrder{}, err
		}
		var matched *model.MarketingCampaign
		for i := range campaigns {
			if campaigns[i].ID == input.MarketingCampaignID {
				matched = &campaigns[i]
				break
			}
		}
		if matched == nil {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign unavailable")
		}
		campaignItem, ok := marketingCampaignTargetItem(*matched, "device_product", input.ProductID)
		if !ok {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing campaign target unavailable")
		}
		if campaignItem.Quantity == 0 {
			campaignItem.Quantity = 1
		}
		if campaignItem.Quantity > 100 {
			return model.CustomerShopOrder{}, fmt.Errorf("marketing quantity too large")
		}
		input.Quantity = campaignItem.Quantity
		marketingCampaignID = matched.ID
		marketingCampaignName = matched.Name
		selectedMarketingCampaign = *matched
		selectedMarketingItem = campaignItem
	}

	if salesStock < input.Quantity {
		return model.CustomerShopOrder{}, ErrInsufficientDeviceStock
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
	membershipDeviceDiscountBPS := uint32(10000)
	err = tx.QueryRowContext(ctx, `
		SELECT m.id, m.plan_version_id, v.default_device_discount_bps
		FROM biz_memberships m
		INNER JOIN catalog_membership_plan_versions v ON v.id=m.plan_version_id
		WHERE m.tenant_id=? AND m.status='active'
		  AND m.cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY m.cycle_end_at DESC, m.id DESC
		LIMIT 1
	`, tenantID).Scan(&membershipID, &membershipPlanVersionID, &membershipDeviceDiscountBPS)
	if errors.Is(err, sql.ErrNoRows) {
		membershipID = sql.NullInt64{}
		membershipPlanVersionID = sql.NullInt64{}
		membershipDeviceDiscountBPS = 10000
	} else if err != nil {
		return model.CustomerShopOrder{}, err
	}
	membershipDeviceDiscountBPS = normalizeMembershipDiscountBPS(membershipDeviceDiscountBPS)

	relations, err := loadOrderRelationSnapshotTx(ctx, tx, tenantID)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	memberPrice := salePrice * uint64(membershipDeviceDiscountBPS) / 10000
	if memberPrice == 0 && salePrice > 0 {
		memberPrice = 1
	}
	discountBPS := uint32(memberPrice * 10000 / listPrice)
	if discountBPS == 0 {
		discountBPS = 1
	}
	quantity := uint64(input.Quantity)
	unitPaidPrice := memberPrice
	listAmount := listPrice * quantity
	payableAmount := memberPrice * quantity
	if marketingCampaignID > 0 {
		discountBPS = normalizeMarketingDiscountBPS(selectedMarketingItem.DiscountBPS)
		listAmount = floorMarketingYuanCents(salePrice * quantity)
		payableAmount = calculateMarketingPayableCents(listAmount, discountBPS)
		if quantity > 0 {
			unitPaidPrice = payableAmount / quantity
		}
	}
	discountAmount := listAmount - payableAmount
	unitListPrice := listPrice
	if marketingCampaignID > 0 {
		unitListPrice = salePrice
	}
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
				'base_sale_price_cents', ?,
				'membership_device_discount_bps', ?,
				'member_price_cents', ?,
				'discount_bps', ?,
				'quantity', ?,
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?
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
		membershipDeviceDiscountBPS,
		memberPrice,
		discountBPS,
		input.Quantity,
		marketingCampaignID,
		marketingCampaignName,
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
				'sku_code', ?,
				'marketing_campaign_id', ?,
				'marketing_campaign_name', ?
			)
		)
	`,
		orderID,
		input.ProductID,
		productVersionID,
		productName,
		input.Quantity,
		unitListPrice,
		unitPaidPrice,
		discountBPS,
		skuCode,
		marketingCampaignID,
		marketingCampaignName,
	)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	orderItemID, err := itemResult.LastInsertId()
	if err != nil {
		return model.CustomerShopOrder{}, err
	}

	if marketingCampaignID > 0 {
		if err := reserveMarketingCampaignOrderTx(
			ctx,
			tx,
			tenantID,
			orderID,
			selectedMarketingCampaign,
			selectedMarketingItem,
			listAmount,
			payableAmount,
		); err != nil {
			return model.CustomerShopOrder{}, err
		}
	}

	holdExpiresAt := time.Now().UTC().Add(s.deviceOrderHoldDuration(ctx))
	if err := holdPendingDeviceOrderStockTx(
		ctx,
		tx,
		userID,
		orderID,
		orderItemID,
		input.ProductID,
		skuCode,
		input.Quantity,
		orderNo,
		holdExpiresAt,
	); err != nil {
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
		productID   int64
		quantity    uint32
		skuCode     string
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT i.id, i.product_id, i.quantity, p.sku_code
		FROM biz_order_items i
		INNER JOIN catalog_device_products p ON p.id=i.product_id
		WHERE i.order_id=? AND i.product_type='device'
		ORDER BY i.id ASC
		LIMIT 1
	`, orderID).Scan(&orderItemID, &productID, &quantity, &skuCode); err != nil {
		return err
	}
	if quantity == 0 {
		return ErrInsufficientDeviceStock
	}

	var warehouseID int64
	snapshots := make([]shipmentDeviceSnapshot, 0, quantity)
	deviceIDs := make([]int64, 0, quantity)

	heldRows, err := tx.QueryContext(ctx, `
		SELECT d.id, d.sn, d.sku_code, d.lifecycle_status, d.custody_warehouse_id,
		       d.owner_org_id, d.current_customer_id
		FROM biz_order_devices od
		INNER JOIN inv_devices d ON d.id=od.device_id
		WHERE od.order_id=? AND od.status='payment_hold'
		ORDER BY od.id ASC
		FOR UPDATE
	`, orderID)
	if err != nil {
		return err
	}
	for heldRows.Next() {
		var snapshot shipmentDeviceSnapshot
		if err := heldRows.Scan(
			&snapshot.ID,
			&snapshot.SN,
			&snapshot.SKU,
			&snapshot.Status,
			&snapshot.WarehouseID,
			&snapshot.OwnerOrgID,
			&snapshot.CustomerID,
		); err != nil {
			heldRows.Close()
			return err
		}
		snapshots = append(snapshots, snapshot)
		deviceIDs = append(deviceIDs, snapshot.ID)
	}
	if err := heldRows.Err(); err != nil {
		heldRows.Close()
		return err
	}
	heldRows.Close()

	if len(deviceIDs) > 0 {
		if len(deviceIDs) != int(quantity) {
			return ErrInsufficientDeviceStock
		}
		for _, snapshot := range snapshots {
			if snapshot.Status != "RESERVED" || snapshot.WarehouseID == nil {
				return ErrInsufficientDeviceStock
			}
			if warehouseID == 0 {
				warehouseID = *snapshot.WarehouseID
			} else if warehouseID != *snapshot.WarehouseID {
				return ErrInsufficientDeviceStock
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE biz_order_devices
			SET status='reserved', hold_expires_at=NULL
			WHERE order_id=? AND status='payment_hold'
		`, orderID); err != nil {
			return err
		}
	} else {
		var salesStock uint32
		if err := tx.QueryRowContext(ctx, `
			SELECT sales_stock
			FROM catalog_device_products
			WHERE id=?
			FOR UPDATE
		`, productID).Scan(&salesStock); err != nil {
			return err
		}
		if salesStock < quantity {
			return ErrInsufficientDeviceStock
		}

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
			"设备订单支付时补锁历史订单库存 · "+orderNo,
		); err != nil {
			return err
		}

		result, err := tx.ExecContext(ctx, `
			UPDATE catalog_device_products
			SET sales_stock=sales_stock-?
			WHERE id=? AND sales_stock>=?
		`, quantity, productID, quantity)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrInsufficientDeviceStock
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
		SET shipment_id=?, status='reserved', hold_expires_at=NULL
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
		       reserved_at, hold_expires_at, shipped_at, delivered_at, returned_at
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
			&item.HoldExpiresAt,
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
