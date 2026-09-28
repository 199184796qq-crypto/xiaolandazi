package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanFactNotFound = errors.New("live agent plan fact not found")
var ErrLiveAgentPlanFactVersionConflict = errors.New("live agent plan fact version conflict")

const liveAgentPlanFactSelect = `
	SELECT id, tenant_id, plan_id, category, fact_key, fact_value, source_quote,
		source_review_bucket, source_review_reason, source_type, source_ref,
		status, version_no, created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_facts
`

func scanLiveAgentPlanFact(row interface {
	Scan(dest ...any) error
}) (model.LiveAgentPlanFact, error) {
	var (
		item      model.LiveAgentPlanFact
		createdBy sql.NullInt64
		updatedBy sql.NullInt64
	)
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.Category, &item.Key, &item.Value,
		&item.SourceQuote, &item.SourceReviewBucket, &item.SourceReviewReason,
		&item.SourceType, &item.SourceRef, &item.Status, &item.VersionNo,
		&createdBy, &updatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
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

func (s *Store) ListLiveAgentPlanFacts(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanFact, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanFactSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY category ASC, fact_key ASC, id ASC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanFact, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanFact(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getLiveAgentPlanFactByKey(
	ctx context.Context,
	tenantID, planID int64,
	category, key string,
) (model.LiveAgentPlanFact, error) {
	item, err := scanLiveAgentPlanFact(s.db.QueryRowContext(ctx, liveAgentPlanFactSelect+`
		WHERE tenant_id=? AND plan_id=? AND category=? AND fact_key=?
	`, tenantID, planID, category, key))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanFactNotFound
	}
	return item, err
}

func (s *Store) AdoptLiveAgentPlanFact(
	ctx context.Context,
	tenantID, planID, actorUserID int64,
	candidate model.LiveAgentPlanFactCandidate,
	sourceRef string,
) (model.LiveAgentPlanFactAdoptionResult, error) {
	category := strings.TrimSpace(candidate.Category)
	if category == "" {
		category = "other"
	}
	key := strings.TrimSpace(candidate.Key)
	value := strings.TrimSpace(candidate.Value)
	result := model.LiveAgentPlanFactAdoptionResult{Candidate: candidate}

	var planExists int
	if err := s.db.QueryRowContext(ctx, `
		SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active'
	`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return result, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return result, err
	}

	existing, err := s.getLiveAgentPlanFactByKey(ctx, tenantID, planID, category, key)
	if err == nil {
		result.Existing = &existing
		if existing.Status == "disabled" {
			tx, txErr := s.db.BeginTx(ctx, nil)
			if txErr != nil {
				return result, txErr
			}
			defer tx.Rollback()
			nextVersion := existing.VersionNo + 1
			_, txErr = tx.ExecContext(ctx, `
				UPDATE live_agent_plan_facts
				SET fact_value=?, source_quote=?, source_review_bucket=?, source_review_reason=?,
				    source_type='analysis_adoption', source_ref=?, status='active', version_no=?,
				    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
				WHERE id=? AND tenant_id=? AND plan_id=?
			`, value, strings.TrimSpace(candidate.SourceQuote), strings.TrimSpace(candidate.ReviewBucket),
				strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef), nextVersion,
				actorUserID, existing.ID, tenantID, planID)
			if txErr != nil {
				return result, txErr
			}
			_, txErr = tx.ExecContext(ctx, `
				INSERT INTO live_agent_plan_fact_revisions (
					tenant_id, plan_id, fact_id, version_no, action, category, fact_key, fact_value,
					source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
				) VALUES (?, ?, ?, ?, 'readopt', ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, ?)
			`, tenantID, planID, existing.ID, nextVersion, category, key, value,
				strings.TrimSpace(candidate.SourceQuote), strings.TrimSpace(candidate.ReviewBucket),
				strings.TrimSpace(candidate.ReviewReason), strings.TrimSpace(sourceRef), actorUserID)
			if txErr != nil {
				return result, txErr
			}
			if txErr = tx.Commit(); txErr != nil {
				return result, txErr
			}
			saved, txErr := scanLiveAgentPlanFact(s.db.QueryRowContext(ctx, liveAgentPlanFactSelect+`
				WHERE id=? AND tenant_id=? AND plan_id=?
			`, existing.ID, tenantID, planID))
			if txErr != nil {
				return result, txErr
			}
			result.Status = "adopted"
			result.Message = "已重新采纳到当前直播方案"
			result.Saved = &saved
			return result, nil
		}
		if strings.TrimSpace(existing.Value) == value {
			result.Status = "unchanged"
			result.Message = "直播方案中已存在相同事实"
			return result, nil
		}
		result.Status = "conflict"
		result.Message = "直播方案中已存在同名但不同内容的事实，需要先处理冲突"
		return result, nil
	}
	if !errors.Is(err, ErrLiveAgentPlanFactNotFound) {
		return result, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_facts (
			tenant_id, plan_id, category, fact_key, fact_value, source_quote,
			source_review_bucket, source_review_reason, source_type, source_ref,
			status, version_no, created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, 'active', 1, ?, ?)
	`, tenantID, planID, category, key, value, strings.TrimSpace(candidate.SourceQuote),
		strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason),
		strings.TrimSpace(sourceRef), actorUserID, actorUserID)
	if err != nil {
		return result, err
	}
	factID, err := insertResult.LastInsertId()
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_fact_revisions (
			tenant_id, plan_id, fact_id, version_no, action, category, fact_key, fact_value,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, 1, 'adopt', ?, ?, ?, ?, ?, ?, 'analysis_adoption', ?, ?)
	`, tenantID, planID, factID, category, key, value, strings.TrimSpace(candidate.SourceQuote),
		strings.TrimSpace(candidate.ReviewBucket), strings.TrimSpace(candidate.ReviewReason),
		strings.TrimSpace(sourceRef), actorUserID)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	saved, err := scanLiveAgentPlanFact(s.db.QueryRowContext(ctx, liveAgentPlanFactSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=?
	`, factID, tenantID, planID))
	if err != nil {
		return result, err
	}
	result.Status = "adopted"
	result.Message = "已采纳到当前直播方案"
	result.Saved = &saved
	return result, nil
}

func (s *Store) UpdateLiveAgentPlanFact(
	ctx context.Context,
	tenantID, planID, factID, actorUserID int64,
	category, key, value string,
) (model.LiveAgentPlanFact, error) {
	return s.UpdateLiveAgentPlanFactWithExpectedVersion(ctx, tenantID, planID, factID, actorUserID, 0, category, key, value)
}

func (s *Store) UpdateLiveAgentPlanFactWithExpectedVersion(
	ctx context.Context,
	tenantID, planID, factID, actorUserID, expectedVersionNo int64,
	category, key, value string,
) (model.LiveAgentPlanFact, error) {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "other"
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	defer tx.Rollback()

	current, err := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
		FOR UPDATE
	`, factID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanFactNotFound
	}
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	if expectedVersionNo > 0 && current.VersionNo != expectedVersionNo {
		return model.LiveAgentPlanFact{}, ErrLiveAgentPlanFactVersionConflict
	}

	if current.Category != category || current.Key != key {
		var conflictID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM live_agent_plan_facts
			WHERE tenant_id=? AND plan_id=? AND category=? AND fact_key=? AND id<>?
			LIMIT 1
		`, tenantID, planID, category, key, factID).Scan(&conflictID)
		if err == nil {
			return model.LiveAgentPlanFact{}, errors.New("same fact key already exists")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.LiveAgentPlanFact{}, err
		}
	}

	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_facts
		SET category=?, fact_key=?, fact_value=?, source_type='manual_edit', source_ref='',
		    version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, category, key, value, nextVersion, actorUserID, factID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_fact_revisions (
			tenant_id, plan_id, fact_id, version_no, action, category, fact_key, fact_value,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'update', ?, ?, ?, ?, ?, ?, 'manual_edit', '', ?)
	`, tenantID, planID, factID, nextVersion, category, key, value,
		current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanFact{}, err
	}
	return scanLiveAgentPlanFact(s.db.QueryRowContext(ctx, liveAgentPlanFactSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=?
	`, factID, tenantID, planID))
}

func (s *Store) DeleteLiveAgentPlanFact(
	ctx context.Context,
	tenantID, planID, factID, actorUserID int64,
) error {
	return s.DeleteLiveAgentPlanFactWithExpectedVersion(ctx, tenantID, planID, factID, actorUserID, 0)
}

func (s *Store) DeleteLiveAgentPlanFactWithExpectedVersion(
	ctx context.Context,
	tenantID, planID, factID, actorUserID, expectedVersionNo int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	current, err := scanLiveAgentPlanFact(tx.QueryRowContext(ctx, liveAgentPlanFactSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
		FOR UPDATE
	`, factID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanFactNotFound
	}
	if err != nil {
		return err
	}
	if expectedVersionNo > 0 && current.VersionNo != expectedVersionNo {
		return ErrLiveAgentPlanFactVersionConflict
	}
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_facts
		SET status='disabled', source_type='manual_delete', source_ref='',
		    version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, nextVersion, actorUserID, factID, tenantID, planID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_fact_revisions (
			tenant_id, plan_id, fact_id, version_no, action, category, fact_key, fact_value,
			source_quote, source_review_bucket, source_review_reason, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'delete', ?, ?, ?, ?, ?, ?, 'manual_delete', '', ?)
	`, tenantID, planID, factID, nextVersion, current.Category, current.Key, current.Value,
		current.SourceQuote, current.SourceReviewBucket, current.SourceReviewReason, actorUserID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
