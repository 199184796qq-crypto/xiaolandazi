package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrIndustryConfigPackNotFound = errors.New("industry config pack not found")

const validIndustryBizDomain = "ecommerce"
const validIndustryBizDomainLocal = "local_service"

// ValidIndustryBizDomain 校验业务域取值。
func ValidIndustryBizDomain(domain string) bool {
	return domain == validIndustryBizDomain || domain == validIndustryBizDomainLocal
}

// UpsertIndustryConfigPack 按 (biz_domain, industry_code) 唯一键写入/更新行业配置包。
func (s *Store) UpsertIndustryConfigPack(ctx context.Context, pack model.IndustryConfigPack) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_industry_config_packs
			(biz_domain, industry_code, industry_name, selling_dimensions_md, sides_json, p0_extra_md, version, status)
		VALUES (?,?,?,?,?,?,?,?)
		ON DUPLICATE KEY UPDATE
			industry_name=VALUES(industry_name),
			selling_dimensions_md=VALUES(selling_dimensions_md),
			sides_json=VALUES(sides_json),
			p0_extra_md=VALUES(p0_extra_md),
			version=VALUES(version),
			status=VALUES(status)
	`, pack.BizDomain, pack.IndustryCode, pack.IndustryName, pack.SellingDimensionsMD, pack.SidesJSON, pack.P0ExtraMD, pack.Version, pack.Status)
	return err
}

// GetIndustryConfigPack 按 (biz_domain, industry_code) 取行业配置包，仅返回 active 记录。
func (s *Store) GetIndustryConfigPack(ctx context.Context, bizDomain, industryCode string) (*model.IndustryConfigPack, error) {
	var p model.IndustryConfigPack
	var p0 sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, biz_domain, industry_code, industry_name, selling_dimensions_md, sides_json, p0_extra_md, version, status, created_at, updated_at
		FROM live_industry_config_packs
		WHERE biz_domain=? AND industry_code=? AND status='active'
	`, bizDomain, industryCode).Scan(&p.ID, &p.BizDomain, &p.IndustryCode, &p.IndustryName, &p.SellingDimensionsMD, &p.SidesJSON, &p0, &p.Version, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIndustryConfigPackNotFound
	}
	if err != nil {
		return nil, err
	}
	p.P0ExtraMD = strings.TrimSpace(p0.String)
	return &p, nil
}

// ListIndustryConfigPacks 列出某业务域下的全部行业配置包（含 disabled，供管理界面）。
func (s *Store) ListIndustryConfigPacks(ctx context.Context, bizDomain string) ([]model.IndustryConfigPack, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, biz_domain, industry_code, industry_name, selling_dimensions_md, sides_json, p0_extra_md, version, status, created_at, updated_at
		FROM live_industry_config_packs
		WHERE biz_domain=?
		ORDER BY industry_code
	`, bizDomain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.IndustryConfigPack
	for rows.Next() {
		var p model.IndustryConfigPack
		var p0 sql.NullString
		if err := rows.Scan(&p.ID, &p.BizDomain, &p.IndustryCode, &p.IndustryName, &p.SellingDimensionsMD, &p.SidesJSON, &p0, &p.Version, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.P0ExtraMD = strings.TrimSpace(p0.String)
		out = append(out, p)
	}
	return out, rows.Err()
}
