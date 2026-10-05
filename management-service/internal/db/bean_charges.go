package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

type BeanChargeReservationInput struct {
	ActionCode        string
	PayerOwnerType    string
	PayerOwnerID      int64
	BeneficiaryUserID *int64
	Units             uint64
	ReferenceType     string
	ReferenceID       *int64
	IdempotencyKey    string
	Metadata          map[string]any
	OperatorUserID    int64
}

func calculateBeanCharge(
	chargeMode string,
	beansPerUnit uint64,
	unitSize uint64,
	minimum uint64,
	maximum uint64,
	units uint64,
) (uint64, error) {
	if beansPerUnit == 0 || unitSize == 0 {
		return 0, ErrBeanInvalidInput
	}
	if units == 0 {
		units = 1
	}
	count := uint64(1)
	if chargeMode == "per_unit" {
		count = (units + unitSize - 1) / unitSize
	} else if chargeMode != "fixed" {
		return 0, ErrBeanInvalidInput
	}
	if count > math.MaxUint64/beansPerUnit {
		return 0, ErrBeanInvalidInput
	}
	quoted := count * beansPerUnit
	if quoted < minimum {
		quoted = minimum
	}
	if maximum > 0 && quoted > maximum {
		quoted = maximum
	}
	if quoted == 0 || quoted > math.MaxInt64 {
		return 0, ErrBeanInvalidInput
	}
	return quoted, nil
}

func (s *Store) QuoteBeanCharge(ctx context.Context, actionCode string, units uint64) (model.BeanChargeQuote, error) {
	settings, err := s.BeanCommerceSettings(ctx)
	if err != nil {
		return model.BeanChargeQuote{}, err
	}
	if !settings.Enabled {
		return model.BeanChargeQuote{ActionCode: strings.TrimSpace(actionCode), Units: units, PricingEnabled: false}, ErrBeanEconomyDisabled
	}
	rule, err := scanBeanPricingRule(s.db.QueryRowContext(ctx, `
		SELECT `+beanPricingColumns+` FROM bean_pricing_rules WHERE action_code=?
	`, strings.ToLower(strings.TrimSpace(actionCode))))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.BeanChargeQuote{ActionCode: strings.TrimSpace(actionCode), Units: units, PricingEnabled: false}, ErrBeanPricingDisabled
		}
		return model.BeanChargeQuote{}, err
	}
	if !rule.Enabled {
		return model.BeanChargeQuote{ActionCode: rule.ActionCode, ActionName: rule.ActionName, Units: units,
			RuleID: rule.ID, RuleVersion: rule.Version, PricingEnabled: false}, ErrBeanPricingDisabled
	}
	quoted, err := calculateBeanCharge(rule.ChargeMode, rule.BeansPerUnit, rule.UnitSize,
		rule.MinimumChargeBeans, rule.MaximumChargeBeans, units)
	if err != nil {
		return model.BeanChargeQuote{}, err
	}
	return model.BeanChargeQuote{ActionCode: rule.ActionCode, ActionName: rule.ActionName, Units: units,
		QuotedBeans: quoted, RuleID: rule.ID, RuleVersion: rule.Version, PricingEnabled: true}, nil
}

type beanChargeSnapshot struct {
	model.BeanCharge
	PayerWalletID      int64
	RuleID             int64
	RuleVersion        uint64
	ChargeMode         string
	BeansPerUnit       uint64
	UnitSize           uint64
	MinimumChargeBeans uint64
	MaximumChargeBeans uint64
	StaffRewardBPS     uint32
}

func scanBeanCharge(row interface{ Scan(...any) error }) (beanChargeSnapshot, error) {
	var out beanChargeSnapshot
	var beneficiary, referenceID sql.NullInt64
	var metadata []byte
	var settledAt sql.NullTime
	err := row.Scan(
		&out.ID, &out.ExternalID, &out.ActionCode, &out.PayerOwnerType, &out.PayerOwnerID,
		&out.PayerWalletID, &beneficiary, &out.RuleID, &out.RuleVersion,
		&out.ChargeMode, &out.BeansPerUnit, &out.UnitSize, &out.MinimumChargeBeans,
		&out.MaximumChargeBeans, &out.StaffRewardBPS, &out.RequestedUnits, &out.ActualUnits,
		&out.QuotedBeans, &out.ReservedBeans, &out.ChargedBeans, &out.StaffRewardBeans,
		&out.PlatformBeans, &out.Status, &out.ReferenceType, &referenceID, &metadata,
		&out.CreatedAt, &settledAt,
	)
	if beneficiary.Valid {
		value := beneficiary.Int64
		out.BeneficiaryUserID = &value
	}
	if referenceID.Valid {
		value := referenceID.Int64
		out.ReferenceID = &value
	}
	if len(metadata) > 0 {
		out.Metadata = metadata
	}
	if settledAt.Valid {
		value := settledAt.Time
		out.SettledAt = &value
	}
	return out, err
}

const beanChargeColumns = `id, external_id, action_code, payer_owner_type, payer_owner_id,
	payer_wallet_id, beneficiary_user_id, rule_id, rule_version, charge_mode,
	beans_per_unit, unit_size, minimum_charge_beans, maximum_charge_beans,
	staff_reward_bps, requested_units, actual_units, quoted_beans, reserved_beans,
	charged_beans, staff_reward_beans, platform_beans, status, reference_type,
	reference_id, metadata_json, created_at, settled_at`

func (s *Store) getBeanCharge(ctx context.Context, externalID string) (beanChargeSnapshot, error) {
	return scanBeanCharge(s.db.QueryRowContext(ctx, `SELECT `+beanChargeColumns+` FROM bean_charges WHERE external_id=?`, externalID))
}

func (s *Store) ReserveBeanCharge(ctx context.Context, input BeanChargeReservationInput) (model.BeanCharge, error) {
	input.ActionCode = strings.ToLower(strings.TrimSpace(input.ActionCode))
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	input.ReferenceType = strings.TrimSpace(input.ReferenceType)
	if input.PayerOwnerType == "" {
		input.PayerOwnerType = "customer"
	}
	if input.PayerOwnerID <= 0 || input.IdempotencyKey == "" || !beanActionCodePattern.MatchString(input.ActionCode) {
		return model.BeanCharge{}, ErrBeanInvalidInput
	}
	quote, err := s.QuoteBeanCharge(ctx, input.ActionCode, input.Units)
	if err != nil {
		return model.BeanCharge{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanCharge{}, err
	}
	defer tx.Rollback()
	if existing, scanErr := scanBeanCharge(tx.QueryRowContext(ctx, `SELECT `+beanChargeColumns+` FROM bean_charges WHERE idempotency_key=?`, input.IdempotencyKey)); scanErr == nil {
		return existing.BeanCharge, tx.Commit()
	} else if !errors.Is(scanErr, sql.ErrNoRows) {
		return model.BeanCharge{}, scanErr
	}
	rule, err := scanBeanPricingRule(tx.QueryRowContext(ctx, `SELECT `+beanPricingColumns+` FROM bean_pricing_rules WHERE id=? FOR SHARE`, quote.RuleID))
	if err != nil {
		return model.BeanCharge{}, err
	}
	if !rule.Enabled || rule.Version != quote.RuleVersion {
		return model.BeanCharge{}, ErrBeanConflict
	}
	wallet, err := ensureBeanWalletTx(ctx, tx, input.PayerOwnerType, input.PayerOwnerID)
	if err != nil {
		return model.BeanCharge{}, err
	}
	if wallet.AvailableBeans < quote.QuotedBeans {
		return model.BeanCharge{}, ErrBeanInsufficientBalance
	}
	externalID, err := newFinanceReference("CHG")
	if err != nil {
		return model.BeanCharge{}, err
	}
	var beneficiary any
	if input.BeneficiaryUserID != nil && *input.BeneficiaryUserID > 0 {
		beneficiary = *input.BeneficiaryUserID
	}
	var referenceID any
	if input.ReferenceID != nil {
		referenceID = *input.ReferenceID
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO bean_charges (
			external_id, idempotency_key, action_code, payer_owner_type, payer_owner_id,
			payer_wallet_id, beneficiary_user_id, rule_id, rule_version, charge_mode,
			beans_per_unit, unit_size, minimum_charge_beans, maximum_charge_beans,
			staff_reward_bps, requested_units, quoted_beans, reserved_beans,
			reference_type, reference_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, externalID, input.IdempotencyKey, rule.ActionCode, input.PayerOwnerType, input.PayerOwnerID,
		wallet.ID, beneficiary, rule.ID, rule.Version, rule.ChargeMode,
		rule.BeansPerUnit, rule.UnitSize, rule.MinimumChargeBeans, rule.MaximumChargeBeans,
		rule.StaffRewardBPS, input.Units, quote.QuotedBeans, quote.QuotedBeans,
		input.ReferenceType, referenceID, marshalBeanMetadata(input.Metadata))
	if err != nil {
		return model.BeanCharge{}, err
	}
	chargeID, err := result.LastInsertId()
	if err != nil {
		return model.BeanCharge{}, err
	}
	reference := chargeID
	if _, _, err := applyBeanWalletTx(ctx, tx, wallet, -int64(quote.QuotedBeans), int64(quote.QuotedBeans),
		"charge_reserve", "bean_charge", &reference, input.OperatorUserID, "小蓝豆消费预冻结", "bean-charge-reserve:"+input.IdempotencyKey); err != nil {
		return model.BeanCharge{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeanCharge{}, err
	}
	charge, err := s.getBeanCharge(ctx, externalID)
	return charge.BeanCharge, err
}

func (s *Store) SettleBeanCharge(ctx context.Context, externalID string, actualUnits uint64, operatorUserID int64) (model.BeanCharge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanCharge{}, err
	}
	defer tx.Rollback()
	charge, err := scanBeanCharge(tx.QueryRowContext(ctx, `SELECT `+beanChargeColumns+` FROM bean_charges WHERE external_id=? FOR UPDATE`, strings.TrimSpace(externalID)))
	if err != nil {
		return model.BeanCharge{}, err
	}
	if charge.Status == "settled" {
		return charge.BeanCharge, tx.Commit()
	}
	if charge.Status != "reserved" {
		return model.BeanCharge{}, ErrBeanConflict
	}
	charged, err := calculateBeanCharge(charge.ChargeMode, charge.BeansPerUnit, charge.UnitSize,
		charge.MinimumChargeBeans, charge.MaximumChargeBeans, actualUnits)
	if err != nil {
		return model.BeanCharge{}, err
	}
	if charged > charge.ReservedBeans {
		return model.BeanCharge{}, ErrBeanConflict
	}
	payer, err := scanBeanWallet(tx.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, available_beans, frozen_beans, status, version, updated_at
		FROM bean_wallet_accounts WHERE id=? FOR UPDATE
	`, charge.PayerWalletID))
	if err != nil {
		return model.BeanCharge{}, err
	}
	refund := charge.ReservedBeans - charged
	referenceID := charge.ID
	if _, _, err := applyBeanWalletTx(ctx, tx, payer, int64(refund), -int64(charge.ReservedBeans),
		"charge_settle", "bean_charge", &referenceID, operatorUserID, "小蓝豆消费结算", "bean-charge-settle:payer:"+fmt.Sprint(charge.ID)); err != nil {
		return model.BeanCharge{}, err
	}
	staffReward := uint64(0)
	if charge.BeneficiaryUserID != nil && charge.StaffRewardBPS > 0 {
		staffReward = charged * uint64(charge.StaffRewardBPS) / 10_000
		if staffReward > 0 {
			staffWallet, err := ensureBeanWalletTx(ctx, tx, "staff", *charge.BeneficiaryUserID)
			if err != nil {
				return model.BeanCharge{}, err
			}
			if _, _, err := applyBeanWalletTx(ctx, tx, staffWallet, int64(staffReward), 0,
				"service_reward", "bean_charge", &referenceID, operatorUserID, "完成服务获得小蓝豆", "bean-charge-settle:staff:"+fmt.Sprint(charge.ID)); err != nil {
				return model.BeanCharge{}, err
			}
		}
	}
	platformBeans := charged - staffReward
	if platformBeans > 0 {
		platformWallet, err := ensureBeanWalletTx(ctx, tx, "platform", 1)
		if err != nil {
			return model.BeanCharge{}, err
		}
		if _, _, err := applyBeanWalletTx(ctx, tx, platformWallet, int64(platformBeans), 0,
			"platform_income", "bean_charge", &referenceID, operatorUserID, "小蓝豆平台结算收入", "bean-charge-settle:platform:"+fmt.Sprint(charge.ID)); err != nil {
			return model.BeanCharge{}, err
		}
	}
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE bean_charges
		SET actual_units=?, charged_beans=?, staff_reward_beans=?, platform_beans=?,
		    status='settled', settled_at=?
		WHERE id=?
	`, actualUnits, charged, staffReward, platformBeans, now, charge.ID); err != nil {
		return model.BeanCharge{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeanCharge{}, err
	}
	out, err := s.getBeanCharge(ctx, externalID)
	return out.BeanCharge, err
}

func (s *Store) ReleaseBeanCharge(ctx context.Context, externalID string, operatorUserID int64, reason string) (model.BeanCharge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.BeanCharge{}, err
	}
	defer tx.Rollback()
	charge, err := scanBeanCharge(tx.QueryRowContext(ctx, `SELECT `+beanChargeColumns+` FROM bean_charges WHERE external_id=? FOR UPDATE`, strings.TrimSpace(externalID)))
	if err != nil {
		return model.BeanCharge{}, err
	}
	if charge.Status == "released" {
		return charge.BeanCharge, tx.Commit()
	}
	if charge.Status != "reserved" {
		return model.BeanCharge{}, ErrBeanConflict
	}
	payer, err := scanBeanWallet(tx.QueryRowContext(ctx, `
		SELECT id, owner_type, owner_id, available_beans, frozen_beans, status, version, updated_at
		FROM bean_wallet_accounts WHERE id=? FOR UPDATE
	`, charge.PayerWalletID))
	if err != nil {
		return model.BeanCharge{}, err
	}
	referenceID := charge.ID
	if _, _, err := applyBeanWalletTx(ctx, tx, payer, int64(charge.ReservedBeans), -int64(charge.ReservedBeans),
		"charge_release", "bean_charge", &referenceID, operatorUserID, "小蓝豆消费解冻："+strings.TrimSpace(reason), "bean-charge-release:"+fmt.Sprint(charge.ID)); err != nil {
		return model.BeanCharge{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE bean_charges SET status='released', settled_at=? WHERE id=?`, time.Now().UTC(), charge.ID); err != nil {
		return model.BeanCharge{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.BeanCharge{}, err
	}
	out, err := s.getBeanCharge(ctx, externalID)
	return out.BeanCharge, err
}
