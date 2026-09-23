package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"livecompanion/management/internal/model"
)

func (s *Store) ListCommercialDeviceProducts(ctx context.Context) ([]model.CommercialDeviceProduct, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.sku_code, p.name, p.description,
		       p.status, p.sort_order, p.created_at, p.updated_at,
		       COALESCE((
		         SELECT COUNT(*)
		         FROM inv_devices d
		         WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
		       ), 0)
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
			&item.Status,
			&item.SortOrder,
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
		       list_price_cents, sale_price_cents,
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

	result, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_device_products (
			code, sku_code, name, description, status, sort_order
		)
		VALUES (?, ?, ?, ?, 'draft', ?)
	`, input.Code, input.SKUCode, input.Name, input.Description, input.SortOrder)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	productID, err := result.LastInsertId()
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO catalog_device_versions (
			product_id, version_no, lifecycle_status, currency,
			list_price_cents, sale_price_cents,
			participates_referral, participates_sales_commission,
			participates_agent_settlement, created_by_user_id
		)
		VALUES (?, 1, 'draft', 'CNY', ?, ?, ?, ?, ?, ?)
	`,
		productID,
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

	result, err := tx.ExecContext(ctx, `
		UPDATE catalog_device_products
		SET code=?, sku_code=?, name=?, description=?, sort_order=?
		WHERE id=?
	`, input.Code, input.SKUCode, input.Name, input.Description, input.SortOrder, productID)
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.CommercialDeviceProduct{}, err
	}
	if affected == 0 {
		return model.CommercialDeviceProduct{}, sql.ErrNoRows
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
			SET list_price_cents=?, sale_price_cents=?,
			    participates_referral=?, participates_sales_commission=?,
			    participates_agent_settlement=?, created_by_user_id=?
			WHERE id=?
		`,
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
				list_price_cents, sale_price_cents,
				participates_referral, participates_sales_commission,
				participates_agent_settlement, created_by_user_id
			)
			VALUES (?, ?, 'draft', 'CNY', ?, ?, ?, ?, ?, ?)
		`,
			productID,
			nextVersion,
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

func (s *Store) ListCustomerDeviceOffers(ctx context.Context) ([]model.CustomerDeviceOffer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.code, p.sku_code, p.name, p.description,
		       v.list_price_cents, v.sale_price_cents, v.version_no,
		       COALESCE((
		         SELECT COUNT(*)
		         FROM inv_devices d
		         WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
		       ), 0)
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
			&item.OriginalPriceCents,
			&item.SalePriceCents,
			&item.VersionNo,
			&item.AvailableStock,
		); err != nil {
			return nil, err
		}
		item.DiscountBPS = 10000
		if item.OriginalPriceCents > 0 {
			item.DiscountBPS = uint32(item.SalePriceCents * 10000 / item.OriginalPriceCents)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
