package db

import (
	"context"
	"database/sql"
	"errors"

	"livecompanion/management/internal/model"
)

var ErrMarketingPresentationAssetInUse = errors.New("marketing presentation asset in use")

func (s *Store) CreateMarketingPresentationAsset(
	ctx context.Context,
	item model.MarketingPresentationAsset,
) (model.MarketingPresentationAsset, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO mkt_campaign_skin_assets (
			storage_driver, storage_bucket, object_key, original_name,
			mime_type, size_bytes, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, item.StorageDriver, item.StorageBucket, item.ObjectKey, item.OriginalName,
		item.MIMEType, item.SizeBytes, item.CreatedByUserID)
	if err != nil {
		return model.MarketingPresentationAsset{}, err
	}
	item.ID, err = result.LastInsertId()
	if err != nil {
		return model.MarketingPresentationAsset{}, err
	}
	return s.GetMarketingPresentationAsset(ctx, item.ID)
}

func (s *Store) GetMarketingPresentationAsset(
	ctx context.Context,
	assetID int64,
) (model.MarketingPresentationAsset, error) {
	var item model.MarketingPresentationAsset
	err := s.db.QueryRowContext(ctx, `
		SELECT id, storage_driver, storage_bucket, object_key, original_name,
		       mime_type, size_bytes, created_by_user_id, created_at
		FROM mkt_campaign_skin_assets
		WHERE id=?
	`, assetID).Scan(
		&item.ID, &item.StorageDriver, &item.StorageBucket, &item.ObjectKey,
		&item.OriginalName, &item.MIMEType, &item.SizeBytes,
		&item.CreatedByUserID, &item.CreatedAt,
	)
	if err != nil {
		return model.MarketingPresentationAsset{}, err
	}
	return item, nil
}

func (s *Store) DeleteMarketingPresentationAsset(ctx context.Context, assetID int64) error {
	var references uint64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM mkt_campaign_presentations
		WHERE JSON_UNQUOTE(JSON_EXTRACT(presentation_json, '$.cover_asset_id')) = ?
	`, assetID).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return ErrMarketingPresentationAssetInUse
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM mkt_campaign_skin_assets WHERE id=?`, assetID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
