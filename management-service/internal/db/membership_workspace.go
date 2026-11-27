package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrMembershipArchived = errors.New("会员方案已归档")

const membershipCardReductionSQL = `JSON_VALUE(v.entitlements_json, '$.workspace_time_card_reduction_bps' RETURNING UNSIGNED)`
const membershipDeviceReductionSQL = `JSON_VALUE(v.entitlements_json, '$.workspace_device_reduction_bps' RETURNING UNSIGNED)`

func membershipPaidBPS(legacy uint32, reduction sql.NullInt64) uint32 {
	if reduction.Valid && reduction.Int64 >= 0 && reduction.Int64 <= 10000 {
		return uint32(10000 - reduction.Int64)
	}
	return normalizeMembershipDiscountBPS(legacy)
}

type WorkspaceMembershipInput struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	PriceCents        uint64 `json:"price_cents"`
	IncludedSeconds   uint64 `json:"included_seconds"`
	TimeCardReduction uint32 `json:"time_card_reduction_bps"`
	DeviceReduction   uint32 `json:"device_reduction_bps"`
	AllowAutoRenew    bool   `json:"allow_auto_renew"`
}

func (s *Store) SaveWorkspaceMembership(ctx context.Context, actorID, planID int64, input WorkspaceMembershipInput) error {
	input.AllowAutoRenew = false
	if input.TimeCardReduction > 10000 || input.DeviceReduction > 10000 {
		return errors.New("会员优惠不能超过 100%")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()

	if planID == 0 {
		code, err := newInternalCode(ctx, tx, "mem", now)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO catalog_membership_plans (code,name,description,status,sort_order)
			SELECT ?,?,?,'inactive',COALESCE(MAX(sort_order),0)+1 FROM catalog_membership_plans
		`, code, input.Name, input.Description)
		if err != nil {
			return err
		}
		planID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	} else {
		var status string
		if err := tx.QueryRowContext(ctx, "SELECT status FROM catalog_membership_plans WHERE id=? FOR UPDATE", planID).Scan(&status); err != nil {
			return err
		}
		if status == "archived" {
			return ErrMembershipArchived
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE catalog_membership_plans
			SET name=?,description=?,status=IF(status='draft','inactive',status) WHERE id=?
		`, input.Name, input.Description, planID); err != nil {
			return err
		}
	}

	var previousID int64
	var lifecycle string
	var price, seconds uint64
	var legacyCard, legacyDevice uint32
	var cardReduction, deviceReduction sql.NullInt64
	var renew bool
	err = tx.QueryRowContext(ctx, `
		SELECT v.id,v.lifecycle_status,v.price_cents,v.included_seconds,
		       v.default_time_card_discount_bps,v.default_device_discount_bps,
		       `+membershipCardReductionSQL+`,`+membershipDeviceReductionSQL+`,v.allow_auto_renew
		FROM catalog_membership_plan_versions v WHERE v.plan_id=?
		ORDER BY (v.lifecycle_status='active') DESC,(v.lifecycle_status='draft') DESC,v.version_no DESC LIMIT 1
	`, planID).Scan(&previousID, &lifecycle, &price, &seconds, &legacyCard, &legacyDevice, &cardReduction, &deviceReduction, &renew)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	if lifecycle != "active" || price != input.PriceCents || seconds != input.IncludedSeconds ||
		membershipPaidBPS(legacyCard, cardReduction) != 10000-input.TimeCardReduction ||
		membershipPaidBPS(legacyDevice, deviceReduction) != 10000-input.DeviceReduction || renew != input.AllowAutoRenew {
		if err := saveMembershipTerms(ctx, tx, actorID, planID, previousID, input, now); err != nil {
			return err
		}
	}
	if err := insertCommercialAuditTx(ctx, tx, actorID, "membership.plan.save", "membership_plan", fmt.Sprint(planID), nil, input); err != nil {
		return err
	}
	return tx.Commit()
}

// Sales and existing memberships reference the terms that were in effect at purchase.
func saveMembershipTerms(ctx context.Context, tx *sql.Tx, actorID, planID, previousID int64, input WorkspaceMembershipInput, now time.Time) error {
	var nextVersion uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no),0)+1 FROM catalog_membership_plan_versions WHERE plan_id=?
	`, planID).Scan(&nextVersion); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_membership_plan_versions SET lifecycle_status='retired',effective_to=?
		WHERE plan_id=? AND lifecycle_status IN ('active','draft')
	`, now, planID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_membership_plan_versions
			(plan_id,version_no,lifecycle_status,currency,price_cents,
			 recurring_month_discount_bps,recurring_quarter_discount_bps,annual_discount_bps,
			 billing_period_unit,billing_period_count,included_seconds,
			 default_time_card_discount_bps,default_device_discount_bps,allow_auto_renew,
			 participates_referral,entitlements_json,effective_from,created_by_user_id,published_by_user_id,published_at)
		SELECT ?,?,'active',COALESCE(v.currency,'CNY'),?,
		       COALESCE(v.recurring_month_discount_bps,10000),COALESCE(v.recurring_quarter_discount_bps,10000),
		       COALESCE(v.annual_discount_bps,10000),COALESCE(v.billing_period_unit,'month'),COALESCE(v.billing_period_count,1),
		       ?,?,?,?,COALESCE(v.participates_referral,1),
		       JSON_SET(IF(JSON_TYPE(v.entitlements_json)='OBJECT',v.entitlements_json,JSON_OBJECT()),
		           '$.workspace_time_card_reduction_bps',CAST(? AS UNSIGNED),
		           '$.workspace_device_reduction_bps',CAST(? AS UNSIGNED)),?,?,?,?
		FROM (SELECT 1) seed LEFT JOIN catalog_membership_plan_versions v ON v.id=?
	`, planID, nextVersion, input.PriceCents, input.IncludedSeconds,
		max(1000, 10000-input.TimeCardReduction), max(1000, 10000-input.DeviceReduction), input.AllowAutoRenew,
		input.TimeCardReduction, input.DeviceReduction, now, actorID, actorID, now, previousID)
	if err != nil {
		return err
	}
	nextID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO catalog_membership_card_discounts (plan_version_id,card_version_id,discount_bps)
		SELECT ?,card_version_id,discount_bps FROM catalog_membership_card_discounts WHERE plan_version_id=?
	`, nextID, previousID)
	return err
}
