package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveOpeningHookNotFound = errors.New("live opening hook not found")

func (s *Store) GetLiveOpeningHook(ctx context.Context, tenant, plan int64) (model.LiveOpeningHookAsset, error) {
	var asset model.LiveOpeningHookAsset
	err := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, plan_id, library_md, prompt_source, created_by_user_id, created_at FROM live_opening_hook_assets WHERE tenant_id=? AND plan_id=? ORDER BY id DESC LIMIT 1`, tenant, plan).Scan(&asset.ID, &asset.TenantID, &asset.PlanID, &asset.LibraryMD, &asset.Source, &asset.CreatedByUserID, &asset.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrLiveOpeningHookNotFound
	}
	return asset, err
}

func (s *Store) SaveLiveOpeningHook(ctx context.Context, tenant, plan, actor int64, md, source string) (model.LiveOpeningHookAsset, error) {
	var asset model.LiveOpeningHookAsset
	if tenant <= 0 || plan <= 0 || actor <= 0 || len(md) < 100 || len(md) > 256*1024 || len(source) > 1000 {
		return asset, errors.New("开场钩子方案资产参数无效")
	}
	// Never overwrite another version or write back to a mother file.
	result, err := s.db.ExecContext(ctx, `INSERT INTO live_opening_hook_assets (tenant_id, plan_id, library_md, prompt_source, created_by_user_id) VALUES (?, ?, ?, ?, ?)`, tenant, plan, strings.TrimSpace(md), source, actor)
	if err != nil {
		return asset, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return asset, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT id, tenant_id, plan_id, library_md, prompt_source, created_by_user_id, created_at FROM live_opening_hook_assets WHERE id=? AND tenant_id=? AND plan_id=?`, id, tenant, plan).Scan(&asset.ID, &asset.TenantID, &asset.PlanID, &asset.LibraryMD, &asset.Source, &asset.CreatedByUserID, &asset.CreatedAt)
	return asset, err
}
