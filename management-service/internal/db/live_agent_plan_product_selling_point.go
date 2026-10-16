package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanProductSellingPointNotFound = errors.New("live agent plan product selling point not found")

const liveAgentPlanProductSellingPointSelect = `
	SELECT id, tenant_id, plan_id, product_link_id, selling_point, tier, point_weight, is_main,
	       angles_json, target_audience, pain_point, source_type, source_ref, status, version_no,
	       created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_product_selling_points
`

func scanLiveAgentPlanProductSellingPoint(row interface{ Scan(dest ...any) error }) (model.LiveAgentPlanProductSellingPoint, error) {
	var item model.LiveAgentPlanProductSellingPoint
	var createdBy, updatedBy sql.NullInt64
	var isMain int
	var anglesJSON string
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.ProductLinkID, &item.SellingPoint, &item.Tier,
		&item.PointWeight, &isMain, &anglesJSON, &item.TargetAudience, &item.PainPoint,
		&item.SourceType, &item.SourceRef, &item.Status, &item.VersionNo, &createdBy, &updatedBy,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanProductSellingPoint{}, err
	}
	item.IsMain = isMain != 0
	item.Angles = []model.LiveAgentPlanProductSellingAngle{}
	if strings.TrimSpace(anglesJSON) != "" {
		_ = json.Unmarshal([]byte(anglesJSON), &item.Angles)
	}
	if createdBy.Valid {
		value := createdBy.Int64
		item.CreatedByUserID = &value
	}
	if updatedBy.Valid {
		value := updatedBy.Int64
		item.UpdatedByUserID = &value
	}
	return item, nil
}

func (s *Store) listLiveAgentPlanProductSellingPoints(ctx context.Context, tenantID, planID int64) (map[int64][]model.LiveAgentPlanProductSellingPoint, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanProductSellingPointSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY product_link_id ASC, is_main DESC, point_weight DESC, id ASC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[int64][]model.LiveAgentPlanProductSellingPoint)
	for rows.Next() {
		item, err := scanLiveAgentPlanProductSellingPoint(rows)
		if err != nil {
			return nil, err
		}
		result[item.ProductLinkID] = append(result[item.ProductLinkID], item)
	}
	return result, rows.Err()
}

// attachLiveAgentPlanProductSellingPoints 把商品卖点池挂到商品链接列表上，key 为商品链接 ID。
func (s *Store) attachLiveAgentPlanProductSellingPoints(ctx context.Context, tenantID, planID int64, links []model.LiveAgentPlanProductLink) error {
	grouped, err := s.listLiveAgentPlanProductSellingPoints(ctx, tenantID, planID)
	if err != nil {
		return err
	}
	for index := range links {
		links[index].SellingPoints = grouped[links[index].ID]
		if links[index].SellingPoints == nil {
			links[index].SellingPoints = []model.LiveAgentPlanProductSellingPoint{}
		}
	}
	return nil
}

// ListLiveAgentPlanProductSellingPoints 返回某商品链接（productLinkID）的卖点池。
func (s *Store) ListLiveAgentPlanProductSellingPoints(ctx context.Context, tenantID, planID, productLinkID int64) ([]model.LiveAgentPlanProductSellingPoint, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanProductSellingPointSelect+`
		WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND status='active'
		ORDER BY is_main DESC, point_weight DESC, id ASC
	`, tenantID, planID, productLinkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanProductSellingPoint, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanProductSellingPoint(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func normalizeProductSellingAngle(a model.LiveAgentPlanProductSellingAngle) model.LiveAgentPlanProductSellingAngle {
	a.Angle = strings.TrimSpace(a.Angle)
	if a.Weight < 1 {
		a.Weight = 1
	}
	return a
}

// SaveLiveAgentPlanProductSellingPoints 整体替换某商品链接的卖点池：先软禁用旧卖点，再按传入重建。
// 每个商品独立维护，归属由 productLinkID 程序保证。
func (s *Store) SaveLiveAgentPlanProductSellingPoints(
	ctx context.Context, tenantID, planID, productLinkID, actorUserID int64,
	points []model.LiveAgentPlanProductSellingPointInput, sourceType, sourceRef string,
) ([]model.LiveAgentPlanProductSellingPoint, error) {
	if sourceType == "" {
		sourceType = "system_agent_edit"
	}
	// 校验卖点名与角度非空，去重。
	seenPoints := map[string]bool{}
	clean := make([]model.LiveAgentPlanProductSellingPointInput, 0, len(points))
	for _, p := range points {
		name := strings.TrimSpace(p.SellingPoint)
		if name == "" || seenPoints[name] {
			continue
		}
		seenPoints[name] = true
		cleanAngles := make([]model.LiveAgentPlanProductSellingAngle, 0, len(p.Angles))
		seenAngles := map[string]bool{}
		for _, a := range p.Angles {
			a = normalizeProductSellingAngle(a)
			if a.Angle == "" || seenAngles[a.Angle] {
				continue
			}
			seenAngles[a.Angle] = true
			cleanAngles = append(cleanAngles, a)
		}
		if len(cleanAngles) == 0 {
			continue
		}
		p.SellingPoint = name
		p.Angles = cleanAngles
		p.Tier = strings.TrimSpace(p.Tier)
		if p.PointWeight < 1 {
			p.PointWeight = 1 // 未设置或非正权重一律等权兜底，与角度权重对齐
		}
		clean = append(clean, p)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 硬删除旧卖点。整体替换语义下无需保留历史（无版本/修订表），
	// 且唯一键 uk_live_agent_plan_product_selling_point (plan_id, product_link_id, selling_point)
	// 不区分 status，软禁用会导致重建同名卖点时撞唯一键（Error 1062）。
	if _, err = tx.ExecContext(ctx, `
		DELETE FROM live_agent_plan_product_selling_points
		WHERE tenant_id=? AND plan_id=? AND product_link_id=?
	`, tenantID, planID, productLinkID); err != nil {
		return nil, err
	}
	for _, p := range clean {
		anglesJSON, _ := json.Marshal(p.Angles)
		isMain := 0
		if p.IsMain {
			isMain = 1
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_product_selling_points (
				tenant_id, plan_id, product_link_id, selling_point, tier, point_weight, is_main,
				angles_json, target_audience, pain_point, source_type, source_ref, status, version_no,
				created_by_user_id, updated_by_user_id
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, ?)
		`, tenantID, planID, productLinkID, p.SellingPoint, p.Tier, p.PointWeight, isMain, string(anglesJSON),
			strings.TrimSpace(p.TargetAudience), strings.TrimSpace(p.PainPoint), sourceType, strings.TrimSpace(sourceRef),
			actorUserID, actorUserID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListLiveAgentPlanProductSellingPoints(ctx, tenantID, planID, productLinkID)
}

// disableLiveAgentPlanProductSellingPointsForLink 删除商品链接时同步停用其卖点池。
func disableLiveAgentPlanProductSellingPointsForLink(ctx context.Context, tx *sql.Tx, tenantID, planID, productLinkID, actorUserID int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE live_agent_plan_product_selling_points
		SET status='disabled', updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE tenant_id=? AND plan_id=? AND product_link_id=? AND status='active'
	`, actorUserID, tenantID, planID, productLinkID)
	return err
}

// SetLiveAgentPlanProductLinkFrequencyWeight 设置单个商品链接的手动频率系数（×100：100=1.0 不干预，50~200=0.5~2.0，0=未设置=1.0）。
func (s *Store) SetLiveAgentPlanProductLinkFrequencyWeight(ctx context.Context, tenantID, planID, productLinkID, actorUserID int64, weight int) (model.LiveAgentPlanProductLink, error) {
	if weight != 0 && (weight < 50 || weight > 200) {
		return model.LiveAgentPlanProductLink{}, errors.New("商品频率系数须为 0（不干预）或 50~200（0.5~2.0）")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanProductLink(tx.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, productLinkID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanProductLink{}, ErrLiveAgentPlanProductLinkNotFound
	}
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	nextVersion := current.VersionNo + 1
	if _, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_product_links
		SET frequency_weight=?, version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, weight, nextVersion, actorUserID, productLinkID, tenantID, planID); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_link_revisions (tenant_id, plan_id, product_link_id, version_no, action, link_key, product_name, room_roles_json, spec, daily_price, quantity, audience, status, source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id)
		VALUES (?, ?, ?, ?, 'weight', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 'system_agent_weight', '', ?)
	`, tenantID, planID, productLinkID, nextVersion, current.LinkKey, current.ProductName, liveRoomProductRolesJSON(current.RoomRoles), current.Spec, current.DailyPrice, current.Quantity, current.Audience, current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	updated, err := scanLiveAgentPlanProductLink(s.db.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, productLinkID, tenantID, planID))
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	links := []model.LiveAgentPlanProductLink{updated}
	if err = s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, links); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	return links[0], nil
}
