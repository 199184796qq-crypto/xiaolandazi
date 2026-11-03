package db

import (
	"context"
	"livecompanion/management/internal/model"
)

// FindScriptParagraphAssets is a bounded recovery lookup. The paragraph key
// includes the complete synthesis identity; callers still validate the object.
func (s *Store) FindScriptParagraphAssets(ctx context.Context, tenant, plan int64, key string) ([]model.MediaAsset, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, tenant_id, agent_id, asset_type, original_name,
 storage_driver, storage_bucket, object_key, mime_type, size_bytes, duration_ms, checksum_sha256,
 status, CAST(metadata_json AS CHAR), created_by_user_id, created_at, updated_at
 FROM media_assets WHERE tenant_id=? AND asset_type='generated_voice' AND status='active'
 AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.plan_id'))=CAST(? AS CHAR)
 AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.paragraph_key'))=? ORDER BY id DESC LIMIT 5`, tenant, plan, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []model.MediaAsset
	for rows.Next() {
		item, err := scanMediaAsset(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
