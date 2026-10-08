package db

import (
	"context"
	"database/sql"
	"errors"
	"livecompanion/management/internal/model"
)

func (s *Store) ListLiveFactCategories(ctx context.Context, tenant int64) ([]model.LiveFactCategory, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT category_value,label,description,family FROM live_fact_categories WHERE tenant_id=? ORDER BY label,category_value`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.LiveFactCategory{}
	for rows.Next() {
		var item model.LiveFactCategory
		item.Group = "自定义分类"
		if err = rows.Scan(&item.Value, &item.Label, &item.Description, &item.Family); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func insertLiveFactCategory(ctx context.Context, tx *sql.Tx, tenant, actor int64, c model.LiveFactCategory) error {
	if tenant <= 0 || actor <= 0 {
		return errors.New("分类账号范围无效")
	}
	normalized, err := model.NewLiveFactCategory(c.Label, c.Family, c.Description)
	if err != nil {
		return err
	}
	if normalized.Value != c.Value {
		return errors.New("分类标识无效")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO live_fact_categories(tenant_id,category_value,label,description,family,created_by_user_id) VALUES(?,?,?,?,?,?) ON DUPLICATE KEY UPDATE category_value=category_value`, tenant, c.Value, c.Label, c.Description, c.Family, actor)
	if err != nil {
		return err
	}
	var label, description, family string
	if err = tx.QueryRowContext(ctx, `SELECT label,description,family FROM live_fact_categories WHERE tenant_id=? AND category_value=? FOR UPDATE`, tenant, c.Value).Scan(&label, &description, &family); err != nil {
		return err
	}
	if label != c.Label || family != c.Family {
		return errors.New("同名分类已存在但所属类型不同，请使用已有分类")
	}
	return nil
}

func (s *Store) CreateLiveFactCategory(ctx context.Context, tenant, actor int64, c model.LiveFactCategory) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = insertLiveFactCategory(ctx, tx, tenant, actor, c); err != nil {
		return err
	}
	return tx.Commit()
}
