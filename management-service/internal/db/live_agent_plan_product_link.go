package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanProductLinkNotFound = errors.New("live agent plan product link not found")
var ErrLiveAgentPlanProductLinkVersionConflict = errors.New("live agent plan product link version conflict")

const liveAgentPlanProductLinkSelect = `
	SELECT id, tenant_id, plan_id, link_key, product_name, room_roles_json, spec, daily_price, quantity, audience,
	       source_quote, source_review_bucket, source_review_reason, source_type, source_ref,
	       status, version_no, created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_product_links
`

func scanLiveAgentPlanProductLink(row interface {
	Scan(dest ...any) error
}) (model.LiveAgentPlanProductLink, error) {
	var item model.LiveAgentPlanProductLink
	var createdBy, updatedBy sql.NullInt64
	var roomRolesJSON sql.NullString
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.LinkKey, &item.ProductName,
		&roomRolesJSON, &item.Spec, &item.DailyPrice, &item.Quantity, &item.Audience, &item.SourceQuote,
		&item.SourceReviewBucket, &item.SourceReviewReason, &item.SourceType, &item.SourceRef,
		&item.Status, &item.VersionNo, &createdBy, &updatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	item.RoomRoles = []string{}
	if roomRolesJSON.Valid && strings.TrimSpace(roomRolesJSON.String) != "" {
		_ = json.Unmarshal([]byte(roomRolesJSON.String), &item.RoomRoles)
		item.RoomRoles = normalizeLiveRoomProductRoles(item.RoomRoles)
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

func normalizeLiveRoomProductRoles(values []string) []string {
	allowed := make(map[string]bool, len(model.LiveRoomProductRoles))
	for _, value := range model.LiveRoomProductRoles {
		allowed[value] = true
	}
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, raw := range values {
		value := strings.ToLower(strings.TrimSpace(raw))
		if !allowed[value] || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	if seen[model.LiveRoomProductRoleOrdinary] && len(result) > 1 {
		return []string{model.LiveRoomProductRoleOrdinary}
	}
	return result
}

func liveRoomProductRolesJSON(values []string) string {
	encoded, _ := json.Marshal(normalizeLiveRoomProductRoles(values))
	return string(encoded)
}

func (s *Store) ListLiveAgentPlanProductLinks(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanProductLink, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanProductLinkSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY link_key ASC, id ASC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanProductLink, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanProductLink(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, items); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) getLiveAgentPlanProductLinkByKey(ctx context.Context, tenantID, planID int64, linkKey string) (model.LiveAgentPlanProductLink, error) {
	item, err := scanLiveAgentPlanProductLink(s.db.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+`
		WHERE tenant_id=? AND plan_id=? AND link_key=?
	`, tenantID, planID, strings.TrimSpace(linkKey)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanProductLink{}, ErrLiveAgentPlanProductLinkNotFound
	}
	if err != nil {
		return item, err
	}
	items := []model.LiveAgentPlanProductLink{item}
	if err := s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, items); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	return items[0], nil
}

func sameProductLinkContent(existing model.LiveAgentPlanProductLink, candidate model.LiveAgentPlanProductLinkCandidate) bool {
	commonMatches := strings.TrimSpace(existing.ProductName) == strings.TrimSpace(candidate.ProductName) &&
		strings.TrimSpace(existing.Spec) == strings.TrimSpace(candidate.Spec) &&
		strings.TrimSpace(existing.DailyPrice) == strings.TrimSpace(candidate.DailyPrice) &&
		strings.TrimSpace(existing.Quantity) == strings.TrimSpace(candidate.Quantity) &&
		strings.TrimSpace(existing.Audience) == strings.TrimSpace(candidate.Audience)
	if commonMatches && len(candidate.RoomRoles) > 0 {
		commonMatches = liveRoomProductRolesJSON(existing.RoomRoles) == liveRoomProductRolesJSON(candidate.RoomRoles)
	}
	if !commonMatches || len(candidate.Attributes) == 0 {
		return commonMatches
	}
	if len(existing.Attributes) != len(candidate.Attributes) {
		return false
	}
	values := make(map[string]string, len(existing.Attributes))
	for _, item := range existing.Attributes {
		values[strings.ToLower(strings.TrimSpace(item.Code))] = strings.TrimSpace(item.Label) + "\x00" + strings.TrimSpace(item.Value) + "\x00" + strings.TrimSpace(item.Unit)
	}
	for index, raw := range candidate.Attributes {
		item := normalizeProductAttributeCandidate(raw, (index+1)*10)
		if values[item.Code] != item.Label+"\x00"+item.Value+"\x00"+item.Unit {
			return false
		}
	}
	return true
}

func (s *Store) AdoptLiveAgentPlanProductLink(
	ctx context.Context, tenantID, planID, actorUserID int64,
	candidate model.LiveAgentPlanProductLinkCandidate, sourceRef string,
) (model.LiveAgentPlanProductLinkAdoptionResult, error) {
	result := model.LiveAgentPlanProductLinkAdoptionResult{Candidate: candidate}
	var planExists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active'`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return result, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return result, err
	}

	linkKey := strings.TrimSpace(candidate.LinkKey)
	existing, err := s.getLiveAgentPlanProductLinkByKey(ctx, tenantID, planID, linkKey)
	if err == nil {
		result.Existing = &existing
		if existing.Status == "disabled" {
			tx, txErr := s.db.BeginTx(ctx, nil)
			if txErr != nil {
				return result, txErr
			}
			defer tx.Rollback()
			nextVersion := existing.VersionNo + 1
			sourceQuote := strings.Join(candidate.SourceQuotes, "\n")
			roomRoles := candidate.RoomRoles
			if len(roomRoles) == 0 {
				roomRoles = existing.RoomRoles
			}
			roomRolesJSON := liveRoomProductRolesJSON(roomRoles)
			_, txErr = tx.ExecContext(ctx, `
				UPDATE live_agent_plan_product_links
				SET product_name=?, room_roles_json=?, spec=?, daily_price=?, quantity=?, audience=?, source_quote=?,
				    source_review_bucket=?, source_review_reason=?, source_type='analysis_adoption', source_ref=?,
				    status='active', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
				WHERE id=? AND tenant_id=? AND plan_id=?
			`, strings.TrimSpace(candidate.ProductName), roomRolesJSON, strings.TrimSpace(candidate.Spec), strings.TrimSpace(candidate.DailyPrice),
				strings.TrimSpace(candidate.Quantity), strings.TrimSpace(candidate.Audience), sourceQuote,
				strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef),
				nextVersion, actorUserID, existing.ID, tenantID, planID)
			if txErr != nil {
				return result, txErr
			}
			_, txErr = tx.ExecContext(ctx, `
				INSERT INTO live_agent_plan_product_link_revisions (
					tenant_id, plan_id, product_link_id, version_no, action, link_key, product_name, room_roles_json, spec,
					daily_price, quantity, audience, status, source_quote, source_review_bucket,
					source_review_reason, source_type, source_ref, actor_user_id
				) VALUES (?, ?, ?, ?, 'readopt', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 'analysis_adoption', ?, ?)
			`, tenantID, planID, existing.ID, nextVersion, linkKey, strings.TrimSpace(candidate.ProductName), roomRolesJSON, strings.TrimSpace(candidate.Spec),
				strings.TrimSpace(candidate.DailyPrice), strings.TrimSpace(candidate.Quantity), strings.TrimSpace(candidate.Audience),
				sourceQuote, strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef), actorUserID)
			if txErr != nil {
				return result, txErr
			}
			if txErr = adoptLiveAgentPlanProductAttributes(ctx, tx, tenantID, planID, existing.ID, actorUserID, candidate.Attributes, sourceRef); txErr != nil {
				return result, txErr
			}
			if txErr = tx.Commit(); txErr != nil {
				return result, txErr
			}
			saved, txErr := scanLiveAgentPlanProductLink(s.db.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, existing.ID, tenantID, planID))
			if txErr != nil {
				return result, txErr
			}
			links := []model.LiveAgentPlanProductLink{saved}
			if txErr = s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, links); txErr != nil {
				return result, txErr
			}
			saved = links[0]
			result.Status = "adopted"
			result.Message = "已重新采纳为当前直播方案正式商品链接"
			result.Saved = &saved
			return result, nil
		}
		if sameProductLinkContent(existing, candidate) {
			result.Status = "unchanged"
			result.Message = "直播方案中已存在相同商品链接"
			return result, nil
		}
		result.Status = "conflict"
		result.Message = "当前方案已有同一链接的不同商品信息，需要通过智能体修改形成新版本"
		return result, nil
	}
	if !errors.Is(err, ErrLiveAgentPlanProductLinkNotFound) {
		return result, err
	}

	sourceQuote := strings.Join(candidate.SourceQuotes, "\n")
	roomRolesJSON := liveRoomProductRolesJSON(candidate.RoomRoles)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()

	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_links (
			tenant_id, plan_id, link_key, product_name, room_roles_json, spec, daily_price, quantity, audience,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref,
			status, version_no, created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, 'active', 1, ?, ?)
	`, tenantID, planID, linkKey, strings.TrimSpace(candidate.ProductName), roomRolesJSON, strings.TrimSpace(candidate.Spec),
		strings.TrimSpace(candidate.DailyPrice), strings.TrimSpace(candidate.Quantity), strings.TrimSpace(candidate.Audience),
		sourceQuote, strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason),
		strings.TrimSpace(sourceRef), actorUserID, actorUserID)
	if err != nil {
		return result, err
	}
	productLinkID, err := insertResult.LastInsertId()
	if err != nil {
		return result, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_link_revisions (
			tenant_id, plan_id, product_link_id, version_no, action, link_key, product_name, room_roles_json, spec,
			daily_price, quantity, audience, status, source_quote, source_review_bucket,
			source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, 1, 'adopt', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 'analysis_adoption', ?, ?)
	`, tenantID, planID, productLinkID, linkKey, strings.TrimSpace(candidate.ProductName), roomRolesJSON, strings.TrimSpace(candidate.Spec),
		strings.TrimSpace(candidate.DailyPrice), strings.TrimSpace(candidate.Quantity), strings.TrimSpace(candidate.Audience),
		sourceQuote, strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason),
		strings.TrimSpace(sourceRef), actorUserID)
	if err != nil {
		return result, err
	}
	if err := adoptLiveAgentPlanProductAttributes(ctx, tx, tenantID, planID, productLinkID, actorUserID, candidate.Attributes, sourceRef); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}

	saved, err := scanLiveAgentPlanProductLink(s.db.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, productLinkID, tenantID, planID))
	if err != nil {
		return result, err
	}
	links := []model.LiveAgentPlanProductLink{saved}
	if err := s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, links); err != nil {
		return result, err
	}
	saved = links[0]
	result.Status = "adopted"
	result.Message = "已采纳为当前直播方案正式商品链接"
	result.Saved = &saved
	return result, nil
}

func (s *Store) UpdateLiveAgentPlanProductLink(
	ctx context.Context, tenantID, planID, productLinkID, actorUserID int64,
	input model.UpdateLiveAgentPlanProductLinkInput,
) (model.LiveAgentPlanProductLink, error) {
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
	if input.ExpectedVersionNo > 0 && current.VersionNo != input.ExpectedVersionNo {
		return model.LiveAgentPlanProductLink{}, ErrLiveAgentPlanProductLinkVersionConflict
	}
	linkKey := strings.TrimSpace(input.LinkKey)
	if linkKey == "" {
		linkKey = current.LinkKey
	}
	if linkKey != current.LinkKey {
		var conflictID int64
		err = tx.QueryRowContext(ctx, `SELECT id FROM live_agent_plan_product_links WHERE tenant_id=? AND plan_id=? AND link_key=? AND id<>? LIMIT 1`, tenantID, planID, linkKey, productLinkID).Scan(&conflictID)
		if err == nil {
			return model.LiveAgentPlanProductLink{}, errors.New("same product link key already exists")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.LiveAgentPlanProductLink{}, err
		}
	}
	roomRoles := current.RoomRoles
	if input.RoomRoles != nil {
		roomRoles = normalizeLiveRoomProductRoles(input.RoomRoles)
	}
	roomRolesJSON := liveRoomProductRolesJSON(roomRoles)
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_product_links
		SET link_key=?, product_name=?, room_roles_json=?, spec=?, daily_price=?, quantity=?, audience=?,
		    source_type='system_agent_edit', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, linkKey, strings.TrimSpace(input.ProductName), roomRolesJSON, strings.TrimSpace(input.Spec), strings.TrimSpace(input.DailyPrice),
		strings.TrimSpace(input.Quantity), strings.TrimSpace(input.Audience), nextVersion, actorUserID, productLinkID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_link_revisions (tenant_id, plan_id, product_link_id, version_no, action, link_key, product_name, room_roles_json, spec, daily_price, quantity, audience, status, source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id)
		VALUES (?, ?, ?, ?, 'update', ?, ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, 'system_agent_edit', '', ?)
	`, tenantID, planID, productLinkID, nextVersion, linkKey, strings.TrimSpace(input.ProductName), roomRolesJSON, strings.TrimSpace(input.Spec),
		strings.TrimSpace(input.DailyPrice), strings.TrimSpace(input.Quantity), strings.TrimSpace(input.Audience), current.SourceQuote,
		current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	if err := relinkLiveAgentPlanBenefitsForLink(ctx, tx, tenantID, planID, current.LinkKey, linkKey, actorUserID); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	updated, err := scanLiveAgentPlanProductLink(s.db.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, productLinkID, tenantID, planID))
	if err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	links := []model.LiveAgentPlanProductLink{updated}
	if err := s.attachLiveAgentPlanProductAttributes(ctx, tenantID, planID, links); err != nil {
		return model.LiveAgentPlanProductLink{}, err
	}
	return links[0], nil
}

func (s *Store) DeleteLiveAgentPlanProductLink(ctx context.Context, tenantID, planID, productLinkID, actorUserID int64) error {
	return s.DeleteLiveAgentPlanProductLinkWithExpectedVersion(ctx, tenantID, planID, productLinkID, actorUserID, 0)
}

func (s *Store) DeleteLiveAgentPlanProductLinkWithExpectedVersion(ctx context.Context, tenantID, planID, productLinkID, actorUserID, expectedVersionNo int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanProductLink(tx.QueryRowContext(ctx, liveAgentPlanProductLinkSelect+` WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE`, productLinkID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanProductLinkNotFound
	}
	if err != nil {
		return err
	}
	if expectedVersionNo > 0 && current.VersionNo != expectedVersionNo {
		return ErrLiveAgentPlanProductLinkVersionConflict
	}
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `UPDATE live_agent_plan_product_links SET status='disabled', source_type='system_agent_delete', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'`, nextVersion, actorUserID, productLinkID, tenantID, planID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_product_link_revisions (tenant_id, plan_id, product_link_id, version_no, action, link_key, product_name, room_roles_json, spec, daily_price, quantity, audience, status, source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id)
		VALUES (?, ?, ?, ?, 'delete', ?, ?, ?, ?, ?, ?, ?, 'disabled', ?, ?, ?, 'system_agent_delete', '', ?)
	`, tenantID, planID, productLinkID, nextVersion, current.LinkKey, current.ProductName, liveRoomProductRolesJSON(current.RoomRoles), current.Spec, current.DailyPrice, current.Quantity, current.Audience, current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return err
	}
	if err := disableLiveAgentPlanBenefitsForLink(ctx, tx, tenantID, planID, current.LinkKey, actorUserID); err != nil {
		return err
	}
	if err := disableLiveAgentPlanProductAttributesForLink(ctx, tx, tenantID, planID, productLinkID, actorUserID); err != nil {
		return err
	}
	return tx.Commit()
}
