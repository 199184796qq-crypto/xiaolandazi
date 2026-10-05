package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

const customerReferralBeneficiaryType = "customer_referrer"

type referralRuleRow struct {
	ID               int64
	ProgramVersionID int64
	ProgramID        int64
	VersionNo        uint32
	PendingDays      uint32
	Priority         int
	EventType        string
	ConditionsJSON   string
	ActionType       string
	ActionConfigJSON string
}

type referralOrderLine struct {
	ID               int64
	ProductType      string
	ProductID        int64
	ProductVersionID int64
	ProductName      string
	Quantity         uint32
	UnitPaidCents    uint64
}

func referralEventType(productType string) string {
	switch strings.TrimSpace(productType) {
	case "membership":
		return "membership_paid"
	case "time_card":
		return "time_card_paid"
	case "device":
		return "device_paid"
	default:
		return ""
	}
}

func int64JSON(value any) int64 {
	switch typed := value.(type) {
	case float64:
		if typed > float64(math.MaxInt64) || typed < float64(math.MinInt64) {
			return 0
		}
		return int64(typed)
	case int:
		return int64(typed)
	case int64:
		return typed
	case json.Number:
		n, _ := typed.Int64()
		return n
	default:
		return 0
	}
}

func stringSliceJSON(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if ok && strings.TrimSpace(text) != "" {
			items = append(items, strings.TrimSpace(text))
		}
	}
	return items
}

func int64SliceJSON(value any) []int64 {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]int64, 0, len(raw))
	for _, item := range raw {
		if value := int64JSON(item); value > 0 {
			items = append(items, value)
		}
	}
	return items
}

func referralContainsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func referralContainsInt64(items []int64, target int64) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func referralRuleMatches(rule referralRuleRow, line referralOrderLine, orderPaid uint64) (map[string]any, bool) {
	conditions := map[string]any{}
	if strings.TrimSpace(rule.ConditionsJSON) != "" {
		if err := json.Unmarshal([]byte(rule.ConditionsJSON), &conditions); err != nil {
			return nil, false
		}
	}
	if minimum := int64JSON(conditions["minimum_paid_cents"]); minimum > 0 && orderPaid < uint64(minimum) {
		return conditions, false
	}
	if level := int64JSON(conditions["referral_level"]); level > 1 {
		return conditions, false
	}
	if types := stringSliceJSON(conditions["product_types"]); len(types) > 0 && !referralContainsString(types, line.ProductType) {
		return conditions, false
	}
	if ids := int64SliceJSON(conditions["product_ids"]); len(ids) > 0 && !referralContainsInt64(ids, line.ProductID) {
		return conditions, false
	}
	return conditions, true
}

func referralRuleAmount(rule referralRuleRow, linePaid uint64) (int64, map[string]any, bool) {
	config := map[string]any{}
	if strings.TrimSpace(rule.ActionConfigJSON) != "" {
		if err := json.Unmarshal([]byte(rule.ActionConfigJSON), &config); err != nil {
			return 0, nil, false
		}
	}
	switch rule.ActionType {
	case "fixed_amount":
		amount := int64JSON(config["amount_cents"])
		return amount, config, amount > 0
	case "percent_paid_amount":
		rate := int64JSON(config["rate_bps"])
		if rate <= 0 || rate > 10000 || linePaid > math.MaxInt64 {
			return 0, config, false
		}
		amount := int64(linePaid) * rate / 10000
		return amount, config, amount > 0
	default:
		return 0, config, false
	}
}

func referralRefundReversalEnabled(conditions map[string]any) bool {
	value, exists := conditions["refund_reversal"]
	if !exists {
		return true
	}
	enabled, ok := value.(bool)
	return !ok || enabled
}

func referralProductParticipatesTx(ctx context.Context, tx *sql.Tx, productType string, productVersionID int64) (bool, error) {
	var participates bool
	var err error
	switch productType {
	case "membership":
		err = tx.QueryRowContext(ctx, `
			SELECT participates_referral
			FROM catalog_membership_plan_versions
			WHERE id=?
		`, productVersionID).Scan(&participates)
	case "time_card":
		err = tx.QueryRowContext(ctx, `
			SELECT participates_referral
			FROM catalog_time_card_versions
			WHERE id=?
		`, productVersionID).Scan(&participates)
	case "device":
		err = tx.QueryRowContext(ctx, `
			SELECT participates_referral
			FROM catalog_device_versions
			WHERE id=?
		`, productVersionID).Scan(&participates)
	default:
		return false, nil
	}
	return participates, err
}

func loadReferralRulesForPaymentTx(ctx context.Context, tx *sql.Tx, paidAt time.Time, eventType string) ([]referralRuleRow, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			r.id, v.id, p.id, v.version_no, v.pending_days,
			r.priority, r.event_type,
			COALESCE(CAST(r.conditions_json AS CHAR), '{}'),
			r.action_type,
			COALESCE(CAST(r.action_config_json AS CHAR), '{}')
		FROM inc_programs p
		INNER JOIN inc_program_versions v ON v.program_id=p.id
		INNER JOIN inc_rules r ON r.program_version_id=v.id
		WHERE p.program_type='referral'
		  AND p.status='active'
		  AND v.lifecycle_status='published'
		  AND (v.effective_from IS NULL OR v.effective_from <= ?)
		  AND (v.effective_to IS NULL OR v.effective_to > ?)
		  AND r.enabled=1
		  AND r.event_type=?
		ORDER BY p.id ASC, r.priority ASC, r.id ASC
	`, paidAt, paidAt, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]referralRuleRow, 0)
	for rows.Next() {
		var item referralRuleRow
		if err := rows.Scan(
			&item.ID,
			&item.ProgramVersionID,
			&item.ProgramID,
			&item.VersionNo,
			&item.PendingDays,
			&item.Priority,
			&item.EventType,
			&item.ConditionsJSON,
			&item.ActionType,
			&item.ActionConfigJSON,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func ensureBeneficiaryWalletTx(ctx context.Context, tx *sql.Tx, beneficiaryType string, beneficiaryID int64) (model.BeneficiaryWallet, error) {
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO fin_beneficiary_wallets (
			beneficiary_type, beneficiary_id, currency,
			available_balance_cents, frozen_balance_cents
		)
		VALUES (?, ?, 'CNY', 0, 0)
	`, beneficiaryType, beneficiaryID); err != nil {
		return model.BeneficiaryWallet{}, err
	}
	var item model.BeneficiaryWallet
	if err := tx.QueryRowContext(ctx, `
		SELECT id, beneficiary_type, beneficiary_id, currency,
		       available_balance_cents, frozen_balance_cents,
		       version, created_at, updated_at
		FROM fin_beneficiary_wallets
		WHERE beneficiary_type=? AND beneficiary_id=? AND currency='CNY'
		FOR UPDATE
	`, beneficiaryType, beneficiaryID).Scan(
		&item.ID,
		&item.BeneficiaryType,
		&item.BeneficiaryID,
		&item.Currency,
		&item.AvailableBalanceCents,
		&item.FrozenBalanceCents,
		&item.Version,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return model.BeneficiaryWallet{}, err
	}
	return item, nil
}

func applyBeneficiaryWalletDeltaTx(
	ctx context.Context,
	tx *sql.Tx,
	beneficiaryType string,
	beneficiaryID int64,
	externalID string,
	businessType string,
	referenceType string,
	referenceID *int64,
	availableDelta int64,
	frozenDelta int64,
	operatorUserID *int64,
	reason string,
) error {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1 FROM fin_beneficiary_wallet_ledger
		WHERE external_id=? LIMIT 1
	`, externalID).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	wallet, err := ensureBeneficiaryWalletTx(ctx, tx, beneficiaryType, beneficiaryID)
	if err != nil {
		return err
	}
	availableAfter := wallet.AvailableBalanceCents + availableDelta
	frozenAfter := wallet.FrozenBalanceCents + frozenDelta
	if frozenAfter < 0 {
		return fmt.Errorf("beneficiary frozen balance would be negative")
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE fin_beneficiary_wallets
		SET available_balance_cents=?,
		    frozen_balance_cents=?,
		    version=version+1
		WHERE id=?
	`, availableAfter, frozenAfter, wallet.ID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO fin_beneficiary_wallet_ledger (
			wallet_id, external_id, business_type,
			reference_type, reference_id,
			available_delta_cents, frozen_delta_cents,
			available_before_cents, available_after_cents,
			frozen_before_cents, frozen_after_cents,
			operator_user_id, reason
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		wallet.ID,
		externalID,
		businessType,
		referenceType,
		referenceID,
		availableDelta,
		frozenDelta,
		wallet.AvailableBalanceCents,
		availableAfter,
		wallet.FrozenBalanceCents,
		frozenAfter,
		operatorUserID,
		reason,
	)
	return err
}

func accrueReferralRewardForPaidOrderTx(ctx context.Context, tx *sql.Tx, orderID int64) error {
	if err := accrueCommerceRewardsTx(ctx, tx, orderID); err != nil {
		return err
	}
	var (
		referredTenantID   int64
		orderNo            string
		orderPaid          uint64
		referralRelationID sql.NullInt64
		referrerTenantID   sql.NullInt64
		paidAt             sql.NullTime
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT tenant_id, order_no, paid_amount_cents,
		       referral_relation_id_snapshot,
		       referrer_tenant_id_snapshot,
		       paid_at
		FROM biz_orders
		WHERE id=?
		FOR UPDATE
	`, orderID).Scan(
		&referredTenantID,
		&orderNo,
		&orderPaid,
		&referralRelationID,
		&referrerTenantID,
		&paidAt,
	); err != nil {
		return err
	}
	if !referrerTenantID.Valid || referrerTenantID.Int64 <= 0 || referrerTenantID.Int64 == referredTenantID || orderPaid == 0 {
		return nil
	}
	paymentTime := time.Now().UTC()
	if paidAt.Valid {
		paymentTime = paidAt.Time
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, product_type, product_id, product_version_id,
		       product_name_snapshot, quantity, unit_paid_price_cents
		FROM biz_order_items
		WHERE order_id=?
		ORDER BY id ASC
	`, orderID)
	if err != nil {
		return err
	}
	lines := make([]referralOrderLine, 0)
	for rows.Next() {
		var line referralOrderLine
		if err := rows.Scan(
			&line.ID,
			&line.ProductType,
			&line.ProductID,
			&line.ProductVersionID,
			&line.ProductName,
			&line.Quantity,
			&line.UnitPaidCents,
		); err != nil {
			rows.Close()
			return err
		}
		lines = append(lines, line)
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, line := range lines {
		var snapshotMode string
		err := tx.QueryRowContext(ctx, `SELECT JSON_UNQUOTE(JSON_EXTRACT(config_json,'$.mode')) FROM fin_order_reward_snapshots WHERE order_item_id=? AND channel='referral'`, line.ID).Scan(&snapshotMode)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && snapshotMode != "legacy" {
			continue
		}
		eventType := referralEventType(line.ProductType)
		if eventType == "" {
			continue
		}
		participates, err := referralProductParticipatesTx(ctx, tx, line.ProductType, line.ProductVersionID)
		if err != nil {
			return err
		}
		if !participates {
			continue
		}
		if line.Quantity > 0 && line.UnitPaidCents > math.MaxUint64/uint64(line.Quantity) {
			return fmt.Errorf("referral line amount overflow")
		}
		linePaid := line.UnitPaidCents * uint64(line.Quantity)
		rules, err := loadReferralRulesForPaymentTx(ctx, tx, paymentTime, eventType)
		if err != nil {
			return err
		}
		usedPrograms := map[int64]bool{}
		for _, rule := range rules {
			if usedPrograms[rule.ProgramID] {
				continue
			}
			conditions, matched := referralRuleMatches(rule, line, orderPaid)
			if !matched {
				continue
			}
			amount, actionConfig, ok := referralRuleAmount(rule, linePaid)
			if !ok {
				continue
			}
			usedPrograms[rule.ProgramID] = true

			availableAt := paymentTime.AddDate(0, 0, int(rule.PendingDays))
			status := "pending"
			availableDelta := int64(0)
			frozenDelta := amount
			if rule.PendingDays == 0 {
				status = "available"
				availableDelta = amount
				frozenDelta = 0
			}

			snapshot := map[string]any{
				"order_id":           orderID,
				"order_no":           orderNo,
				"order_paid_cents":   orderPaid,
				"order_item_id":      line.ID,
				"product_type":       line.ProductType,
				"product_id":         line.ProductID,
				"product_version_id": line.ProductVersionID,
				"product_name":       line.ProductName,
				"line_paid_cents":    linePaid,
				"referral_level":     1,
				"referred_tenant_id": referredTenantID,
				"referrer_tenant_id": referrerTenantID.Int64,
				"program_version_id": rule.ProgramVersionID,
				"program_version_no": rule.VersionNo,
				"rule_id":            rule.ID,
				"event_type":         eventType,
				"action_type":        rule.ActionType,
				"action_config":      actionConfig,
				"conditions":         conditions,
				"refund_reversal":    referralRefundReversalEnabled(conditions),
				"pending_days":       rule.PendingDays,
			}
			if referralRelationID.Valid {
				snapshot["referral_relation_id"] = referralRelationID.Int64
			}
			snapshotJSON, err := json.Marshal(snapshot)
			if err != nil {
				return err
			}

			externalID := fmt.Sprintf("REF-EARN-%d-%d-%d", orderID, line.ID, rule.ID)
			idempotencyKey := fmt.Sprintf("referral-order-%d-item-%d-rule-%d", orderID, line.ID, rule.ID)
			result, err := tx.ExecContext(ctx, `
				INSERT IGNORE INTO inc_earnings (
					external_id, beneficiary_type, beneficiary_id,
					earning_type, source_order_id,
					program_version_id, rule_id, currency,
					amount_cents, quota_seconds, status, available_at,
					calculation_snapshot_json, idempotency_key
				)
				VALUES (
					?, ?, ?, 'referral_reward', ?,
					?, ?, 'CNY',
					?, 0, ?, ?,
					CAST(? AS JSON), ?
				)
			`,
				externalID,
				customerReferralBeneficiaryType,
				referrerTenantID.Int64,
				orderID,
				rule.ProgramVersionID,
				rule.ID,
				amount,
				status,
				availableAt,
				string(snapshotJSON),
				idempotencyKey,
			)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected == 0 {
				continue
			}
			earningID, err := result.LastInsertId()
			if err != nil {
				return err
			}
			referenceID := earningID
			if err := applyBeneficiaryWalletDeltaTx(
				ctx,
				tx,
				customerReferralBeneficiaryType,
				referrerTenantID.Int64,
				fmt.Sprintf("referral-accrual-%d", earningID),
				"referral_accrual",
				"earning",
				&referenceID,
				availableDelta,
				frozenDelta,
				nil,
				fmt.Sprintf("推荐客户订单 %s 返佣", orderNo),
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func releaseMatureReferralRewardsTx(ctx context.Context, tx *sql.Tx, tenantID int64) error {
	if err := releaseCommerceEarningsTx(ctx, tx, customerReferralBeneficiaryType, tenantID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, amount_cents
		FROM inc_earnings
		WHERE beneficiary_type=?
		  AND beneficiary_id=?
		  AND earning_type IN ('referral_reward', 'referral_reward_refund_reversal')
		  AND status='pending'
		  AND available_at IS NOT NULL
		  AND available_at<=CURRENT_TIMESTAMP(3)
		ORDER BY available_at ASC, amount_cents ASC, id ASC
		FOR UPDATE
	`, customerReferralBeneficiaryType, tenantID)
	if err != nil {
		return err
	}
	type earningRow struct {
		ID     int64
		Amount int64
	}
	items := make([]earningRow, 0)
	for rows.Next() {
		var item earningRow
		if err := rows.Scan(&item.ID, &item.Amount); err != nil {
			rows.Close()
			return err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
			UPDATE inc_earnings
			SET status='available', updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND status='pending'
		`, item.ID); err != nil {
			return err
		}
		referenceID := item.ID
		if err := applyBeneficiaryWalletDeltaTx(
			ctx,
			tx,
			customerReferralBeneficiaryType,
			tenantID,
			fmt.Sprintf("referral-release-%d", item.ID),
			"referral_release",
			"earning",
			&referenceID,
			item.Amount,
			-item.Amount,
			nil,
			"返佣冻结期结束，可提现",
		); err != nil {
			return err
		}
	}
	return nil
}
