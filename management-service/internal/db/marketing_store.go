package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrMarketingCampaignStockInsufficient = errors.New("marketing campaign stock insufficient")
var ErrMarketingCampaignPendingOrders = errors.New("marketing campaign has pending orders")
var ErrMarketingCampaignHistoryLocked = errors.New("marketing campaign has historical usage")

func campaignTimeValue(value string) (any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return parsed.UTC(), nil
}

func campaignTimeString(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339)
}

func normalizeMarketingCampaignInput(input model.MarketingCampaignInput) (model.MarketingCampaignInput, error) {
	var controlsErr error
	input.Controls, controlsErr = normalizeCampaignControls(input.Controls, strings.TrimSpace(input.StartsAt))
	if controlsErr != nil {
		return input, controlsErr
	}
	input.Code = strings.ToLower(strings.TrimSpace(input.Code))
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Status = strings.TrimSpace(input.Status)
	input.PricingRule = strings.TrimSpace(input.PricingRule)
	input.StartsAt = strings.TrimSpace(input.StartsAt)
	input.EndsAt = strings.TrimSpace(input.EndsAt)
	input.DisplayLocations = normalizeMarketingDisplayLocations(input.DisplayLocations)

	if input.PricingRule == "" {
		input.PricingRule = "floor_yuan"
	}
	if input.Status == "" {
		input.Status = "active"
	}
	if input.Code == "" || input.Name == "" {
		return model.MarketingCampaignInput{}, errors.New("invalid marketing campaign")
	}
	if len(input.Items) == 0 {
		return model.MarketingCampaignInput{}, errors.New("marketing campaign requires items")
	}
	seenTargets := map[string]bool{}
	for i := range input.Items {
		item, ok := normalizeMarketingCampaignItem(input.Items[i])
		if !ok {
			return model.MarketingCampaignInput{}, errors.New("invalid marketing campaign item")
		}
		key := fmt.Sprintf("%s:%d", item.TargetType, item.TargetID)
		if seenTargets[key] {
			return input, errors.New("同一活动不能重复添加相同商品")
		}
		seenTargets[key] = true
		item.SortOrder = (i + 1) * 10
		input.Items[i] = item
	}
	startsAt, err := campaignTimeValue(input.StartsAt)
	if err != nil {
		return model.MarketingCampaignInput{}, fmt.Errorf("invalid campaign starts_at: %w", err)
	}
	endsAt, err := campaignTimeValue(input.EndsAt)
	if err != nil {
		return model.MarketingCampaignInput{}, fmt.Errorf("invalid campaign ends_at: %w", err)
	}
	if startsAt != nil && endsAt != nil {
		if !endsAt.(time.Time).After(startsAt.(time.Time)) {
			return model.MarketingCampaignInput{}, errors.New("campaign end must be after start")
		}
	}
	return input, nil
}

func insertMarketingCampaignPlacementsTx(
	ctx context.Context,
	tx *sql.Tx,
	campaignID int64,
	displayLocations []string,
) error {
	locations := normalizeMarketingDisplayLocations(displayLocations)
	for index, location := range locations {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaign_placements (
				campaign_id, placement_code, sort_order
			)
			VALUES (?, ?, ?)
		`, campaignID, location, (index+1)*10); err != nil {
			return err
		}
	}
	return nil
}

func insertMarketingCampaignItemsTx(
	ctx context.Context,
	tx *sql.Tx,
	campaignID int64,
	pricingRule string,
	items []model.MarketingCampaignItem,
) error {
	for index, rawItem := range items {
		item, ok := normalizeMarketingCampaignItem(rawItem)
		if !ok {
			return errors.New("invalid marketing campaign item")
		}
		sortOrder := item.SortOrder
		if sortOrder == 0 {
			sortOrder = (index + 1) * 10
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaign_items (
				campaign_id, target_type, target_id, quantity, sort_order
			)
			VALUES (?, ?, ?, ?, ?)
		`, campaignID, item.TargetType, item.TargetID, item.Quantity, sortOrder)
		if err != nil {
			return err
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mkt_campaign_item_options(campaign_item_id,fixed_price_cents) VALUES (?,?)`, itemID, item.FixedPriceCents); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaign_price_rules (
				campaign_id, campaign_item_id, pricing_mode,
				package_months, discount_bps, pricing_rule
			)
			VALUES (?, ?, ?, ?, ?, ?)
		`,
			campaignID,
			itemID,
			item.PricingMode,
			item.PackageMonths,
			item.DiscountBPS,
			pricingRule,
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaign_inventory (
				campaign_id, campaign_item_id, stock_limit,
				reserved_quantity, used_quantity
			)
			VALUES (?, ?, NULL, 0, 0)
		`, campaignID, itemID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) listNormalizedMarketingCampaigns(ctx context.Context) ([]model.MarketingCampaign, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, code, name, description, status, sort_order, pricing_rule,
		       starts_at, ends_at, created_by_user_id, updated_by_user_id,
		       created_at, updated_at
		FROM mkt_campaigns
		ORDER BY sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.MarketingCampaign, 0)
	indexByID := make(map[int64]int)
	for rows.Next() {
		var item model.MarketingCampaign
		var startsAt, endsAt sql.NullTime
		var createdBy, updatedBy sql.NullInt64
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.Name,
			&item.Description,
			&item.Status,
			&item.SortOrder,
			&item.PricingRule,
			&startsAt,
			&endsAt,
			&createdBy,
			&updatedBy,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		item.StartsAt = campaignTimeString(startsAt)
		item.EndsAt = campaignTimeString(endsAt)
		if createdBy.Valid {
			value := createdBy.Int64
			item.CreatedByUserID = &value
		}
		if updatedBy.Valid {
			value := updatedBy.Int64
			item.UpdatedByUserID = &value
		}
		item.Items = make([]model.MarketingCampaignItem, 0)
		indexByID[item.ID] = len(items)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}

	placementRows, err := s.db.QueryContext(ctx, `
		SELECT campaign_id, placement_code
		FROM mkt_campaign_placements
		ORDER BY campaign_id ASC, sort_order ASC, id ASC
	`)
	if err != nil {
		return nil, err
	}
	for placementRows.Next() {
		var campaignID int64
		var placementCode string
		if err := placementRows.Scan(&campaignID, &placementCode); err != nil {
			placementRows.Close()
			return nil, err
		}
		index, ok := indexByID[campaignID]
		if !ok {
			continue
		}
		items[index].DisplayLocations = append(items[index].DisplayLocations, placementCode)
	}
	if err := placementRows.Err(); err != nil {
		placementRows.Close()
		return nil, err
	}
	placementRows.Close()
	for i := range items {
		items[i].DisplayLocations = normalizeMarketingDisplayLocations(items[i].DisplayLocations)
	}

	itemRows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.campaign_id, i.target_type, i.target_id,
		       i.quantity, i.sort_order,
		       COALESCE(r.pricing_mode, 'discount'),
		       COALESCE(r.package_months, 1),
		       COALESCE(r.discount_bps, 10000)
		FROM mkt_campaign_items i
		LEFT JOIN mkt_campaign_price_rules r ON r.campaign_item_id=i.id
		ORDER BY i.campaign_id ASC, i.sort_order ASC, i.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()

	for itemRows.Next() {
		var item model.MarketingCampaignItem
		if err := itemRows.Scan(
			&item.ID,
			&item.CampaignID,
			&item.TargetType,
			&item.TargetID,
			&item.Quantity,
			&item.SortOrder,
			&item.PricingMode,
			&item.PackageMonths,
			&item.DiscountBPS,
		); err != nil {
			return nil, err
		}
		index, ok := indexByID[item.CampaignID]
		if !ok {
			continue
		}
		items[index].Items = append(items[index].Items, item)
	}
	if err := itemRows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		if len(items[i].Items) == 0 {
			continue
		}
		first := items[i].Items[0]
		items[i].TargetType = first.TargetType
		items[i].TargetID = first.TargetID
		items[i].PricingMode = first.PricingMode
		items[i].PackageMonths = first.PackageMonths
		items[i].DiscountBPS = first.DiscountBPS
	}
	if err := s.loadCampaignControls(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) GetMarketingCampaign(ctx context.Context, campaignID int64) (model.MarketingCampaign, error) {
	items, err := s.listNormalizedMarketingCampaigns(ctx)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	for _, item := range items {
		if item.ID == campaignID {
			return item, nil
		}
	}
	return model.MarketingCampaign{}, sql.ErrNoRows
}

func (s *Store) CreateMarketingCampaign(
	ctx context.Context,
	userID int64,
	input model.MarketingCampaignInput,
) (model.MarketingCampaign, error) {
	input, err := normalizeMarketingCampaignInput(input)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	startsAt, _ := campaignTimeValue(input.StartsAt)
	endsAt, _ := campaignTimeValue(input.EndsAt)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO mkt_campaigns (
			code, name, description, status, sort_order, pricing_rule,
			starts_at, ends_at, created_by_user_id, updated_by_user_id
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		input.Code,
		input.Name,
		input.Description,
		input.Status,
		input.SortOrder,
		input.PricingRule,
		startsAt,
		endsAt,
		userID,
		userID,
	)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	campaignID, err := result.LastInsertId()
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	if err := saveCampaignControlsTx(ctx, tx, campaignID, input.Controls); err != nil {
		return model.MarketingCampaign{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mkt_campaign_scopes (campaign_id, scope_type)
		VALUES (?, 'all_customers')
	`, campaignID); err != nil {
		return model.MarketingCampaign{}, err
	}
	if err := insertMarketingCampaignPlacementsTx(
		ctx,
		tx,
		campaignID,
		input.DisplayLocations,
	); err != nil {
		return model.MarketingCampaign{}, err
	}
	if err := insertMarketingCampaignItemsTx(
		ctx,
		tx,
		campaignID,
		input.PricingRule,
		input.Items,
	); err != nil {
		return model.MarketingCampaign{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MarketingCampaign{}, err
	}
	return s.GetMarketingCampaign(ctx, campaignID)
}

func marketingCampaignItemsMatchTx(
	ctx context.Context,
	tx *sql.Tx,
	campaignID int64,
	inputItems []model.MarketingCampaignItem,
) (bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT i.target_type, i.target_id, i.quantity,
		       COALESCE(r.pricing_mode, 'discount'),
		       COALESCE(r.package_months, 1),
		       COALESCE(r.discount_bps, 10000)
		FROM mkt_campaign_items i
		LEFT JOIN mkt_campaign_price_rules r ON r.campaign_item_id=i.id
		WHERE i.campaign_id=?
		ORDER BY i.sort_order ASC, i.id ASC
		FOR UPDATE
	`, campaignID)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	current := make([]model.MarketingCampaignItem, 0)
	for rows.Next() {
		var item model.MarketingCampaignItem
		if err := rows.Scan(
			&item.TargetType,
			&item.TargetID,
			&item.Quantity,
			&item.PricingMode,
			&item.PackageMonths,
			&item.DiscountBPS,
		); err != nil {
			return false, err
		}
		current = append(current, item)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(current) != len(inputItems) {
		return false, nil
	}
	for i := range current {
		incoming, ok := normalizeMarketingCampaignItem(inputItems[i])
		if !ok {
			return false, nil
		}
		if current[i].TargetType != incoming.TargetType ||
			current[i].TargetID != incoming.TargetID ||
			current[i].Quantity != incoming.Quantity ||
			current[i].PricingMode != incoming.PricingMode ||
			current[i].PackageMonths != incoming.PackageMonths ||
			current[i].DiscountBPS != incoming.DiscountBPS {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) UpdateMarketingCampaign(
	ctx context.Context,
	campaignID int64,
	userID int64,
	input model.MarketingCampaignInput,
) (model.MarketingCampaign, error) {
	input, err := normalizeMarketingCampaignInput(input)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	startsAt, _ := campaignTimeValue(input.StartsAt)
	endsAt, _ := campaignTimeValue(input.EndsAt)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MarketingCampaign{}, err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM mkt_campaigns
		WHERE id=?
		FOR UPDATE
	`, campaignID).Scan(&existingID); err != nil {
		return model.MarketingCampaign{}, err
	}

	var pendingUsage int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mkt_campaign_usage
		WHERE campaign_id=? AND status='pending'
	`, campaignID).Scan(&pendingUsage); err != nil {
		return model.MarketingCampaign{}, err
	}
	if pendingUsage > 0 {
		return model.MarketingCampaign{}, ErrMarketingCampaignPendingOrders
	}

	var historicalUsage int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mkt_campaign_usage
		WHERE campaign_id=?
	`, campaignID).Scan(&historicalUsage); err != nil {
		return model.MarketingCampaign{}, err
	}
	preserveItems := historicalUsage > 0
	if preserveItems {
		var raw string
		var controls model.MarketingCampaignControls
		err := tx.QueryRowContext(ctx, `SELECT CAST(controls_json AS CHAR) FROM mkt_campaign_controls WHERE campaign_id=?`, campaignID).Scan(&raw)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return model.MarketingCampaign{}, err
		}
		if err == nil {
			if err = json.Unmarshal([]byte(raw), &controls); err != nil {
				return model.MarketingCampaign{}, err
			}
		}
		if controls.Audience == "" {
			controls.Audience = "all"
		}
		if controls != input.Controls {
			return model.MarketingCampaign{}, ErrMarketingCampaignHistoryLocked
		}
		for _, item := range input.Items {
			var fixed sql.NullInt64
			err = tx.QueryRowContext(ctx, `SELECT o.fixed_price_cents FROM mkt_campaign_items i LEFT JOIN mkt_campaign_item_options o ON o.campaign_item_id=i.id WHERE i.campaign_id=? AND i.target_type=? AND i.target_id=? ORDER BY i.id LIMIT 1`, campaignID, item.TargetType, item.TargetID).Scan(&fixed)
			if err != nil {
				return model.MarketingCampaign{}, err
			}
			if (item.FixedPriceCents == nil) != (!fixed.Valid) || (fixed.Valid && uint64(fixed.Int64) != *item.FixedPriceCents) {
				return model.MarketingCampaign{}, ErrMarketingCampaignHistoryLocked
			}
		}
	}
	if preserveItems {
		matches, err := marketingCampaignItemsMatchTx(ctx, tx, campaignID, input.Items)
		if err != nil {
			return model.MarketingCampaign{}, err
		}
		if !matches {
			return model.MarketingCampaign{}, ErrMarketingCampaignHistoryLocked
		}
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE mkt_campaigns
		SET code=?, name=?, description=?, status=?, sort_order=?,
		    pricing_rule=?, starts_at=?, ends_at=?, updated_by_user_id=?
		WHERE id=?
	`,
		input.Code,
		input.Name,
		input.Description,
		input.Status,
		input.SortOrder,
		input.PricingRule,
		startsAt,
		endsAt,
		userID,
		campaignID,
	); err != nil {
		return model.MarketingCampaign{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM mkt_campaign_placements
		WHERE campaign_id=?
	`, campaignID); err != nil {
		return model.MarketingCampaign{}, err
	}
	if err := insertMarketingCampaignPlacementsTx(
		ctx,
		tx,
		campaignID,
		input.DisplayLocations,
	); err != nil {
		return model.MarketingCampaign{}, err
	}
	if !preserveItems {
		if err := saveCampaignControlsTx(ctx, tx, campaignID, input.Controls); err != nil {
			return model.MarketingCampaign{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE o FROM mkt_campaign_item_options o JOIN mkt_campaign_items i ON i.id=o.campaign_item_id WHERE i.campaign_id=?`, campaignID); err != nil {
			return model.MarketingCampaign{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM mkt_campaign_items
			WHERE campaign_id=?
		`, campaignID); err != nil {
			return model.MarketingCampaign{}, err
		}
		if err := insertMarketingCampaignItemsTx(
			ctx,
			tx,
			campaignID,
			input.PricingRule,
			input.Items,
		); err != nil {
			return model.MarketingCampaign{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return model.MarketingCampaign{}, err
	}
	return s.GetMarketingCampaign(ctx, campaignID)
}

func (s *Store) DeleteMarketingCampaign(ctx context.Context, campaignID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM mkt_campaigns
		WHERE id=?
		FOR UPDATE
	`, campaignID).Scan(&existingID); err != nil {
		return err
	}

	var usageCount int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mkt_campaign_usage
		WHERE campaign_id=?
	`, campaignID).Scan(&usageCount); err != nil {
		return err
	}
	if usageCount > 0 {
		return ErrMarketingCampaignHistoryLocked
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM mkt_campaigns
		WHERE id=?
	`, campaignID); err != nil {
		return err
	}
	return tx.Commit()
}

func reserveMarketingCampaignOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	tenantID int64,
	orderID int64,
	campaign model.MarketingCampaign,
	item model.MarketingCampaignItem,
	listAmountCents uint64,
	payableAmountCents uint64,
) error {
	if err := reserveCampaignClaimTx(ctx, tx, tenantID, orderID, campaign, item); err != nil {
		return err
	}
	if campaign.ID <= 0 || item.ID <= 0 {
		return errors.New("invalid marketing campaign snapshot")
	}
	quantity := item.Quantity
	if quantity == 0 {
		quantity = 1
	}

	var (
		stockLimit       sql.NullInt64
		reservedQuantity uint64
		usedQuantity     uint64
	)
	if err := tx.QueryRowContext(ctx, `
		SELECT stock_limit, reserved_quantity, used_quantity
		FROM mkt_campaign_inventory
		WHERE campaign_item_id=? AND campaign_id=?
		FOR UPDATE
	`, item.ID, campaign.ID).Scan(
		&stockLimit,
		&reservedQuantity,
		&usedQuantity,
	); err != nil {
		return err
	}
	if stockLimit.Valid &&
		usedQuantity+reservedQuantity+uint64(quantity) > uint64(stockLimit.Int64) {
		return ErrMarketingCampaignStockInsufficient
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mkt_campaign_inventory
		SET reserved_quantity=reserved_quantity+?
		WHERE campaign_item_id=? AND campaign_id=?
	`, quantity, item.ID, campaign.ID); err != nil {
		return err
	}

	discountAmount := uint64(0)
	if listAmountCents > payableAmountCents {
		discountAmount = listAmountCents - payableAmountCents
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mkt_campaign_order_snapshots (
			order_id, campaign_id, campaign_item_id,
			campaign_code, campaign_name,
			target_type, target_id, pricing_mode,
			package_months, quantity, discount_bps,
			list_amount_cents, payable_amount_cents, discount_amount_cents
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		orderID,
		campaign.ID,
		item.ID,
		campaign.Code,
		campaign.Name,
		item.TargetType,
		item.TargetID,
		item.PricingMode,
		item.PackageMonths,
		quantity,
		item.DiscountBPS,
		listAmountCents,
		payableAmountCents,
		discountAmount,
	); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mkt_campaign_usage (
			campaign_id, campaign_item_id, tenant_id, order_id,
			quantity, discount_amount_cents, status
		)
		VALUES (?, ?, ?, ?, ?, ?, 'pending')
	`,
		campaign.ID,
		item.ID,
		tenantID,
		orderID,
		quantity,
		discountAmount,
	); err != nil {
		return err
	}
	return nil
}

func consumeMarketingCampaignOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	orderID int64,
) error {
	if _, err := tx.ExecContext(ctx, `UPDATE mkt_claim_reservations SET status='consumed' WHERE order_id=? AND status='pending'`, orderID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, campaign_id, campaign_item_id, quantity
		FROM mkt_campaign_usage
		WHERE order_id=? AND status='pending'
		ORDER BY id ASC
		FOR UPDATE
	`, orderID)
	if err != nil {
		return err
	}
	type usageRow struct {
		id             int64
		campaignID     int64
		campaignItemID sql.NullInt64
		quantity       uint64
	}
	usages := make([]usageRow, 0)
	for rows.Next() {
		var usage usageRow
		if err := rows.Scan(
			&usage.id,
			&usage.campaignID,
			&usage.campaignItemID,
			&usage.quantity,
		); err != nil {
			rows.Close()
			return err
		}
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, usage := range usages {
		if usage.campaignItemID.Valid {
			result, err := tx.ExecContext(ctx, `
				UPDATE mkt_campaign_inventory
				SET reserved_quantity=reserved_quantity-?,
				    used_quantity=used_quantity+?
				WHERE campaign_id=? AND campaign_item_id=?
				  AND reserved_quantity>=?
			`,
				usage.quantity,
				usage.quantity,
				usage.campaignID,
				usage.campaignItemID.Int64,
				usage.quantity,
			)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return errors.New("marketing campaign reservation mismatch")
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mkt_campaign_usage
			SET status='consumed'
			WHERE id=? AND status='pending'
		`, usage.id); err != nil {
			return err
		}
	}
	return nil
}

func releaseMarketingCampaignOrderTx(
	ctx context.Context,
	tx *sql.Tx,
	orderID int64,
	status string,
) error {
	if _, err := tx.ExecContext(ctx, `UPDATE mkt_claim_reservations SET status='released' WHERE order_id=? AND status='pending'`, orderID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, campaign_id, campaign_item_id, quantity
		FROM mkt_campaign_usage
		WHERE order_id=? AND status='pending'
		ORDER BY id ASC
		FOR UPDATE
	`, orderID)
	if err != nil {
		return err
	}
	type usageRow struct {
		id             int64
		campaignID     int64
		campaignItemID sql.NullInt64
		quantity       uint64
	}
	usages := make([]usageRow, 0)
	for rows.Next() {
		var usage usageRow
		if err := rows.Scan(
			&usage.id,
			&usage.campaignID,
			&usage.campaignItemID,
			&usage.quantity,
		); err != nil {
			rows.Close()
			return err
		}
		usages = append(usages, usage)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if strings.TrimSpace(status) == "" {
		status = "cancelled"
	}
	for _, usage := range usages {
		if usage.campaignItemID.Valid {
			result, err := tx.ExecContext(ctx, `
				UPDATE mkt_campaign_inventory
				SET reserved_quantity=reserved_quantity-?
				WHERE campaign_id=? AND campaign_item_id=?
				  AND reserved_quantity>=?
			`,
				usage.quantity,
				usage.campaignID,
				usage.campaignItemID.Int64,
				usage.quantity,
			)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return errors.New("marketing campaign reservation mismatch")
			}
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mkt_campaign_usage
			SET status=?
			WHERE id=? AND status='pending'
		`, status, usage.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) migrateFeatureMarketingCampaigns(ctx context.Context) error {
	records, err := s.ListFeatureRecords(ctx, "marketing-campaigns")
	if err != nil {
		return err
	}
	for _, record := range records {
		campaign, ok := parseMarketingCampaign(record)
		if !ok {
			continue
		}
		if campaign.PricingRule == "" {
			campaign.PricingRule = "floor_yuan"
		}

		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}

		var existingID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id
			FROM mkt_campaigns
			WHERE id=? OR code=?
			LIMIT 1
		`, campaign.ID, campaign.Code).Scan(&existingID)
		if err == nil {
			_ = tx.Rollback()
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return err
		}

		startsAt, err := campaignTimeValue(campaign.StartsAt)
		if err != nil {
			startsAt = nil
		}
		endsAt, err := campaignTimeValue(campaign.EndsAt)
		if err != nil {
			endsAt = nil
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaigns (
				id, code, name, description, status, sort_order, pricing_rule,
				starts_at, ends_at, created_by_user_id, updated_by_user_id,
				created_at, updated_at
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			campaign.ID,
			campaign.Code,
			campaign.Name,
			campaign.Description,
			campaign.Status,
			campaign.SortOrder,
			campaign.PricingRule,
			startsAt,
			endsAt,
			record.CreatedByUserID,
			record.UpdatedByUserID,
			record.CreatedAt,
			record.UpdatedAt,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mkt_campaign_scopes (campaign_id, scope_type)
			VALUES (?, 'all_customers')
		`, campaign.ID); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := insertMarketingCampaignItemsTx(
			ctx,
			tx,
			campaign.ID,
			campaign.PricingRule,
			campaign.Items,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
