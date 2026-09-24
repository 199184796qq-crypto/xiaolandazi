package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

func normalizeMembershipDiscountBPS(value uint32) uint32 {
	// Legacy editor values such as 9折/8折 were once persisted as 900/800 BPS.
	// Treat that historical range as 9000/8000 BPS. New writes use canonical
	// basis points directly (9折 = 9000, 8.5折 = 8500, 原价 = 10000).
	if value >= 100 && value < 1000 {
		value *= 10
	}
	if value == 0 || value > 10000 {
		return 10000
	}
	return value
}

func (s *Store) ListCommercialMembershipPlans(ctx context.Context) ([]model.CommercialMembershipPlan, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, name, description, status, sort_order, created_at, updated_at
		FROM catalog_membership_plans
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialMembershipPlan, 0)
	for rows.Next() {
		var item model.CommercialMembershipPlan
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.Description,
			&item.Status,
			&item.SortOrder,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		versions, err := s.listMembershipVersions(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		for j := range versions {
			version := versions[j]
			if items[i].LatestVersion == nil || version.VersionNo > items[i].LatestVersion.VersionNo {
				copy := version
				items[i].LatestVersion = &copy
			}
			switch version.LifecycleStatus {
			case "active":
				copy := version
				items[i].ActiveVersion = &copy
			case "draft":
				if items[i].DraftVersion == nil || version.VersionNo > items[i].DraftVersion.VersionNo {
					copy := version
					items[i].DraftVersion = &copy
				}
			}
		}
	}

	campaigns, err := s.ListMarketingCampaigns(ctx, "membership", 0, false)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].MarketingCampaigns = make([]model.MarketingCampaign, 0)
		for _, campaign := range campaigns {
			if marketingCampaignHasTarget(campaign, "membership", items[i].ID) {
				items[i].MarketingCampaigns = append(items[i].MarketingCampaigns, campaign)
			}
		}
	}

	return items, nil
}

func (s *Store) ListCustomerMembershipOffers(ctx context.Context) ([]model.CustomerMembershipOffer, error) {
	plans, err := s.ListCommercialMembershipPlans(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]model.CustomerMembershipOffer, 0)
	for _, plan := range plans {
		if plan.Status != "active" || plan.ActiveVersion == nil {
			continue
		}
		version := plan.ActiveVersion
		activeCampaigns := make([]model.MarketingCampaign, 0)
		for _, campaign := range plan.MarketingCampaigns {
			if marketingCampaignWindowActive(campaign, time.Now().UTC()) &&
				marketingCampaignHasDisplayLocation(campaign, "membership") {
				activeCampaigns = append(activeCampaigns, campaign)
			}
		}
		items = append(items, model.CustomerMembershipOffer{
			ID:                          plan.ID,
			Code:                        plan.Code,
			Name:                        plan.Name,
			Description:                 plan.Description,
			MonthlyPriceCents:           version.PriceCents,
			RecurringMonthDiscountBPS:   version.RecurringMonthDiscountBPS,
			RecurringQuarterDiscountBPS: version.RecurringQuarterDiscountBPS,
			AnnualDiscountBPS:           version.AnnualDiscountBPS,
			IncludedSeconds:             version.IncludedSeconds,
			TimeCardDiscountBPS:         version.DefaultTimeCardDiscountBPS,
			DeviceDiscountBPS:           version.DefaultDeviceDiscountBPS,
			AllowAutoRenew:              version.AllowAutoRenew,
			VersionNo:                   version.VersionNo,
			MarketingCampaigns:          activeCampaigns,
		})
	}
	return items, nil
}

func (s *Store) GetCommercialMembershipPlan(
	ctx context.Context,
	planID int64,
) (model.CommercialMembershipPlan, error) {
	items, err := s.ListCommercialMembershipPlans(ctx)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	for _, item := range items {
		if item.ID == planID {
			return item, nil
		}
	}
	return model.CommercialMembershipPlan{}, sql.ErrNoRows
}

func (s *Store) listMembershipVersions(
	ctx context.Context,
	planID int64,
) ([]model.CommercialMembershipVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, plan_id, version_no, lifecycle_status, currency, price_cents,
			recurring_month_discount_bps, recurring_quarter_discount_bps, annual_discount_bps,
			billing_period_unit, billing_period_count, included_seconds,
			default_time_card_discount_bps, default_device_discount_bps, allow_auto_renew,
			effective_from, effective_to, published_at, created_at
		FROM catalog_membership_plan_versions
		WHERE plan_id = ?
		ORDER BY version_no DESC
	`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialMembershipVersion, 0)
	for rows.Next() {
		var item model.CommercialMembershipVersion
		if err := rows.Scan(
			&item.ID,
			&item.PlanID,
			&item.VersionNo,
			&item.LifecycleStatus,
			&item.Currency,
			&item.PriceCents,
			&item.RecurringMonthDiscountBPS,
			&item.RecurringQuarterDiscountBPS,
			&item.AnnualDiscountBPS,
			&item.BillingPeriodUnit,
			&item.BillingPeriodCount,
			&item.IncludedSeconds,
			&item.DefaultTimeCardDiscountBPS,
			&item.DefaultDeviceDiscountBPS,
			&item.AllowAutoRenew,
			&item.EffectiveFrom,
			&item.EffectiveTo,
			&item.PublishedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.RecurringMonthDiscountBPS = normalizeMembershipDiscountBPS(item.RecurringMonthDiscountBPS)
		item.RecurringQuarterDiscountBPS = normalizeMembershipDiscountBPS(item.RecurringQuarterDiscountBPS)
		item.AnnualDiscountBPS = normalizeMembershipDiscountBPS(item.AnnualDiscountBPS)
		item.DefaultTimeCardDiscountBPS = normalizeMembershipDiscountBPS(item.DefaultTimeCardDiscountBPS)
		item.DefaultDeviceDiscountBPS = normalizeMembershipDiscountBPS(item.DefaultDeviceDiscountBPS)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateCommercialMembershipPlan(
	ctx context.Context,
	actorUserID int64,
	input model.CommercialMembershipInput,
) (model.CommercialMembershipPlan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_membership_plans (
			code, name, description, status, sort_order
		)
		VALUES (?, ?, ?, 'draft', ?)
	`, input.Code, input.Name, input.Description, input.SortOrder)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	planID, err := result.LastInsertId()
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_membership_plan_versions (
			plan_id, version_no, lifecycle_status, currency, price_cents,
			recurring_month_discount_bps, recurring_quarter_discount_bps, annual_discount_bps,
			billing_period_unit, billing_period_count, included_seconds,
			default_time_card_discount_bps, default_device_discount_bps, allow_auto_renew,
			created_by_user_id
		)
		VALUES (?, 1, 'draft', 'CNY', ?, ?, ?, ?, 'month', 1, ?, ?, ?, ?, ?)
	`,
		planID,
		input.PriceCents,
		input.RecurringMonthDiscountBPS,
		input.RecurringQuarterDiscountBPS,
		input.AnnualDiscountBPS,
		input.IncludedSeconds,
		input.DefaultTimeCardDiscountBPS,
		input.DefaultDeviceDiscountBPS,
		input.AllowAutoRenew,
		actorUserID,
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if err := insertCommercialAuditTx(
		ctx,
		tx,
		actorUserID,
		"membership.plan.create",
		"membership_plan",
		fmt.Sprintf("%d", planID),
		nil,
		input,
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	return s.GetCommercialMembershipPlan(ctx, planID)
}

func (s *Store) SaveCommercialMembershipDraft(
	ctx context.Context,
	actorUserID int64,
	planID int64,
	input model.CommercialMembershipInput,
) (model.CommercialMembershipPlan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	defer tx.Rollback()

	var existingCode string
	var existingName string
	var existingDescription string
	var existingSortOrder int
	if err := tx.QueryRowContext(ctx, `
		SELECT code, name, description, sort_order
		FROM catalog_membership_plans
		WHERE id = ?
		FOR UPDATE
	`, planID).Scan(
		&existingCode,
		&existingName,
		&existingDescription,
		&existingSortOrder,
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	before := map[string]any{
		"code":        existingCode,
		"name":        existingName,
		"description": existingDescription,
		"sort_order":  existingSortOrder,
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_membership_plans
		SET code = ?, name = ?, description = ?, sort_order = ?
		WHERE id = ?
	`, input.Code, input.Name, input.Description, input.SortOrder, planID); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	var draftID int64
	var draftVersionNo uint32
	err = tx.QueryRowContext(ctx, `
		SELECT id, version_no
		FROM catalog_membership_plan_versions
		WHERE plan_id = ? AND lifecycle_status = 'draft'
		ORDER BY version_no DESC
		LIMIT 1
		FOR UPDATE
	`, planID).Scan(&draftID, &draftVersionNo)

	switch {
	case err == nil:
		if _, err := tx.ExecContext(ctx, `
			UPDATE catalog_membership_plan_versions
			SET
				price_cents = ?,
				recurring_month_discount_bps = ?,
				recurring_quarter_discount_bps = ?,
				annual_discount_bps = ?,
				included_seconds = ?,
				default_time_card_discount_bps = ?,
				default_device_discount_bps = ?,
				allow_auto_renew = ?
			WHERE id = ?
		`,
			input.PriceCents,
			input.RecurringMonthDiscountBPS,
			input.RecurringQuarterDiscountBPS,
			input.AnnualDiscountBPS,
			input.IncludedSeconds,
			input.DefaultTimeCardDiscountBPS,
			input.DefaultDeviceDiscountBPS,
			input.AllowAutoRenew,
			draftID,
		); err != nil {
			return model.CommercialMembershipPlan{}, err
		}
	case errors.Is(err, sql.ErrNoRows):
		var maxVersionNo uint32
		var activePrice uint64
		var activeRecurringMonthDiscount uint32
		var activeRecurringQuarterDiscount uint32
		var activeAnnualDiscount uint32
		var activeIncluded uint64
		var activeDiscount uint32
		var activeDeviceDiscount uint32
		var activeAutoRenew bool
		copyErr := tx.QueryRowContext(ctx, `
			SELECT
				version_no, price_cents,
				recurring_month_discount_bps, recurring_quarter_discount_bps, annual_discount_bps,
				included_seconds, default_time_card_discount_bps,
				default_device_discount_bps, allow_auto_renew
			FROM catalog_membership_plan_versions
			WHERE plan_id = ?
			ORDER BY version_no DESC
			LIMIT 1
			FOR UPDATE
		`, planID).Scan(
			&maxVersionNo,
			&activePrice,
			&activeRecurringMonthDiscount,
			&activeRecurringQuarterDiscount,
			&activeAnnualDiscount,
			&activeIncluded,
			&activeDiscount,
			&activeDeviceDiscount,
			&activeAutoRenew,
		)
		if copyErr != nil && !errors.Is(copyErr, sql.ErrNoRows) {
			return model.CommercialMembershipPlan{}, copyErr
		}
		draftVersionNo = maxVersionNo + 1
		if draftVersionNo == 0 {
			draftVersionNo = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO catalog_membership_plan_versions (
				plan_id, version_no, lifecycle_status, currency, price_cents,
				recurring_month_discount_bps, recurring_quarter_discount_bps, annual_discount_bps,
				billing_period_unit, billing_period_count, included_seconds,
				default_time_card_discount_bps, default_device_discount_bps, allow_auto_renew,
				created_by_user_id
			)
			VALUES (?, ?, 'draft', 'CNY', ?, ?, ?, ?, 'month', 1, ?, ?, ?, ?, ?)
		`,
			planID,
			draftVersionNo,
			input.PriceCents,
			input.RecurringMonthDiscountBPS,
			input.RecurringQuarterDiscountBPS,
			input.AnnualDiscountBPS,
			input.IncludedSeconds,
			input.DefaultTimeCardDiscountBPS,
			input.DefaultDeviceDiscountBPS,
			input.AllowAutoRenew,
			actorUserID,
		); err != nil {
			return model.CommercialMembershipPlan{}, err
		}
	default:
		return model.CommercialMembershipPlan{}, err
	}

	if err := insertCommercialAuditTx(
		ctx,
		tx,
		actorUserID,
		"membership.plan.draft_save",
		"membership_plan",
		fmt.Sprintf("%d", planID),
		before,
		input,
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	return s.GetCommercialMembershipPlan(ctx, planID)
}

func (s *Store) PublishCommercialMembershipDraft(
	ctx context.Context,
	actorUserID int64,
	planID int64,
) (model.CommercialMembershipPlan, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	defer tx.Rollback()

	var planName string
	if err := tx.QueryRowContext(ctx, `
		SELECT name
		FROM catalog_membership_plans
		WHERE id = ?
		FOR UPDATE
	`, planID).Scan(&planName); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	var draftID int64
	var draftVersionNo uint32
	if err := tx.QueryRowContext(ctx, `
		SELECT id, version_no
		FROM catalog_membership_plan_versions
		WHERE plan_id = ? AND lifecycle_status = 'draft'
		ORDER BY version_no DESC
		LIMIT 1
		FOR UPDATE
	`, planID).Scan(&draftID, &draftVersionNo); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	now := time.Now().UTC()

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_membership_plan_versions
		SET lifecycle_status = 'retired', effective_to = ?
		WHERE plan_id = ? AND lifecycle_status = 'active'
	`, now, planID); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_membership_plan_versions
		SET
			lifecycle_status = 'active',
			effective_from = ?,
			effective_to = NULL,
			published_by_user_id = ?,
			published_at = ?
		WHERE id = ?
	`, now, actorUserID, now, draftID); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_membership_plans
		SET status = 'active'
		WHERE id = ?
	`, planID); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	after := map[string]any{
		"plan_name":         planName,
		"published_version": draftVersionNo,
		"published_at":      now,
	}
	if err := insertCommercialAuditTx(
		ctx,
		tx,
		actorUserID,
		"membership.plan.publish",
		"membership_plan",
		fmt.Sprintf("%d", planID),
		nil,
		after,
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	return s.GetCommercialMembershipPlan(ctx, planID)
}

func (s *Store) SetCommercialMembershipStatus(
	ctx context.Context,
	actorUserID int64,
	planID int64,
	status string,
) (model.CommercialMembershipPlan, error) {
	if status != "active" && status != "inactive" && status != "archived" {
		return model.CommercialMembershipPlan{}, fmt.Errorf("invalid membership status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	defer tx.Rollback()

	var name, beforeStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT name, status FROM catalog_membership_plans WHERE id=? FOR UPDATE
	`, planID).Scan(&name, &beforeStatus); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	if status == "active" {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM catalog_membership_plan_versions
			WHERE plan_id=? AND lifecycle_status='active'
		`, planID).Scan(&count); err != nil {
			return model.CommercialMembershipPlan{}, err
		}
		if count == 0 {
			return model.CommercialMembershipPlan{}, fmt.Errorf("membership has no published version")
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_membership_plans SET status=? WHERE id=?`, status, planID); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	if err := insertCommercialAuditTx(
		ctx, tx, actorUserID, "membership.plan.status", "membership_plan", fmt.Sprintf("%d", planID),
		map[string]any{"name": name, "status": beforeStatus},
		map[string]any{"name": name, "status": status},
	); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CommercialMembershipPlan{}, err
	}
	return s.GetCommercialMembershipPlan(ctx, planID)
}

func insertCommercialAuditTx(
	ctx context.Context,
	tx *sql.Tx,
	actorUserID int64,
	action string,
	entityType string,
	entityID string,
	before any,
	after any,
) error {
	var beforeJSON []byte
	var afterJSON []byte
	var err error

	if before != nil {
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return err
		}
	}
	if after != nil {
		afterJSON, err = json.Marshal(after)
		if err != nil {
			return err
		}
	}

	var beforeValue any
	var afterValue any
	if len(beforeJSON) > 0 {
		beforeValue = string(beforeJSON)
	}
	if len(afterJSON) > 0 {
		afterValue = string(afterJSON)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO biz_audit_events (
			actor_user_id, action, entity_type, entity_id,
			before_json, after_json
		)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		actorUserID,
		action,
		entityType,
		entityID,
		beforeValue,
		afterValue,
	)
	return err
}
