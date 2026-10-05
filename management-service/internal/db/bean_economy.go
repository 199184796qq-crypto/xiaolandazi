package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var (
	ErrBeanEconomyDisabled     = errors.New("bean economy disabled")
	ErrBeanPricingDisabled     = errors.New("bean pricing disabled")
	ErrBeanInsufficientBalance = errors.New("bean balance insufficient")
	ErrBeanCashInsufficient    = errors.New("cash balance insufficient")
	ErrBeanConflict            = errors.New("bean state conflict")
	ErrBeanInvalidInput        = errors.New("bean input invalid")
)

var beanActionCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,95}$`)

func (s *Store) BeanCommerceSettings(ctx context.Context) (model.BeanCommerceSettings, error) {
	var out model.BeanCommerceSettings
	err := s.db.QueryRowContext(ctx, `
		SELECT purchase_beans_per_yuan, minimum_purchase_cents,
		       staff_cash_fen_per_100_beans, minimum_staff_conversion_beans,
		       enabled, version, updated_by_user_id, updated_at
		FROM bean_commerce_settings WHERE id=1
	`).Scan(
		&out.PurchaseBeansPerYuan,
		&out.MinimumPurchaseCents,
		&out.StaffCashFenPer100Beans,
		&out.MinimumStaffConversionBeans,
		&out.Enabled,
		&out.Version,
		&out.UpdatedByUserID,
		&out.UpdatedAt,
	)
	return out, err
}

func validateBeanSettings(input model.BeanCommerceSettingsInput) error {
	if input.PurchaseBeansPerYuan == 0 || input.PurchaseBeansPerYuan > 10_000_000 {
		return ErrBeanInvalidInput
	}
	if input.MinimumPurchaseCents < 100 || input.MinimumPurchaseCents > 100_000_000 {
		return ErrBeanInvalidInput
	}
	if input.StaffCashFenPer100Beans == 0 || input.StaffCashFenPer100Beans > 1_000_000 {
		return ErrBeanInvalidInput
	}
	if input.MinimumStaffConversionBeans == 0 || input.MinimumStaffConversionBeans > 1_000_000_000 {
		return ErrBeanInvalidInput
	}
	return nil
}

func (s *Store) UpdateBeanCommerceSettings(
	ctx context.Context,
	actorUserID int64,
	input model.BeanCommerceSettingsInput,
) (model.BeanCommerceSettings, error) {
	if err := validateBeanSettings(input); err != nil {
		return model.BeanCommerceSettings{}, err
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE bean_commerce_settings
		SET purchase_beans_per_yuan=?, minimum_purchase_cents=?,
		    staff_cash_fen_per_100_beans=?, minimum_staff_conversion_beans=?,
		    enabled=?, updated_by_user_id=?, version=version+1
		WHERE id=1 AND version=?
	`, input.PurchaseBeansPerYuan, input.MinimumPurchaseCents,
		input.StaffCashFenPer100Beans, input.MinimumStaffConversionBeans,
		input.Enabled, actorUserID, input.Version)
	if err != nil {
		return model.BeanCommerceSettings{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return model.BeanCommerceSettings{}, ErrBeanConflict
	}
	return s.BeanCommerceSettings(ctx)
}

func scanBeanPricingRule(row interface{ Scan(...any) error }) (model.BeanPricingRule, error) {
	var out model.BeanPricingRule
	err := row.Scan(
		&out.ID, &out.ActionCode, &out.ActionName, &out.Category, &out.Description,
		&out.ChargeMode, &out.BeansPerUnit, &out.UnitSize, &out.MinimumChargeBeans,
		&out.MaximumChargeBeans, &out.StaffRewardBPS, &out.Enabled, &out.Version,
		&out.UpdatedByUserID, &out.UpdatedAt,
	)
	return out, err
}

const beanPricingColumns = `id, action_code, action_name, category, description,
	charge_mode, beans_per_unit, unit_size, minimum_charge_beans,
	maximum_charge_beans, staff_reward_bps, enabled, version,
	updated_by_user_id, updated_at`

func (s *Store) ListBeanPricingRules(ctx context.Context) ([]model.BeanPricingRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+beanPricingColumns+` FROM bean_pricing_rules ORDER BY category, action_code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.BeanPricingRule, 0)
	for rows.Next() {
		item, scanErr := scanBeanPricingRule(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func validateBeanPricingRule(input *model.BeanPricingRuleInput) error {
	input.ActionCode = strings.ToLower(strings.TrimSpace(input.ActionCode))
	input.ActionName = strings.TrimSpace(input.ActionName)
	input.Category = strings.ToLower(strings.TrimSpace(input.Category))
	input.Description = strings.TrimSpace(input.Description)
	input.ChargeMode = strings.ToLower(strings.TrimSpace(input.ChargeMode))
	if !beanActionCodePattern.MatchString(input.ActionCode) || input.ActionName == "" {
		return ErrBeanInvalidInput
	}
	if input.Category != "ai" && input.Category != "service" && input.Category != "tool" {
		return ErrBeanInvalidInput
	}
	if input.ChargeMode != "fixed" && input.ChargeMode != "per_unit" {
		return ErrBeanInvalidInput
	}
	if input.BeansPerUnit == 0 || input.UnitSize == 0 || input.MinimumChargeBeans == 0 {
		return ErrBeanInvalidInput
	}
	if input.MaximumChargeBeans > 0 && input.MaximumChargeBeans < input.MinimumChargeBeans {
		return ErrBeanInvalidInput
	}
	if input.StaffRewardBPS > 10_000 {
		return ErrBeanInvalidInput
	}
	return nil
}

func (s *Store) SaveBeanPricingRule(
	ctx context.Context,
	actorUserID int64,
	ruleID int64,
	input model.BeanPricingRuleInput,
) (model.BeanPricingRule, error) {
	if err := validateBeanPricingRule(&input); err != nil {
		return model.BeanPricingRule{}, err
	}
	if ruleID == 0 {
		result, err := s.db.ExecContext(ctx, `
			INSERT INTO bean_pricing_rules (
				action_code, action_name, category, description, charge_mode,
				beans_per_unit, unit_size, minimum_charge_beans, maximum_charge_beans,
				staff_reward_bps, enabled, updated_by_user_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, input.ActionCode, input.ActionName, input.Category, input.Description, input.ChargeMode,
			input.BeansPerUnit, input.UnitSize, input.MinimumChargeBeans, input.MaximumChargeBeans,
			input.StaffRewardBPS, input.Enabled, actorUserID)
		if err != nil {
			return model.BeanPricingRule{}, err
		}
		ruleID, err = result.LastInsertId()
		if err != nil {
			return model.BeanPricingRule{}, err
		}
	} else {
		result, err := s.db.ExecContext(ctx, `
			UPDATE bean_pricing_rules
			SET action_code=?, action_name=?, category=?, description=?, charge_mode=?,
			    beans_per_unit=?, unit_size=?, minimum_charge_beans=?, maximum_charge_beans=?,
			    staff_reward_bps=?, enabled=?, updated_by_user_id=?, version=version+1
			WHERE id=? AND version=?
		`, input.ActionCode, input.ActionName, input.Category, input.Description, input.ChargeMode,
			input.BeansPerUnit, input.UnitSize, input.MinimumChargeBeans, input.MaximumChargeBeans,
			input.StaffRewardBPS, input.Enabled, actorUserID, ruleID, input.Version)
		if err != nil {
			return model.BeanPricingRule{}, err
		}
		if affected, _ := result.RowsAffected(); affected != 1 {
			return model.BeanPricingRule{}, ErrBeanConflict
		}
	}
	return scanBeanPricingRule(s.db.QueryRowContext(ctx, `SELECT `+beanPricingColumns+` FROM bean_pricing_rules WHERE id=?`, ruleID))
}

func (s *Store) beanSummary(ctx context.Context) (model.BeanCommercialSummary, error) {
	var out model.BeanCommercialSummary
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN owner_type='customer' THEN available_beans ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN owner_type='customer' THEN frozen_beans ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN owner_type='staff' THEN available_beans ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN owner_type='staff' THEN frozen_beans ELSE 0 END),0)
		FROM bean_wallet_accounts WHERE status='active'
	`).Scan(&out.CustomerAvailableBeans, &out.CustomerFrozenBeans, &out.StaffAvailableBeans, &out.StaffFrozenBeans); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(credited_beans),0), COALESCE(SUM(cash_amount_cents),0)
		FROM bean_purchase_orders WHERE status='paid'
	`).Scan(&out.PurchasedBeans, &out.PurchaseCashCents); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(charged_beans),0) FROM bean_charges WHERE status='settled'
	`).Scan(&out.ChargedBeans); err != nil {
		return out, err
	}
	if err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(cash_amount_cents),0)
		FROM bean_conversion_requests WHERE status IN ('reviewing','approved')
	`).Scan(&out.PendingConversionCents); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Store) BeanCommercialDashboard(ctx context.Context) (model.BeanCommercialDashboard, error) {
	settings, err := s.BeanCommerceSettings(ctx)
	if err != nil {
		return model.BeanCommercialDashboard{}, err
	}
	rules, err := s.ListBeanPricingRules(ctx)
	if err != nil {
		return model.BeanCommercialDashboard{}, err
	}
	summary, err := s.beanSummary(ctx)
	if err != nil {
		return model.BeanCommercialDashboard{}, err
	}
	return model.BeanCommercialDashboard{Settings: settings, Rules: rules, Summary: summary}, nil
}

func scanBeanWallet(row interface{ Scan(...any) error }) (model.BeanWallet, error) {
	var out model.BeanWallet
	err := row.Scan(&out.ID, &out.OwnerType, &out.OwnerID, &out.AvailableBeans, &out.FrozenBeans, &out.Status, &out.Version, &out.UpdatedAt)
	return out, err
}

func ensureBeanWalletTx(ctx context.Context, tx *sql.Tx, ownerType string, ownerID int64) (model.BeanWallet, error) {
	if ownerID <= 0 || (ownerType != "customer" && ownerType != "staff" && ownerType != "platform") {
		return model.BeanWallet{}, ErrBeanInvalidInput
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO bean_wallet_accounts (owner_type, owner_id) VALUES (?, ?)
	`, ownerType, ownerID); err != nil {
		return model.BeanWallet{}, err
	}
	return scanBeanWallet(tx.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, available_beans, frozen_beans, status, version, updated_at
		FROM bean_wallet_accounts WHERE owner_type=? AND owner_id=? FOR UPDATE
	`, ownerType, ownerID))
}

func applyBeanWalletTx(
	ctx context.Context,
	tx *sql.Tx,
	wallet model.BeanWallet,
	availableDelta int64,
	frozenDelta int64,
	businessType string,
	referenceType string,
	referenceID *int64,
	operatorUserID int64,
	reason string,
	idempotencyKey string,
) (model.BeanWallet, int64, error) {
	if wallet.AvailableBeans > math.MaxInt64 || wallet.FrozenBeans > math.MaxInt64 {
		return wallet, 0, ErrBeanInvalidInput
	}
	availableBefore := int64(wallet.AvailableBeans)
	frozenBefore := int64(wallet.FrozenBeans)
	if (availableDelta > 0 && availableBefore > math.MaxInt64-availableDelta) ||
		(frozenDelta > 0 && frozenBefore > math.MaxInt64-frozenDelta) {
		return wallet, 0, ErrBeanInvalidInput
	}
	availableAfter := availableBefore + availableDelta
	frozenAfter := frozenBefore + frozenDelta
	if availableAfter < 0 || frozenAfter < 0 {
		return wallet, 0, ErrBeanInsufficientBalance
	}
	updateResult, err := tx.ExecContext(ctx, `
		UPDATE bean_wallet_accounts
		SET available_beans=?, frozen_beans=?, version=version+1
		WHERE id=? AND version=? AND status='active'
	`, availableAfter, frozenAfter, wallet.ID, wallet.Version)
	if err != nil {
		return wallet, 0, err
	}
	if affected, _ := updateResult.RowsAffected(); affected != 1 {
		return wallet, 0, ErrBeanConflict
	}
	externalID, err := newFinanceReference("BEAN")
	if err != nil {
		return wallet, 0, err
	}
	var referenceValue any
	if referenceID != nil {
		referenceValue = *referenceID
	}
	var operatorValue any
	if operatorUserID > 0 {
		operatorValue = operatorUserID
	}
	var idempotencyValue any
	if strings.TrimSpace(idempotencyKey) != "" {
		idempotencyValue = strings.TrimSpace(idempotencyKey)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO bean_wallet_ledger (
			external_id, wallet_id, available_delta, frozen_delta,
			available_before, available_after, frozen_before, frozen_after,
			business_type, reference_type, reference_id, operator_user_id,
			reason, idempotency_key
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, externalID, wallet.ID, availableDelta, frozenDelta,
		wallet.AvailableBeans, availableAfter, wallet.FrozenBeans, frozenAfter,
		businessType, referenceType, referenceValue, operatorValue,
		strings.TrimSpace(reason), idempotencyValue)
	if err != nil {
		return wallet, 0, err
	}
	ledgerID, err := result.LastInsertId()
	if err != nil {
		return wallet, 0, err
	}
	wallet.AvailableBeans = uint64(availableAfter)
	wallet.FrozenBeans = uint64(frozenAfter)
	wallet.Version++
	return wallet, ledgerID, nil
}

func scanBeanLedger(row interface{ Scan(...any) error }) (model.BeanLedgerEntry, error) {
	var out model.BeanLedgerEntry
	var referenceID sql.NullInt64
	err := row.Scan(
		&out.ID, &out.ExternalID, &out.WalletID, &out.AvailableDelta, &out.FrozenDelta,
		&out.AvailableBefore, &out.AvailableAfter, &out.FrozenBefore, &out.FrozenAfter,
		&out.BusinessType, &out.ReferenceType, &referenceID, &out.Reason, &out.CreatedAt,
	)
	if referenceID.Valid {
		value := referenceID.Int64
		out.ReferenceID = &value
	}
	return out, err
}

const beanLedgerColumns = `id, external_id, wallet_id, available_delta, frozen_delta,
	available_before, available_after, frozen_before, frozen_after,
	business_type, reference_type, reference_id, reason, created_at`

func (s *Store) beanWalletDashboard(ctx context.Context, ownerType string, ownerID int64, limit int) (model.BeanWalletDashboard, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanWalletDashboard{}, err
	}
	defer tx.Rollback()
	wallet, err := ensureBeanWalletTx(ctx, tx, ownerType, ownerID)
	if err != nil {
		return model.BeanWalletDashboard{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeanWalletDashboard{}, err
	}
	settings, err := s.BeanCommerceSettings(ctx)
	if err != nil {
		return model.BeanWalletDashboard{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+beanLedgerColumns+` FROM bean_wallet_ledger WHERE wallet_id=? ORDER BY id DESC LIMIT ?`, wallet.ID, limit)
	if err != nil {
		return model.BeanWalletDashboard{}, err
	}
	ledger := make([]model.BeanLedgerEntry, 0)
	for rows.Next() {
		entry, scanErr := scanBeanLedger(rows)
		if scanErr != nil {
			rows.Close()
			return model.BeanWalletDashboard{}, scanErr
		}
		ledger = append(ledger, entry)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return model.BeanWalletDashboard{}, err
	}
	rows.Close()
	return model.BeanWalletDashboard{Wallet: wallet, Ledger: ledger, Settings: settings}, nil
}

func scanBeanPurchase(row interface{ Scan(...any) error }) (model.BeanPurchaseOrder, error) {
	var out model.BeanPurchaseOrder
	err := row.Scan(&out.ID, &out.PurchaseNo, &out.TenantID, &out.CashAmountCents,
		&out.BeansPerYuanSnapshot, &out.CreditedBeans, &out.Status, &out.OperatorUserID, &out.CreatedAt)
	return out, err
}

func (s *Store) CustomerBeanWallet(ctx context.Context, tenantID int64, limit int) (model.BeanWalletDashboard, error) {
	out, err := s.beanWalletDashboard(ctx, "customer", tenantID, limit)
	if err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, purchase_no, tenant_id, cash_amount_cents, beans_per_yuan_snapshot,
		       credited_beans, status, operator_user_id, created_at
		FROM bean_purchase_orders WHERE tenant_id=? ORDER BY id DESC LIMIT ?
	`, tenantID, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Purchases = make([]model.BeanPurchaseOrder, 0)
	for rows.Next() {
		item, scanErr := scanBeanPurchase(rows)
		if scanErr != nil {
			return out, scanErr
		}
		out.Purchases = append(out.Purchases, item)
	}
	return out, rows.Err()
}

func (s *Store) PurchaseCustomerBeans(
	ctx context.Context,
	tenantID int64,
	actorUserID int64,
	amountCents uint64,
	idempotencyKey string,
) (model.BeanPurchaseOrder, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if tenantID <= 0 || actorUserID <= 0 || amountCents == 0 || amountCents > math.MaxInt64 || idempotencyKey == "" {
		return model.BeanPurchaseOrder{}, ErrBeanInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	defer tx.Rollback()
	if existing, scanErr := scanBeanPurchase(tx.QueryRowContext(ctx, `
		SELECT id, purchase_no, tenant_id, cash_amount_cents, beans_per_yuan_snapshot,
		       credited_beans, status, operator_user_id, created_at
		FROM bean_purchase_orders WHERE tenant_id=? AND idempotency_key=?
	`, tenantID, idempotencyKey)); scanErr == nil {
		return existing, tx.Commit()
	} else if !errors.Is(scanErr, sql.ErrNoRows) {
		return model.BeanPurchaseOrder{}, scanErr
	}
	var settings model.BeanCommerceSettings
	if err := tx.QueryRowContext(ctx, `
		SELECT purchase_beans_per_yuan, minimum_purchase_cents,
		       staff_cash_fen_per_100_beans, minimum_staff_conversion_beans,
		       enabled, version, updated_by_user_id, updated_at
		FROM bean_commerce_settings WHERE id=1 FOR SHARE
	`).Scan(&settings.PurchaseBeansPerYuan, &settings.MinimumPurchaseCents,
		&settings.StaffCashFenPer100Beans, &settings.MinimumStaffConversionBeans,
		&settings.Enabled, &settings.Version, &settings.UpdatedByUserID, &settings.UpdatedAt); err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	if !settings.Enabled {
		return model.BeanPurchaseOrder{}, ErrBeanEconomyDisabled
	}
	if amountCents < settings.MinimumPurchaseCents {
		return model.BeanPurchaseOrder{}, ErrBeanInvalidInput
	}
	if settings.PurchaseBeansPerYuan > math.MaxUint64/amountCents {
		return model.BeanPurchaseOrder{}, ErrBeanInvalidInput
	}
	creditedBeans := amountCents * settings.PurchaseBeansPerYuan / 100
	if creditedBeans == 0 || creditedBeans > math.MaxInt64 {
		return model.BeanPurchaseOrder{}, ErrBeanInvalidInput
	}
	var cashWalletID int64
	var cashBalance int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id, balance_cents FROM fin_wallet_accounts
		WHERE tenant_id=? AND account_type='cash' AND currency='CNY' AND status='active'
		FOR UPDATE
	`, tenantID).Scan(&cashWalletID, &cashBalance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.BeanPurchaseOrder{}, ErrBeanCashInsufficient
		}
		return model.BeanPurchaseOrder{}, err
	}
	if cashBalance < int64(amountCents) {
		return model.BeanPurchaseOrder{}, ErrBeanCashInsufficient
	}
	purchaseNo, err := newFinanceReference("BP")
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO bean_purchase_orders (
			purchase_no, tenant_id, cash_amount_cents, beans_per_yuan_snapshot,
			credited_beans, operator_user_id, idempotency_key
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, purchaseNo, tenantID, amountCents, settings.PurchaseBeansPerYuan, creditedBeans, actorUserID, idempotencyKey)
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	purchaseID, err := result.LastInsertId()
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fin_wallet_accounts SET balance_cents=balance_cents-? WHERE id=?`, amountCents, cashWalletID); err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	cashExternalID, err := newFinanceReference("WL")
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	cashLedger, err := tx.ExecContext(ctx, `
		INSERT INTO fin_wallet_ledger (
			external_id, tenant_id, wallet_account_id, direction, amount_cents,
			balance_before_cents, balance_after_cents, business_type, business_id,
			order_no, operator_user_id, reason, idempotency_key, occurred_at
		) VALUES (?, ?, ?, 'debit', ?, ?, ?, 'bean_purchase', ?, ?, ?, ?, ?, ?)
	`, cashExternalID, tenantID, cashWalletID, amountCents, cashBalance, cashBalance-int64(amountCents),
		purchaseID, purchaseNo, actorUserID, "使用现金余额购买小蓝豆", "bean-purchase-cash:"+idempotencyKey, time.Now().UTC())
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	cashLedgerID, _ := cashLedger.LastInsertId()
	beanWallet, err := ensureBeanWalletTx(ctx, tx, "customer", tenantID)
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	referenceID := purchaseID
	_, beanLedgerID, err := applyBeanWalletTx(ctx, tx, beanWallet, int64(creditedBeans), 0,
		"purchase", "bean_purchase", &referenceID, actorUserID, "现金购买小蓝豆", "bean-purchase-credit:"+idempotencyKey)
	if err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bean_purchase_orders SET cash_ledger_id=?, bean_ledger_id=? WHERE id=?`, cashLedgerID, beanLedgerID, purchaseID); err != nil {
		return model.BeanPurchaseOrder{}, err
	}
	item := model.BeanPurchaseOrder{ID: purchaseID, PurchaseNo: purchaseNo, TenantID: tenantID,
		CashAmountCents: amountCents, BeansPerYuanSnapshot: settings.PurchaseBeansPerYuan,
		CreditedBeans: creditedBeans, Status: "paid", OperatorUserID: actorUserID, CreatedAt: time.Now().UTC()}
	return item, tx.Commit()
}

func scanBeanConversion(row interface{ Scan(...any) error }) (model.BeanConversionRequest, error) {
	var out model.BeanConversionRequest
	var approvedAt, paidAt sql.NullTime
	err := row.Scan(&out.ID, &out.ConversionNo, &out.UserID, &out.UserName, &out.BeanAmount,
		&out.CashAmountCents, &out.CashFenPer100BeansSnapshot, &out.Status, &out.RejectReason,
		&out.RequestedAt, &approvedAt, &paidAt)
	if approvedAt.Valid {
		value := approvedAt.Time
		out.ApprovedAt = &value
	}
	if paidAt.Valid {
		value := paidAt.Time
		out.PaidAt = &value
	}
	return out, err
}

const beanConversionSelect = `SELECT c.id, c.conversion_no, c.user_id, COALESCE(u.display_name,''),
	c.bean_amount, c.cash_amount_cents, c.cash_fen_per_100_beans_snapshot,
	c.status, c.reject_reason, c.requested_at, c.approved_at, c.paid_at
	FROM bean_conversion_requests c LEFT JOIN mgmt_users u ON u.id=c.user_id`

func (s *Store) StaffBeanWallet(ctx context.Context, userID int64, limit int) (model.BeanWalletDashboard, error) {
	out, err := s.beanWalletDashboard(ctx, "staff", userID, limit)
	if err != nil {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, beanConversionSelect+` WHERE c.user_id=? ORDER BY c.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Conversions = make([]model.BeanConversionRequest, 0)
	for rows.Next() {
		item, scanErr := scanBeanConversion(rows)
		if scanErr != nil {
			return out, scanErr
		}
		out.Conversions = append(out.Conversions, item)
	}
	return out, rows.Err()
}

func (s *Store) CreateBeanConversion(ctx context.Context, userID int64, beans uint64) (model.BeanConversionRequest, error) {
	if userID <= 0 || beans == 0 || beans > math.MaxInt64 {
		return model.BeanConversionRequest{}, ErrBeanInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	defer tx.Rollback()
	var settings model.BeanCommerceSettings
	if err := tx.QueryRowContext(ctx, `
		SELECT purchase_beans_per_yuan, minimum_purchase_cents,
		       staff_cash_fen_per_100_beans, minimum_staff_conversion_beans,
		       enabled, version, updated_by_user_id, updated_at
		FROM bean_commerce_settings WHERE id=1 FOR SHARE
	`).Scan(&settings.PurchaseBeansPerYuan, &settings.MinimumPurchaseCents,
		&settings.StaffCashFenPer100Beans, &settings.MinimumStaffConversionBeans,
		&settings.Enabled, &settings.Version, &settings.UpdatedByUserID, &settings.UpdatedAt); err != nil {
		return model.BeanConversionRequest{}, err
	}
	if !settings.Enabled {
		return model.BeanConversionRequest{}, ErrBeanEconomyDisabled
	}
	if beans < settings.MinimumStaffConversionBeans || settings.StaffCashFenPer100Beans > math.MaxUint64/beans {
		return model.BeanConversionRequest{}, ErrBeanInvalidInput
	}
	cashCents := beans * settings.StaffCashFenPer100Beans / 100
	if cashCents == 0 {
		return model.BeanConversionRequest{}, ErrBeanInvalidInput
	}
	wallet, err := ensureBeanWalletTx(ctx, tx, "staff", userID)
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	if wallet.AvailableBeans < beans {
		return model.BeanConversionRequest{}, ErrBeanInsufficientBalance
	}
	conversionNo, err := newFinanceReference("BC")
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO bean_conversion_requests (
			conversion_no, user_id, wallet_id, bean_amount, cash_amount_cents,
			cash_fen_per_100_beans_snapshot, requested_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, conversionNo, userID, wallet.ID, beans, cashCents, settings.StaffCashFenPer100Beans, userID)
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	conversionID, err := result.LastInsertId()
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	referenceID := conversionID
	if _, _, err := applyBeanWalletTx(ctx, tx, wallet, -int64(beans), int64(beans),
		"conversion_hold", "bean_conversion", &referenceID, userID, "员工小蓝豆兑付申请冻结", "bean-conversion-hold:"+conversionNo); err != nil {
		return model.BeanConversionRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeanConversionRequest{}, err
	}
	return s.getBeanConversion(ctx, conversionID)
}

func (s *Store) getBeanConversion(ctx context.Context, id int64) (model.BeanConversionRequest, error) {
	return scanBeanConversion(s.db.QueryRowContext(ctx, beanConversionSelect+` WHERE c.id=?`, id))
}

func (s *Store) ListBeanConversions(ctx context.Context, status string, limit int) ([]model.BeanConversionRequest, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := beanConversionSelect
	args := make([]any, 0, 2)
	if status != "" && status != "all" {
		query += ` WHERE c.status=?`
		args = append(args, status)
	}
	query += ` ORDER BY c.id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.BeanConversionRequest, 0)
	for rows.Next() {
		item, scanErr := scanBeanConversion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) TransitionBeanConversion(
	ctx context.Context,
	conversionID int64,
	action string,
	actorUserID int64,
	reason string,
) (model.BeanConversionRequest, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanConversionRequest{}, err
	}
	defer tx.Rollback()
	var userID, walletID int64
	var beans uint64
	var status string
	var requester int64
	if err := tx.QueryRowContext(ctx, `
		SELECT user_id, wallet_id, bean_amount, status, requested_by_user_id
		FROM bean_conversion_requests WHERE id=? FOR UPDATE
	`, conversionID).Scan(&userID, &walletID, &beans, &status, &requester); err != nil {
		return model.BeanConversionRequest{}, err
	}
	if actorUserID == requester {
		return model.BeanConversionRequest{}, ErrBeanConflict
	}
	now := time.Now().UTC()
	switch action {
	case "approve":
		if status != "reviewing" {
			return model.BeanConversionRequest{}, ErrBeanConflict
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bean_conversion_requests SET status='approved', approved_by_user_id=?, approved_at=? WHERE id=?`, actorUserID, now, conversionID); err != nil {
			return model.BeanConversionRequest{}, err
		}
	case "reject":
		if status != "reviewing" && status != "approved" {
			return model.BeanConversionRequest{}, ErrBeanConflict
		}
		wallet, err := scanBeanWallet(tx.QueryRowContext(ctx, `
			SELECT id, owner_type, owner_id, available_beans, frozen_beans, status, version, updated_at
			FROM bean_wallet_accounts WHERE id=? FOR UPDATE
		`, walletID))
		if err != nil {
			return model.BeanConversionRequest{}, err
		}
		referenceID := conversionID
		if _, _, err := applyBeanWalletTx(ctx, tx, wallet, int64(beans), -int64(beans),
			"conversion_rejected", "bean_conversion", &referenceID, actorUserID, "财务驳回员工小蓝豆兑付："+strings.TrimSpace(reason), "bean-conversion-reject:"+fmt.Sprint(conversionID)); err != nil {
			return model.BeanConversionRequest{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bean_conversion_requests SET status='rejected', reject_reason=? WHERE id=?`, strings.TrimSpace(reason), conversionID); err != nil {
			return model.BeanConversionRequest{}, err
		}
	case "pay":
		if status != "approved" {
			return model.BeanConversionRequest{}, ErrBeanConflict
		}
		wallet, err := scanBeanWallet(tx.QueryRowContext(ctx, `
			SELECT id, owner_type, owner_id, available_beans, frozen_beans, status, version, updated_at
			FROM bean_wallet_accounts WHERE id=? FOR UPDATE
		`, walletID))
		if err != nil {
			return model.BeanConversionRequest{}, err
		}
		referenceID := conversionID
		if _, _, err := applyBeanWalletTx(ctx, tx, wallet, 0, -int64(beans),
			"conversion_paid", "bean_conversion", &referenceID, actorUserID, "财务确认员工小蓝豆兑付打款", "bean-conversion-pay:"+fmt.Sprint(conversionID)); err != nil {
			return model.BeanConversionRequest{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bean_conversion_requests SET status='paid', paid_by_user_id=?, paid_at=? WHERE id=?`, actorUserID, now, conversionID); err != nil {
			return model.BeanConversionRequest{}, err
		}
	default:
		return model.BeanConversionRequest{}, ErrBeanInvalidInput
	}
	if err := tx.Commit(); err != nil {
		return model.BeanConversionRequest{}, err
	}
	return s.getBeanConversion(ctx, conversionID)
}

func (s *Store) BeanFinanceDashboard(ctx context.Context) (model.BeanFinanceDashboard, error) {
	summary, err := s.beanSummary(ctx)
	if err != nil {
		return model.BeanFinanceDashboard{}, err
	}
	conversions, err := s.ListBeanConversions(ctx, "all", 200)
	if err != nil {
		return model.BeanFinanceDashboard{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+beanLedgerColumns+` FROM bean_wallet_ledger ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return model.BeanFinanceDashboard{}, err
	}
	defer rows.Close()
	ledger := make([]model.BeanLedgerEntry, 0)
	for rows.Next() {
		entry, scanErr := scanBeanLedger(rows)
		if scanErr != nil {
			return model.BeanFinanceDashboard{}, scanErr
		}
		ledger = append(ledger, entry)
	}
	return model.BeanFinanceDashboard{Summary: summary, Conversions: conversions, RecentLedger: ledger}, rows.Err()
}

func marshalBeanMetadata(metadata map[string]any) any {
	if len(metadata) == 0 {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil
	}
	return string(raw)
}
