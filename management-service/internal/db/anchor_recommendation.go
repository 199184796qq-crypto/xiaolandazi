package db

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"sort"
	"strings"
)

//go:embed anchor_recommendation_schema.sql
var anchorRecommendationSchema string
var ErrAnchorRecommendationConflict = errors.New("主播风格或推荐版本已变化，请刷新后重新生成简介")
var ErrAnchorRecommendationUnavailable = errors.New("系统推荐主播已下架或不存在")

func (s *Store) MigrateAnchorRecommendations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, anchorRecommendationSchema); err != nil {
		return err
	}
	columns, err := s.indexColumns(ctx, "live_anchor_styles", "uk_anchor_recommendation_import")
	if err != nil {
		return err
	}
	if len(columns) == 0 {
		_, err = s.db.ExecContext(ctx, `ALTER TABLE live_anchor_styles ADD UNIQUE KEY uk_anchor_recommendation_import (tenant_id,source_recommendation_id)`)
	}
	return err
}
func (s *Store) SaveLiveAnchorAvatarData(ctx context.Context, tenant, style, actor int64, data string) (model.LiveAnchorStyle, error) {
	if _, err := s.GetLiveAnchorStyle(ctx, tenant, style); err != nil {
		return model.LiveAnchorStyle{}, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE live_anchor_styles SET avatar_data=?,updated_by_user_id=?,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND status='active'`, data, actor, style, tenant); err != nil {
		return model.LiveAnchorStyle{}, err
	}
	return s.GetLiveAnchorStyle(ctx, tenant, style)
}

const recommendationSelect = `SELECT id,source_tenant_id,source_style_id,name,introduction,summary,snapshot_json,version_no,status,updated_at FROM system_anchor_recommendations`

func scanAnchorRecommendation(row interface{ Scan(...any) error }) (model.AnchorRecommendation, error) {
	var item model.AnchorRecommendation
	var raw string
	err := row.Scan(&item.ID, &item.SourceTenantID, &item.SourceStyleID, &item.Name, &item.Introduction, &item.Summary, &raw, &item.VersionNo, &item.Status, &item.UpdatedAt)
	if err != nil {
		return item, err
	}
	if err = json.Unmarshal([]byte(raw), &item.Snapshot); err != nil {
		return item, err
	}
	item.AvatarType = item.Snapshot.AvatarType
	item.AvatarURL = item.Snapshot.AvatarURL
	return item, nil
}
func (s *Store) ListAnchorRecommendations(ctx context.Context, admin bool) ([]model.AnchorRecommendation, error) {
	query := recommendationSelect
	if !admin {
		query += ` WHERE status='active'`
	}
	query += ` ORDER BY updated_at DESC,id DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.AnchorRecommendation{}
	for rows.Next() {
		item, err := scanAnchorRecommendation(rows)
		if err != nil {
			return nil, err
		}
		if !admin {
			item.SourceTenantID = 0
			item.SourceStyleID = 0
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func (s *Store) ListAnchorRecommendationSources(ctx context.Context, search string) ([]model.AnchorRecommendationSource, error) {
	pattern := "%" + strings.TrimSpace(search) + "%"
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.tenant_id,t.name,s.name,s.description,s.avatar_type,COALESCE(s.avatar_data,''),s.profile_json FROM live_anchor_styles s JOIN tenants t ON t.id=s.tenant_id WHERE s.status='active' AND (s.name LIKE ? OR t.name LIKE ?) ORDER BY s.updated_at DESC,s.id DESC LIMIT 100`, pattern, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []model.AnchorRecommendationSource{}
	for rows.Next() {
		var item model.AnchorRecommendationSource
		var profile string
		if err = rows.Scan(&item.ID, &item.TenantID, &item.TenantName, &item.Name, &item.Description, &item.AvatarType, &item.AvatarURL, &profile); err != nil {
			return nil, err
		}
		item.HasStyle = model.HasReusableAnchorProfile(decodeLiveAnchorStyleProfile(profile))
		items = append(items, item)
	}
	return items, rows.Err()
}

// Compare against the locked source and plugins, so a model draft cannot publish
// a different user's later edits. The published snapshot has its own version.
func (s *Store) PublishAnchorRecommendation(ctx context.Context, source model.LiveAnchorStyle, actor int64, name, introduction, summary, expectedSHA string, generation map[string]any) (model.AnchorRecommendation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	defer tx.Rollback()
	var current model.LiveAnchorStyle
	var profile string
	err = tx.QueryRowContext(ctx, `SELECT name,description,avatar_type,COALESCE(avatar_data,''),profile_json FROM live_anchor_styles WHERE id=? AND tenant_id=? AND status='active' FOR UPDATE`, source.ID, source.TenantID).Scan(&current.Name, &current.Description, &current.AvatarType, &current.AvatarURL, &profile)
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	current.Profile = decodeLiveAnchorStyleProfile(profile)
	rows, err := tx.QueryContext(ctx, `SELECT plugin_id,plugin_version,enabled,parameters_json FROM live_anchor_style_plugins WHERE tenant_id=? AND style_id=? ORDER BY id FOR UPDATE`, source.TenantID, source.ID)
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	current.PluginSettings = []model.LiveAnchorStylePluginSetting{}
	for rows.Next() {
		var p model.LiveAnchorStylePluginSetting
		var enabled int
		var raw string
		if err = rows.Scan(&p.PluginID, &p.PluginVersion, &enabled, &raw); err != nil {
			rows.Close()
			return model.AnchorRecommendation{}, err
		}
		p.Enabled = enabled != 0
		if err = json.Unmarshal([]byte(raw), &p.Parameters); err != nil {
			rows.Close()
			return model.AnchorRecommendation{}, err
		}
		current.PluginSettings = append(current.PluginSettings, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	if model.AnchorRecommendationSHA(current) != expectedSHA || model.AnchorRecommendationSHA(source) != expectedSHA {
		return model.AnchorRecommendation{}, ErrAnchorRecommendationConflict
	}
	if !model.HasReusableAnchorProfile(current.Profile) {
		return model.AnchorRecommendation{}, errors.New("请先分析或保存主播风格，再推荐给用户")
	}
	current.Name = name
	current.Description = introduction
	result, err := tx.ExecContext(ctx, `INSERT INTO system_anchor_recommendations (source_tenant_id,source_style_id,name,introduction,summary,snapshot_json,source_sha256,generation_json,published_by_user_id) VALUES (?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=LAST_INSERT_ID(id),name=VALUES(name),introduction=VALUES(introduction),summary=VALUES(summary),snapshot_json=VALUES(snapshot_json),source_sha256=VALUES(source_sha256),generation_json=VALUES(generation_json),published_by_user_id=VALUES(published_by_user_id),version_no=version_no+1,status='active',updated_at=CURRENT_TIMESTAMP(3)`, source.TenantID, source.ID, name, introduction, summary, encodeJSON(current), expectedSHA, encodeJSON(generation), actor)
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AnchorRecommendation{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.AnchorRecommendation{}, err
	}
	return scanAnchorRecommendation(s.db.QueryRowContext(ctx, recommendationSelect+` WHERE id=?`, id))
}
func (s *Store) WithdrawAnchorRecommendation(ctx context.Context, id, version, actor int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE system_anchor_recommendations SET status='withdrawn',published_by_user_id=?,version_no=version_no+1,updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND version_no=? AND status='active'`, actor, id, version)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return ErrAnchorRecommendationConflict
	}
	return nil
}
func (s *Store) ImportAnchorRecommendations(ctx context.Context, tenant, actor int64, ids []int64) ([]model.LiveAnchorStyle, error) {
	ids = append([]int64(nil), ids...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	styleIDs := []int64{}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		rec, err := scanAnchorRecommendation(tx.QueryRowContext(ctx, recommendationSelect+` WHERE id=? AND status='active' FOR UPDATE`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAnchorRecommendationUnavailable
		}
		if err != nil {
			return nil, err
		}
		var styleID int64
		var status string
		err = tx.QueryRowContext(ctx, `SELECT id,status FROM live_anchor_styles WHERE tenant_id=? AND source_recommendation_id=? FOR UPDATE`, tenant, id).Scan(&styleID, &status)
		if err == nil {
			if status != "active" {
				if _, err = tx.ExecContext(ctx, `UPDATE live_anchor_styles SET status='active',updated_by_user_id=? WHERE id=? AND tenant_id=?`, actor, styleID, tenant); err != nil {
					return nil, err
				}
			}
			styleIDs = append(styleIDs, styleID)
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		profile := rec.Snapshot.Profile
		inserted, err := tx.ExecContext(ctx, `INSERT INTO live_anchor_styles (tenant_id,name,description,avatar_type,avatar_data,source_recommendation_id,profile_json,created_by_user_id,updated_by_user_id) VALUES (?,?,?,?,?,?,?,?,?)`, tenant, rec.Name, rec.Summary, rec.AvatarType, rec.AvatarURL, rec.ID, encodeJSON(profile), actor, actor)
		if err != nil {
			return nil, err
		}
		styleID, err = inserted.LastInsertId()
		if err != nil {
			return nil, err
		}
		version, err := tx.ExecContext(ctx, `INSERT INTO live_anchor_style_analysis_versions (tenant_id,style_id,version_no,kind,sample_ids_json,source_text,profile_json,created_by_user_id) VALUES (?,?,1,'recommendation_import','[]','',?,?)`, tenant, styleID, encodeJSON(profile), actor)
		if err != nil {
			return nil, err
		}
		versionID, err := version.LastInsertId()
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE live_anchor_styles SET active_analysis_version_id=? WHERE id=? AND tenant_id=?`, versionID, styleID, tenant); err != nil {
			return nil, err
		}
		for _, p := range rec.Snapshot.PluginSettings {
			if _, err = tx.ExecContext(ctx, `INSERT INTO live_anchor_style_plugins (tenant_id,style_id,plugin_id,plugin_version,enabled,parameters_json,updated_by_user_id) VALUES (?,?,?,?,?,?,?)`, tenant, styleID, p.PluginID, p.PluginVersion, boolInt(p.Enabled), encodeJSON(p.Parameters), actor); err != nil {
				return nil, err
			}
		}
		styleIDs = append(styleIDs, styleID)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	items := []model.LiveAnchorStyle{}
	for _, id := range styleIDs {
		item, err := s.GetLiveAnchorStyle(ctx, tenant, id)
		if err != nil {
			return nil, fmt.Errorf("读取已加入主播: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}
