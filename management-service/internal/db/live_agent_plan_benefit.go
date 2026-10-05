package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanBenefitNotFound = errors.New("live agent plan benefit not found")
var ErrLiveAgentPlanBenefitVersionConflict = errors.New("live agent plan benefit version conflict")

const liveAgentPlanBenefitSelect = `
	SELECT id, tenant_id, plan_id, benefit_key, link_key, product_name, activity_price, gift,
	       activity_text, starts_at, ends_at, source_quote, source_review_bucket, source_review_reason,
	       source_type, source_ref, status, version_no, created_by_user_id, updated_by_user_id,
	       created_at, updated_at
	FROM live_agent_plan_benefits
`

func scanLiveAgentPlanBenefit(row interface {
	Scan(dest ...any) error
}) (model.LiveAgentPlanBenefit, error) {
	var (
		item      model.LiveAgentPlanBenefit
		startsAt  sql.NullTime
		endsAt    sql.NullTime
		createdBy sql.NullInt64
		updatedBy sql.NullInt64
	)
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.Key, &item.LinkKey, &item.ProductName,
		&item.ActivityPrice, &item.Gift, &item.Activity, &startsAt, &endsAt, &item.SourceQuote,
		&item.SourceReviewBucket, &item.SourceReviewReason, &item.SourceType, &item.SourceRef,
		&item.Status, &item.VersionNo, &createdBy, &updatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	if startsAt.Valid {
		value := startsAt.Time
		item.StartsAt = &value
	}
	if endsAt.Valid {
		value := endsAt.Time
		item.EndsAt = &value
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

func (s *Store) ListLiveAgentPlanBenefits(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanBenefit, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE tenant_id=? AND plan_id=? AND status<>'disabled'
		ORDER BY
			CASE status WHEN 'active' THEN 0 WHEN 'draft' THEN 1 WHEN 'expired' THEN 2 ELSE 3 END,
			updated_at DESC, id DESC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanBenefit, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanBenefit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListActiveLiveAgentPlanBenefits(ctx context.Context, tenantID, planID int64, now time.Time) ([]model.LiveAgentPlanBenefit, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		  AND (starts_at IS NULL OR starts_at<=?)
		  AND (ends_at IS NULL OR ends_at>=?)
		ORDER BY updated_at DESC, id DESC
	`, tenantID, planID, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanBenefit, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanBenefit(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getLiveAgentPlanBenefitByKey(ctx context.Context, tenantID, planID int64, key string) (model.LiveAgentPlanBenefit, error) {
	item, err := scanLiveAgentPlanBenefit(s.db.QueryRowContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE tenant_id=? AND plan_id=? AND benefit_key=?
	`, tenantID, planID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanBenefit{}, ErrLiveAgentPlanBenefitNotFound
	}
	return item, err
}

func parseOptionalBenefitTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, errors.New("invalid benefit time")
}

func normalizeBenefitKey(candidate model.LiveAgentPlanBenefitCandidate) string {
	key := strings.TrimSpace(candidate.Key)
	if key != "" {
		return key
	}
	linkKey := strings.TrimSpace(candidate.LinkKey)
	if linkKey == "" {
		linkKey = "room"
	}
	return linkKey + ":current-benefit"
}

func benefitStatusForWindow(startsAt, endsAt *time.Time, now time.Time) string {
	if startsAt == nil || endsAt == nil {
		return "draft"
	}
	if endsAt.Before(*startsAt) {
		return "draft"
	}
	if now.Before(*startsAt) {
		return "draft"
	}
	if now.After(*endsAt) {
		return "expired"
	}
	return "active"
}

func sameBenefitContent(existing model.LiveAgentPlanBenefit, candidate model.LiveAgentPlanBenefitCandidate, startsAt, endsAt *time.Time) bool {
	sameTime := func(left *time.Time, right *time.Time) bool {
		if left == nil || right == nil {
			return left == nil && right == nil
		}
		return left.Equal(*right)
	}
	return strings.TrimSpace(existing.LinkKey) == strings.TrimSpace(candidate.LinkKey) &&
		strings.TrimSpace(existing.ProductName) == strings.TrimSpace(candidate.ProductName) &&
		strings.TrimSpace(existing.ActivityPrice) == strings.TrimSpace(candidate.ActivityPrice) &&
		strings.TrimSpace(existing.Gift) == strings.TrimSpace(candidate.Gift) &&
		strings.TrimSpace(existing.Activity) == strings.TrimSpace(candidate.Activity) &&
		sameTime(existing.StartsAt, startsAt) && sameTime(existing.EndsAt, endsAt)
}

func (s *Store) AdoptLiveAgentPlanBenefit(
	ctx context.Context, tenantID, planID, actorUserID int64,
	candidate model.LiveAgentPlanBenefitCandidate, sourceRef string,
) (model.LiveAgentPlanBenefitAdoptionResult, error) {
	result := model.LiveAgentPlanBenefitAdoptionResult{Candidate: candidate}
	var planExists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active'`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return result, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return result, err
	}

	startsAt, err := parseOptionalBenefitTime(candidate.StartsAt)
	if err != nil {
		return result, err
	}
	endsAt, err := parseOptionalBenefitTime(candidate.EndsAt)
	if err != nil {
		return result, err
	}
	key := normalizeBenefitKey(candidate)
	status := benefitStatusForWindow(startsAt, endsAt, time.Now())
	sourceQuote := strings.Join(candidate.SourceQuotes, "\n")

	existing, err := s.getLiveAgentPlanBenefitByKey(ctx, tenantID, planID, key)
	if err == nil {
		result.Existing = &existing
		if sameBenefitContent(existing, candidate, startsAt, endsAt) {
			result.Status = "unchanged"
			result.Message = "直播方案中已存在相同活动福利"
			return result, nil
		}
		result.Status = "conflict"
		result.Message = "当前方案已有同一活动位的不同内容，需要通过智能体修改形成新版本"
		return result, nil
	}
	if !errors.Is(err, ErrLiveAgentPlanBenefitNotFound) {
		return result, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_benefits (
			tenant_id, plan_id, benefit_key, link_key, product_name, activity_price, gift,
			activity_text, starts_at, ends_at, source_quote, source_review_bucket,
			source_review_reason, source_type, source_ref, status, version_no,
			created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, ?, 1, ?, ?)
	`, tenantID, planID, key, strings.TrimSpace(candidate.LinkKey), strings.TrimSpace(candidate.ProductName),
		strings.TrimSpace(candidate.ActivityPrice), strings.TrimSpace(candidate.Gift), strings.TrimSpace(candidate.Activity),
		startsAt, endsAt, sourceQuote, strings.TrimSpace(candidate.ReviewBucket),
		strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef), status, actorUserID, actorUserID)
	if err != nil {
		return result, err
	}
	benefitID, err := insertResult.LastInsertId()
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_benefit_revisions (
			tenant_id, plan_id, benefit_id, version_no, action, benefit_key, link_key,
			product_name, activity_price, gift, activity_text, starts_at, ends_at, status,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, 1, 'adopt', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, ?)
	`, tenantID, planID, benefitID, key, strings.TrimSpace(candidate.LinkKey), strings.TrimSpace(candidate.ProductName),
		strings.TrimSpace(candidate.ActivityPrice), strings.TrimSpace(candidate.Gift), strings.TrimSpace(candidate.Activity),
		startsAt, endsAt, status, sourceQuote, strings.TrimSpace(candidate.ReviewBucket),
		strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef), actorUserID)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}

	saved, err := scanLiveAgentPlanBenefit(s.db.QueryRowContext(ctx, liveAgentPlanBenefitSelect+` WHERE id=? AND tenant_id=? AND plan_id=?`, benefitID, tenantID, planID))
	if err != nil {
		return result, err
	}
	if saved.Status == "active" {
		result.Status = "adopted"
		result.Message = "已采纳并在有效期内生效"
	} else {
		result.Status = "drafted"
		if saved.Status == "expired" {
			result.Message = "已采纳为历史活动，但当前已过期，不会进入直播智能体"
		} else {
			result.Message = "已采纳为活动草稿；缺少有效期或尚未到开始时间，不会进入直播智能体"
		}
	}
	result.Saved = &saved
	return result, nil
}

func (s *Store) UpdateLiveAgentPlanBenefit(
	ctx context.Context, tenantID, planID, benefitID, actorUserID int64,
	input model.UpdateLiveAgentPlanBenefitInput,
) (model.LiveAgentPlanBenefit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	defer tx.Rollback()

	current, err := scanLiveAgentPlanBenefit(tx.QueryRowContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled' FOR UPDATE
	`, benefitID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanBenefit{}, ErrLiveAgentPlanBenefitNotFound
	}
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	if input.ExpectedVersionNo > 0 && current.VersionNo != input.ExpectedVersionNo {
		return model.LiveAgentPlanBenefit{}, ErrLiveAgentPlanBenefitVersionConflict
	}

	startsAt, err := parseOptionalBenefitTime(input.StartsAt)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	endsAt, err := parseOptionalBenefitTime(input.EndsAt)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		key = current.Key
	}
	if key != current.Key {
		return model.LiveAgentPlanBenefit{}, errors.New("benefit key cannot be changed")
	}

	candidate := model.LiveAgentPlanBenefitCandidate{
		Key:           key,
		LinkKey:       strings.TrimSpace(input.LinkKey),
		ProductName:   strings.TrimSpace(input.ProductName),
		ActivityPrice: strings.TrimSpace(input.ActivityPrice),
		Gift:          strings.TrimSpace(input.Gift),
		Activity:      strings.TrimSpace(input.Activity),
	}
	if sameBenefitContent(current, candidate, startsAt, endsAt) {
		return current, nil
	}

	status := benefitStatusForWindow(startsAt, endsAt, time.Now())
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_benefits
		SET link_key=?, product_name=?, activity_price=?, gift=?, activity_text=?, starts_at=?, ends_at=?,
		    status=?, source_type='system_agent_edit', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled'
	`, candidate.LinkKey, candidate.ProductName, candidate.ActivityPrice, candidate.Gift, candidate.Activity,
		startsAt, endsAt, status, nextVersion, actorUserID, benefitID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_benefit_revisions (
			tenant_id, plan_id, benefit_id, version_no, action, benefit_key, link_key,
			product_name, activity_price, gift, activity_text, starts_at, ends_at, status,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'update', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'system_agent_edit', '', ?)
	`, tenantID, planID, benefitID, nextVersion, current.Key, candidate.LinkKey, candidate.ProductName,
		candidate.ActivityPrice, candidate.Gift, candidate.Activity, startsAt, endsAt, status,
		current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanBenefit{}, err
	}
	return scanLiveAgentPlanBenefit(s.db.QueryRowContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=?
	`, benefitID, tenantID, planID))
}

func (s *Store) DeleteLiveAgentPlanBenefit(
	ctx context.Context, tenantID, planID, benefitID, actorUserID int64,
) error {
	return s.DeleteLiveAgentPlanBenefitWithExpectedVersion(ctx, tenantID, planID, benefitID, actorUserID, 0)
}

func (s *Store) DeleteLiveAgentPlanBenefitWithExpectedVersion(
	ctx context.Context, tenantID, planID, benefitID, actorUserID, expectedVersionNo int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanLiveAgentPlanBenefit(tx.QueryRowContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled' FOR UPDATE
	`, benefitID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanBenefitNotFound
	}
	if err != nil {
		return err
	}
	if expectedVersionNo > 0 && current.VersionNo != expectedVersionNo {
		return ErrLiveAgentPlanBenefitVersionConflict
	}
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_benefits
		SET status='disabled', source_type='system_agent_delete', source_ref='', version_no=?,
		    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled'
	`, nextVersion, actorUserID, benefitID, tenantID, planID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_benefit_revisions (
			tenant_id, plan_id, benefit_id, version_no, action, benefit_key, link_key,
			product_name, activity_price, gift, activity_text, starts_at, ends_at, status,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'delete', ?, ?, ?, ?, ?, ?, ?, ?, 'disabled', ?, ?, ?, 'system_agent_delete', '', ?)
	`, tenantID, planID, benefitID, nextVersion, current.Key, current.LinkKey, current.ProductName,
		current.ActivityPrice, current.Gift, current.Activity, current.StartsAt, current.EndsAt,
		current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

// disableLiveAgentPlanBenefitsForLink removes every link-scoped benefit from
// the active domain in the same transaction as the parent product-link delete.
// Revision rows remain audit-only and are never returned to generation.
func disableLiveAgentPlanBenefitsForLink(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, planID int64,
	linkKey string,
	actorUserID int64,
) error {
	linkKey = strings.TrimSpace(linkKey)
	if linkKey == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE tenant_id=? AND plan_id=? AND link_key=? AND status<>'disabled'
		FOR UPDATE
	`, tenantID, planID, linkKey)
	if err != nil {
		return err
	}
	items := make([]model.LiveAgentPlanBenefit, 0)
	for rows.Next() {
		item, scanErr := scanLiveAgentPlanBenefit(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, current := range items {
		nextVersion := current.VersionNo + 1
		if _, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_benefits
			SET status='disabled', source_type='system_agent_parent_delete', source_ref='', version_no=?,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled'
		`, nextVersion, actorUserID, current.ID, tenantID, planID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_benefit_revisions (
				tenant_id, plan_id, benefit_id, version_no, action, benefit_key, link_key,
				product_name, activity_price, gift, activity_text, starts_at, ends_at, status,
				source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
			) VALUES (?, ?, ?, ?, 'cascade_delete', ?, ?, ?, ?, ?, ?, ?, ?, 'disabled', ?, ?, ?, 'system_agent_parent_delete', '', ?)
		`, tenantID, planID, current.ID, nextVersion, current.Key, current.LinkKey, current.ProductName,
			current.ActivityPrice, current.Gift, current.Activity, current.StartsAt, current.EndsAt,
			current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID); err != nil {
			return err
		}
	}
	return nil
}

func relinkLiveAgentPlanBenefitsForLink(
	ctx context.Context,
	tx *sql.Tx,
	tenantID, planID int64,
	oldLinkKey, newLinkKey string,
	actorUserID int64,
) error {
	oldLinkKey = strings.TrimSpace(oldLinkKey)
	newLinkKey = strings.TrimSpace(newLinkKey)
	if oldLinkKey == "" || newLinkKey == "" || oldLinkKey == newLinkKey {
		return nil
	}
	rows, err := tx.QueryContext(ctx, liveAgentPlanBenefitSelect+`
		WHERE tenant_id=? AND plan_id=? AND link_key=? AND status<>'disabled'
		FOR UPDATE
	`, tenantID, planID, oldLinkKey)
	if err != nil {
		return err
	}
	items := make([]model.LiveAgentPlanBenefit, 0)
	for rows.Next() {
		item, scanErr := scanLiveAgentPlanBenefit(rows)
		if scanErr != nil {
			rows.Close()
			return scanErr
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, current := range items {
		nextVersion := current.VersionNo + 1
		if _, err = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_benefits
			SET link_key=?, source_type='system_agent_parent_edit', source_ref='', version_no=?,
			    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=? AND plan_id=? AND status<>'disabled'
		`, newLinkKey, nextVersion, actorUserID, current.ID, tenantID, planID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_benefit_revisions (
				tenant_id, plan_id, benefit_id, version_no, action, benefit_key, link_key,
				product_name, activity_price, gift, activity_text, starts_at, ends_at, status,
				source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
			) VALUES (?, ?, ?, ?, 'cascade_relink', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'system_agent_parent_edit', '', ?)
		`, tenantID, planID, current.ID, nextVersion, current.Key, newLinkKey, current.ProductName,
			current.ActivityPrice, current.Gift, current.Activity, current.StartsAt, current.EndsAt, current.Status,
			current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID); err != nil {
			return err
		}
	}
	return nil
}
