package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/model"
)

var ErrInvalidDeviceSalesStock = errors.New("invalid device sales stock")

func (s *Store) ListCommercialDeviceProducts(ctx context.Context) ([]model.CommercialDeviceProduct, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.sku_code, p.name, p.description, p.image_url,
		       p.unit_code,
		       COALESCE((
		         SELECT d.label
		         FROM mgmt_system_dictionary_items d
		         WHERE d.category='product_unit' AND d.code=p.unit_code
		         LIMIT 1
		       ), p.unit_code),
		       p.status, p.sort_order, p.sales_stock,
		       COALESCE((
		         SELECT COUNT(*)
		         FROM inv_devices d
		         WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
		       ), 0),
		       p.created_at, p.updated_at,
		       LEAST(
		         p.sales_stock,
		         COALESCE((
		           SELECT COUNT(*)
		           FROM inv_devices d
		           WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
		         ), 0)
		       )
		FROM catalog_device_products p
		ORDER BY p.sort_order ASC, p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialDeviceProduct, 0)
	for rows.Next() {
		var item model.CommercialDeviceProduct
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.SKUCode,
			&item.Name,
			&item.Description,
			&item.ImageURL,
			&item.UnitCode,
			&item.UnitLabel,
			&item.Status,
			&item.SortOrder,
			&item.SalesStock,
			&item.RealStock,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.AvailableStock,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range items {
		versions, err := s.listDeviceProductVersions(ctx, items[i].ID)
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
	campaigns, err := s.ListMarketingCampaigns(ctx, "device_product", 0, false)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].MarketingCampaigns = make([]model.MarketingCampaign, 0)
		for _, campaign := range campaigns {
			if marketingCampaignHasTarget(campaign, "device_product", items[i].ID) {
				items[i].MarketingCampaigns = append(items[i].MarketingCampaigns, campaign)
			}
		}
	}
	return items, nil
}

func (s *Store) GetCommercialDeviceProduct(ctx context.Context, productID int64) (model.CommercialDeviceProduct, error) {
	items, err := s.ListCommercialDeviceProducts(ctx)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	for _, item := range items {
		if item.ID == productID {
			return item, nil
		}
	}
	return model.CommercialDeviceProduct{}, sql.ErrNoRows
}

func (s *Store) listDeviceProductVersions(ctx context.Context, productID int64) ([]model.CommercialDeviceVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, product_id, version_no, lifecycle_status, currency,
		       cost_price_cents, list_price_cents, sale_price_cents,
		       participates_referral, participates_sales_commission,
		       participates_agent_settlement, effective_from, effective_to,
		       published_at, created_at
		FROM catalog_device_versions
		WHERE product_id=?
		ORDER BY version_no ASC
	`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CommercialDeviceVersion, 0)
	for rows.Next() {
		var item model.CommercialDeviceVersion
		if err := rows.Scan(
			&item.ID,
			&item.ProductID,
			&item.VersionNo,
			&item.LifecycleStatus,
			&item.Currency,
			&item.CostPriceCents,
			&item.ListPriceCents,
			&item.SalePriceCents,
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

func ensureInventorySKUExistsTx(ctx context.Context, tx *sql.Tx, skuCode string) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM inv_devices
		WHERE sku_code=?
		LIMIT 1
	`, skuCode).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("inventory sku not found")
		}
		return err
	}
	return nil
}

func inventorySKUInStockCountTx(ctx context.Context, tx *sql.Tx, skuCode string) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM inv_devices
		WHERE sku_code=? AND lifecycle_status='IN_STOCK'
	`, skuCode).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func validateDeviceSalesStockTx(ctx context.Context, tx *sql.Tx, skuCode string, salesStock int) error {
	if salesStock < 0 {
		return ErrInvalidDeviceSalesStock
	}
	realStock, err := inventorySKUInStockCountTx(ctx, tx, skuCode)
	if err != nil {
		return err
	}
	if salesStock > realStock {
		return ErrInvalidDeviceSalesStock
	}
	return nil
}

func (s *Store) CreateCommercialDeviceProduct(
	ctx context.Context,
	userID int64,
	input model.CommercialDeviceInput,
) (model.CommercialDeviceProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	defer tx.Rollback()

	if err := ensureInventorySKUExistsTx(ctx, tx, input.SKUCode); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if err := validateDeviceSalesStockTx(ctx, tx, input.SKUCode, input.SalesStock); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	temporaryCode := fmt.Sprintf("tmp-%d-%d", userID, time.Now().UTC().UnixNano())
	result, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_device_products (
			code, sku_code, name, description, image_url, unit_code, sales_stock, status, sort_order
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'draft', ?)
	`, temporaryCode, input.SKUCode, input.Name, input.Description, input.ImageURL, input.UnitCode, input.SalesStock, input.SortOrder)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	productID, err := result.LastInsertId()
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	fixedCode := fmt.Sprintf("dev-%06d", productID)
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET code=?
		WHERE id=?
	`, fixedCode, productID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_device_versions (
			product_id, version_no, lifecycle_status, currency,
			cost_price_cents, list_price_cents, sale_price_cents,
			participates_referral, participates_sales_commission,
			participates_agent_settlement, created_by_user_id
		)
		VALUES (?, 1, 'draft', 'CNY', ?, ?, ?, ?, ?, ?, ?)
	`,
		productID,
		input.CostPriceCents,
		input.ListPriceCents,
		input.SalePriceCents,
		input.ParticipatesReferral,
		input.ParticipatesSalesCommission,
		input.ParticipatesAgentSettlement,
		userID,
	); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	return s.GetCommercialDeviceProduct(ctx, productID)
}

func (s *Store) SaveCommercialDeviceProductDraft(
	ctx context.Context,
	userID int64,
	productID int64,
	input model.CommercialDeviceInput,
) (model.CommercialDeviceProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	defer tx.Rollback()

	if err := ensureInventorySKUExistsTx(ctx, tx, input.SKUCode); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if err := validateDeviceSalesStockTx(ctx, tx, input.SKUCode, input.SalesStock); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	var existingID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_device_products
		WHERE id=?
		FOR UPDATE
	`, productID).Scan(&existingID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET sku_code=?, name=?, description=?, image_url=?, unit_code=?, sales_stock=?, sort_order=?
		WHERE id=?
	`, input.SKUCode, input.Name, input.Description, input.ImageURL, input.UnitCode, input.SalesStock, input.SortOrder, productID)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	var draftID int64
	err = tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_device_versions
		WHERE product_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
	`, productID).Scan(&draftID)

	if err == nil {
		_, err = tx.ExecContext(ctx, `
			UPDATE catalog_device_versions
			SET cost_price_cents=?, list_price_cents=?, sale_price_cents=?,
			    participates_referral=?, participates_sales_commission=?,
			    participates_agent_settlement=?, created_by_user_id=?
			WHERE id=?
		`,
			input.CostPriceCents,
			input.ListPriceCents,
			input.SalePriceCents,
			input.ParticipatesReferral,
			input.ParticipatesSalesCommission,
			input.ParticipatesAgentSettlement,
			userID,
			draftID,
		)
		if err != nil {
			return model.CommercialDeviceProduct{}, err
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		var nextVersion uint32
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(version_no), 0) + 1
			FROM catalog_device_versions
			WHERE product_id=?
		`, productID).Scan(&nextVersion); err != nil {
			return model.CommercialDeviceProduct{}, err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO catalog_device_versions (
				product_id, version_no, lifecycle_status, currency,
				cost_price_cents, list_price_cents, sale_price_cents,
				participates_referral, participates_sales_commission,
				participates_agent_settlement, created_by_user_id
			)
			VALUES (?, ?, 'draft', 'CNY', ?, ?, ?, ?, ?, ?, ?)
		`,
			productID,
			nextVersion,
			input.CostPriceCents,
			input.ListPriceCents,
			input.SalePriceCents,
			input.ParticipatesReferral,
			input.ParticipatesSalesCommission,
			input.ParticipatesAgentSettlement,
			userID,
		); err != nil {
			return model.CommercialDeviceProduct{}, err
		}
	} else {
		return model.CommercialDeviceProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	return s.GetCommercialDeviceProduct(ctx, productID)
}

func (s *Store) PublishCommercialDeviceProduct(
	ctx context.Context,
	userID int64,
	productID int64,
) (model.CommercialDeviceProduct, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	defer tx.Rollback()

	var draftID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM catalog_device_versions
		WHERE product_id=? AND lifecycle_status='draft'
		ORDER BY version_no DESC
		LIMIT 1
	`, productID).Scan(&draftID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_versions
		SET lifecycle_status='retired',
		    effective_to=COALESCE(effective_to, CURRENT_TIMESTAMP(3))
		WHERE product_id=? AND lifecycle_status='published'
	`, productID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_versions
		SET lifecycle_status='published', effective_from=?,
		    effective_to=NULL, published_by_user_id=?, published_at=?
		WHERE id=?
	`, now, userID, now, draftID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET status='active'
		WHERE id=?
	`, productID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	return s.GetCommercialDeviceProduct(ctx, productID)
}

func (s *Store) SetCommercialDeviceProductStatus(
	ctx context.Context,
	actorUserID int64,
	productID int64,
	status string,
) (model.CommercialDeviceProduct, error) {
	if status != "active" && status != "inactive" && status != "archived" {
		return model.CommercialDeviceProduct{}, errors.New("invalid device product status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	defer tx.Rollback()

	var name, beforeStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT name, status FROM catalog_device_products WHERE id=? FOR UPDATE
	`, productID).Scan(&name, &beforeStatus); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if status == "active" {
		var count int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM catalog_device_versions
			WHERE product_id=? AND lifecycle_status='published'
		`, productID).Scan(&count); err != nil {
			return model.CommercialDeviceProduct{}, err
		}
		if count == 0 {
			return model.CommercialDeviceProduct{}, errors.New("device product has no published version")
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE catalog_device_products SET status=? WHERE id=?`, status, productID); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if err := insertCommercialAuditTx(
		ctx, tx, actorUserID, "device.product.status", "device_product", fmt.Sprintf("%d", productID),
		map[string]any{"name": name, "status": beforeStatus},
		map[string]any{"name": name, "status": status},
	); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	return s.GetCommercialDeviceProduct(ctx, productID)
}

func (s *Store) ListCustomerDeviceOffers(ctx context.Context, tenantID int64) ([]model.CustomerDeviceOffer, error) {
	membershipDiscountBPS, err := s.CustomerDeviceDiscountBPS(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.sku_code, p.name, p.description, p.image_url,
		       p.unit_code,
		       COALESCE((
		         SELECT d.label
		         FROM mgmt_system_dictionary_items d
		         WHERE d.category='product_unit' AND d.code=p.unit_code
		         LIMIT 1
		       ), p.unit_code),
		       v.list_price_cents, v.sale_price_cents, v.version_no,
		       LEAST(
		         p.sales_stock,
		         COALESCE((
		           SELECT COUNT(*)
		           FROM inv_devices d
		           WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
		         ), 0)
		       )
		FROM catalog_device_products p
		INNER JOIN catalog_device_versions v ON v.product_id=p.id
		WHERE p.status='active'
		  AND v.lifecycle_status='published'
		  AND (v.effective_from IS NULL OR v.effective_from <= CURRENT_TIMESTAMP(3))
		  AND (v.effective_to IS NULL OR v.effective_to > CURRENT_TIMESTAMP(3))
		ORDER BY p.sort_order ASC, p.id ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.CustomerDeviceOffer, 0)
	for rows.Next() {
		var item model.CustomerDeviceOffer
		if err := rows.Scan(
			&item.ID,
			&item.Code,
			&item.SKUCode,
			&item.Name,
			&item.Description,
			&item.ImageURL,
			&item.UnitCode,
			&item.UnitLabel,
			&item.OriginalPriceCents,
			&item.BaseSalePriceCents,
			&item.VersionNo,
			&item.AvailableStock,
		); err != nil {
			return nil, err
		}
		item.MembershipDiscountBPS = membershipDiscountBPS
		item.SalePriceCents = item.BaseSalePriceCents * uint64(membershipDiscountBPS) / 10000
		item.DiscountBPS = 10000
		if item.OriginalPriceCents > 0 {
			item.DiscountBPS = uint32(item.SalePriceCents * 10000 / item.OriginalPriceCents)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CustomerDeviceDiscountBPS(ctx context.Context, tenantID int64) (uint32, error) {
	var discount uint32
	err := s.db.QueryRowContext(ctx, `
		SELECT v.default_device_discount_bps
		FROM biz_memberships m
		INNER JOIN catalog_membership_plan_versions v ON v.id=m.plan_version_id
		WHERE m.tenant_id=? AND m.status='active'
		  AND m.cycle_start_at <= CURRENT_TIMESTAMP(3)
		  AND m.cycle_end_at > CURRENT_TIMESTAMP(3)
		ORDER BY m.cycle_end_at DESC, m.id DESC
		LIMIT 1
	`, tenantID).Scan(&discount)
	if errors.Is(err, sql.ErrNoRows) {
		return 10000, nil
	}
	if err != nil {
		return 0, err
	}
	return normalizeMembershipDiscountBPS(discount), nil
}
