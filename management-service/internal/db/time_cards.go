package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) ListCommercialTimeCards(ctx context.Context) ([]model.CommercialTimeCardProduct, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, name, description, status, sort_order, created_at, updated_at
		FROM catalog_time_card_products
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialTimeCardProduct, 0)
	for rows.Next() {
		var item model.CommercialTimeCardProduct
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
		versions, err := s.listTimeCardVersions(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
		for j := range versions {
			version := versions[j]
			if items[i].LatestVersion == nil || version.VersionNo > items[i].LatestVersion.VersionNo {
				copy := version
				items[i].LatestVersion = &copy
			}
			if version.LifecycleStatus == "published" {
				copy := version
				items[i].ActiveVersion = &copy
			}
			if version.LifecycleStatus == "draft" {
				copy := version
				items[i].DraftVersion = &copy
			}
		}
	}
	campaigns, err := s.ListMarketingCampaigns(ctx, "time_card", 0, false)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].MarketingCampaigns = make([]model.MarketingCampaign, 0)
		for _, campaign := range campaigns {
			if marketingCampaignHasTarget(campaign, "time_card", items[i].ID) {
				items[i].MarketingCampaigns = append(items[i].MarketingCampaigns, campaign)
			}
		}
	}
	return items, nil
}

func (s *Store) GetCommercialTimeCard(ctx context.Context, productID int64) (model.CommercialTimeCardProduct, error) {
	items, err := s.ListCommercialTimeCards(ctx)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	for _, item := range items {
		if item.ID == productID {
			return item, nil
		}
	}
	return model.CommercialTimeCardProduct{}, sql.ErrNoRows
}

func (s *Store) listTimeCardVersions(ctx context.Context, productID int64) ([]model.CommercialTimeCardVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, product_id, version_no, lifecycle_status, currency,
		       price_cents, duration_seconds, validity_days,
		       activation_mode, activation_deadline_days,
		       participates_referral, participates_sales_commission,
		       participates_agent_settlement, effective_from, effective_to,
		       published_at, created_at
		FROM catalog_time_card_versions
		WHERE product_id=?
		ORDER BY version_no ASC
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialTimeCardVersion, 0)
	for rows.Next() {
		var item model.CommercialTimeCardVersion
		if err := rows.Scan(
			&item.ID,
			&item.ProductID,
			&item.VersionNo,
			&item.LifecycleStatus,
			&item.Currency,
			&item.PriceCents,
			&item.DurationSeconds,
			&item.ValidityDays,
			&item.ActivationMode,
			&item.ActivationDeadlineDays,
			&item.ParticipatesReferral,
			&item.ParticipatesSalesCommission,
			&item.ParticipatesAgentSettlement,
			&item.EffectiveFrom,
			&item.EffectiveTo,
			&item.PublishedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateCommercialTimeCard(
	ctx context.Context,
	userID int64,
	input model.CommercialTimeCardInput,
) (model.CommercialTimeCardProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_time_card_products (code, name, description, status, sort_order)
		VALUES (?, ?, ?, 'draft', ?)
	`, input.Code, input.Name, input.Description, input.SortOrder)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	productID, err := result.LastInsertId()
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO catalog_time_card_versions (
			product_id, version_no, lifecycle_status, currency, price_cents,
			duration_seconds, validity_days, activation_mode, activation_deadline_days,
			participates_referral, participates_sales_commission, participates_agent_settlement,
			created_by_user_id
		)
		VALUES (?, 1, 'draft', 'CNY', ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		productID,
		input.PriceCents,
		input.DurationSeconds,
		input.ValidityDays,
		input.ActivationMode,
		input.ActivationDeadlineDays,
		input.ParticipatesReferral,
		input.ParticipatesSalesCommission,
		input.ParticipatesAgentSettlement,
		userID,
	)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	return s.GetCommercialTimeCard(ctx, productID)
}

func (s *Store) SaveCommercialTimeCardDraft(
	ctx context.Context,
	userID int64,
	productID int64,
	input model.CommercialTimeCardInput,
) (model.CommercialTimeCardProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_time_card_products
		WHERE id=?
		FOR UPDATE
	`, productID).Scan(&existingID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE catalog_time_card_products
		SET code=?, name=?, description=?, sort_order=?
		WHERE id=?
	`, input.Code, input.Name, input.Description, input.SortOrder, productID)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	var draftID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_time_card_versions
		WHERE product_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
	`, productID).Scan(&draftID)

	if err == nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE catalog_time_card_versions
			SET price_cents=?, duration_seconds=?, validity_days=?,
			    activation_mode=?, activation_deadline_days=?,
			    participates_referral=?, participates_sales_commission=?,
			    participates_agent_settlement=?, created_by_user_id=?
			WHERE id=?
		`,
			input.PriceCents,
			input.DurationSeconds,
			input.ValidityDays,
			input.ActivationMode,
			input.ActivationDeadlineDays,
			input.ParticipatesReferral,
			input.ParticipatesSalesCommission,
			input.ParticipatesAgentSettlement,
			userID,
			draftID,
		)
		if err != nil {
			return model.CommercialTimeCardProduct{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var nextVersion uint32
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version_no), 0) + 1
			FROM catalog_time_card_versions
			WHERE product_id=?
		`, productID).Scan(&nextVersion); err != nil {
			return model.CommercialTimeCardProduct{}, err
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO catalog_time_card_versions (
				product_id, version_no, lifecycle_status, currency, price_cents,
				duration_seconds, validity_days, activation_mode, activation_deadline_days,
				participates_referral, participates_sales_commission, participates_agent_settlement,
				created_by_user_id
			)
			VALUES (?, ?, 'draft', 'CNY', ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			productID,
			nextVersion,
			input.PriceCents,
			input.DurationSeconds,
			input.ValidityDays,
			input.ActivationMode,
			input.ActivationDeadlineDays,
			input.ParticipatesReferral,
			input.ParticipatesSalesCommission,
			input.ParticipatesAgentSettlement,
			userID,
		)
		if err != nil {
			return model.CommercialTimeCardProduct{}, err
		}
	} else {
		return model.CommercialTimeCardProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	return s.GetCommercialTimeCard(ctx, productID)
}

func (s *Store) PublishCommercialTimeCard(
	ctx context.Context,
	userID int64,
	productID int64,
) (model.CommercialTimeCardProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	defer tx.Rollback()

	var draftID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_time_card_versions
		WHERE product_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
	`, productID).Scan(&draftID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_time_card_versions
		SET lifecycle_status='retired', effective_to=COALESCE(effective_to, CURRENT_TIMESTAMP(3))
		WHERE product_id=? AND lifecycle_status='published'
	`, productID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_time_card_versions
		SET lifecycle_status='published', effective_from=?,
		    effective_to=NULL, published_by_user_id=?, published_at=?
		WHERE id=?
	`, now, userID, now, draftID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_time_card_products
		SET status='active'
		WHERE id=?
	`, productID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	return s.GetCommercialTimeCard(ctx, productID)
}

func (s *Store) SetCommercialTimeCardStatus(
	ctx context.Context,
	actorUserID int64,
	productID int64,
	status string,
) (model.CommercialTimeCardProduct, error) {
	if status != "active" && status != "inactive" && status != "archived" {
		return model.CommercialTimeCardProduct{}, fmt.Errorf("invalid time card status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	defer tx.Rollback()

	var name, beforeStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT name, status FROM catalog_time_card_products WHERE id=? FOR UPDATE
	`, productID).Scan(&name, &beforeStatus); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	if status == "active" {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM catalog_time_card_versions
			WHERE product_id=? AND lifecycle_status='published'
		`, productID).Scan(&count); err != nil {
			return model.CommercialTimeCardProduct{}, err
		}
		if count == 0 {
			return model.CommercialTimeCardProduct{}, fmt.Errorf("time card has no published version")
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_time_card_products SET status=? WHERE id=?`, status, productID); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	if err := insertCommercialAuditTx(
		ctx, tx, actorUserID, "time_card.product.status", "time_card_product", fmt.Sprintf("%d", productID),
		map[string]any{"name": name, "status": beforeStatus},
		map[string]any{"name": name, "status": status},
	); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CommercialTimeCardProduct{}, err
	}
	return s.GetCommercialTimeCard(ctx, productID)
}

func (s *Store) CustomerTimeCardDiscountBPS(ctx context.Context, tenantID int64) (uint32, error) {
	var discount uint32
	err := s.db.QueryRowContext(ctx, `
		SELECT v.default_time_card_discount_bps
		FROM biz_memberships m
		INNER JOIN catalog_membership_plan_versions v ON v.id=m.plan_version_id
		WHERE m.tenant_id=? AND m.status='active'
		  AND m.cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY m.id DESC
		LIMIT 1
	`, tenantID).Scan(&discount)
	if errors.Is(err, sql.ErrNoRows) {
		return 10000, nil
	}
	if err != nil {
		return 0, err
	}
	if discount == 0 || discount > 10000 {
		return 10000, nil
	}
	return discount, nil
}

func (s *Store) EnsureDefaultTimeCards(ctx context.Context) error {
	type seed struct {
		code      string
		name      string
		desc      string
		price     uint64
		duration  uint64
		validDays uint32
		sort      int
	}
	seeds := []seed{
		{"tc_10h", "10 小时时长卡", "适合短时测试和临时补充算力。", 3900, 10 * 3600, 365, 10},
		{"tc_50h", "50 小时时长卡", "适合中小直播间阶段性使用。", 16900, 50 * 3600, 365, 20},
		{"tc_100h", "100 小时时长卡", "适合常规直播运营和持续补充时长。", 29900, 100 * 3600, 365, 30},
		{"tc_500h", "500 小时时长卡", "适合高频直播和较长周期使用。", 139900, 500 * 3600, 365, 40},
	}

	for _, item := range seeds {
		result, err := s.db.ExecContext(ctx, `
			INSERT IGNORE INTO catalog_time_card_products (code, name, description, status, sort_order)
			VALUES (?, ?, ?, 'active', ?)
		`, item.code, item.name, item.desc, item.sort)
		if err != nil {
			return fmt.Errorf("seed time card product: %w", err)
		}

		var productID int64
		if id, err := result.LastInsertId(); err == nil && id > 0 {
			productID = id
		} else if err := s.db.QueryRowContext(ctx,
			"SELECT id FROM catalog_time_card_products WHERE code=?",
			item.code,
		).Scan(&productID); err != nil {
			return err
		}

		if _, err := s.db.ExecContext(ctx, `
			INSERT IGNORE INTO catalog_time_card_versions (
				product_id, version_no, lifecycle_status, currency, price_cents,
				duration_seconds, validity_days, participates_referral,
				participates_sales_commission, participates_agent_settlement,
				effective_from, published_at
			)
			VALUES (?, 1, 'published', 'CNY', ?, ?, ?, 1, 1, 1, CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3))
		`, productID, item.price, item.duration, item.validDays); err != nil {
			return fmt.Errorf("seed time card version: %w", err)
		}
	}
	return nil
}
