package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

var ErrLiveAgentPlanScriptReferenceNotFound = errors.New("live agent plan script reference not found")
var ErrLiveAgentPlanScriptReferenceVersionConflict = errors.New("live agent plan script reference version conflict")
var ErrLiveAgentPlanScriptReferenceAlreadyExists = errors.New("live agent plan script reference already exists")

const liveAgentPlanScriptReferenceSelect = `
	SELECT id, tenant_id, plan_id, reference_key, title, content_text, goal, transition_text,
		execution_mode, source_quote, source_type, source_ref, status, version_no,
		created_by_user_id, updated_by_user_id, created_at, updated_at
	FROM live_agent_plan_script_references
`

func scanLiveAgentPlanScriptReference(row interface {
	Scan(dest ...any) error
}) (model.LiveAgentPlanScriptReference, error) {
	var (
		item      model.LiveAgentPlanScriptReference
		createdBy sql.NullInt64
		updatedBy sql.NullInt64
	)
	err := row.Scan(
		&item.ID, &item.TenantID, &item.PlanID, &item.ReferenceKey, &item.Title,
		&item.ContentText, &item.Goal, &item.Transition, &item.ExecutionMode,
		&item.SourceQuote, &item.SourceType, &item.SourceRef, &item.Status, &item.VersionNo,
		&createdBy, &updatedBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
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

func (s *Store) ListLiveAgentPlanScriptReferences(ctx context.Context, tenantID, planID int64) ([]model.LiveAgentPlanScriptReference, error) {
	rows, err := s.db.QueryContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE tenant_id=? AND plan_id=? AND status='active'
		ORDER BY updated_at DESC, id DESC
	`, tenantID, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LiveAgentPlanScriptReference, 0)
	for rows.Next() {
		item, err := scanLiveAgentPlanScriptReference(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) getLiveAgentPlanScriptReferenceByKey(ctx context.Context, tenantID, planID int64, key string) (model.LiveAgentPlanScriptReference, error) {
	item, err := scanLiveAgentPlanScriptReference(s.db.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE tenant_id=? AND plan_id=? AND reference_key=?
	`, tenantID, planID, strings.TrimSpace(key)))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceNotFound
	}
	return item, err
}

func normalizeScriptReferenceExecutionMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "verbatim":
		return "verbatim"
	default:
		return "intent"
	}
}

func (s *Store) CreateLiveAgentPlanScriptReference(
	ctx context.Context,
	tenantID, planID, actorUserID int64,
	input model.CreateLiveAgentPlanScriptReferenceInput,
) (model.LiveAgentPlanScriptReference, error) {
	key := strings.TrimSpace(input.ReferenceKey)
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.ContentText)
	goal := strings.TrimSpace(input.Goal)
	transition := strings.TrimSpace(input.Transition)
	executionMode := normalizeScriptReferenceExecutionMode(input.ExecutionMode)
	sourceQuote := strings.TrimSpace(input.SourceQuote)
	sourceType := strings.TrimSpace(input.SourceType)
	if sourceType == "" {
		sourceType = "system_agent"
	}
	sourceRef := strings.TrimSpace(input.SourceRef)

	var planExists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM live_agent_plans WHERE id=? AND tenant_id=? AND status='active'`, planID, tenantID).Scan(&planExists); errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanNotFound
	} else if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}

	existing, err := s.getLiveAgentPlanScriptReferenceByKey(ctx, tenantID, planID, key)
	if err == nil {
		if existing.Status != "disabled" {
			return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceAlreadyExists
		}
		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			return model.LiveAgentPlanScriptReference{}, txErr
		}
		defer tx.Rollback()
		current, txErr := scanLiveAgentPlanScriptReference(tx.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
			WHERE id=? AND tenant_id=? AND plan_id=? FOR UPDATE
		`, existing.ID, tenantID, planID))
		if txErr != nil {
			return model.LiveAgentPlanScriptReference{}, txErr
		}
		if current.Status != "disabled" {
			return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceAlreadyExists
		}
		nextVersion := current.VersionNo + 1
		_, txErr = tx.ExecContext(ctx, `
			UPDATE live_agent_plan_script_references
			SET title=?, content_text=?, goal=?, transition_text=?, execution_mode=?, source_quote=?,
			    source_type=?, source_ref=?, status='active', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=? AND plan_id=?
		`, title, content, goal, transition, executionMode, sourceQuote, sourceType, sourceRef,
			nextVersion, actorUserID, current.ID, tenantID, planID)
		if txErr != nil {
			return model.LiveAgentPlanScriptReference{}, txErr
		}
		_, txErr = tx.ExecContext(ctx, `
			INSERT INTO live_agent_plan_script_reference_revisions (
				tenant_id, plan_id, reference_id, version_no, action, reference_key, title, content_text,
				goal, transition_text, execution_mode, status, source_quote, source_type, source_ref, actor_user_id
			) VALUES (?, ?, ?, ?, 'readopt', ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)
		`, tenantID, planID, current.ID, nextVersion, key, title, content, goal, transition,
			executionMode, sourceQuote, sourceType, sourceRef, actorUserID)
		if txErr != nil {
			return model.LiveAgentPlanScriptReference{}, txErr
		}
		if txErr = tx.Commit(); txErr != nil {
			return model.LiveAgentPlanScriptReference{}, txErr
		}
		return scanLiveAgentPlanScriptReference(s.db.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
			WHERE id=? AND tenant_id=? AND plan_id=?
		`, current.ID, tenantID, planID))
	}
	if !errors.Is(err, ErrLiveAgentPlanScriptReferenceNotFound) {
		return model.LiveAgentPlanScriptReference{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	defer tx.Rollback()
	insertResult, err := tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_script_references (
			tenant_id, plan_id, reference_key, title, content_text, goal, transition_text,
			execution_mode, source_quote, source_type, source_ref, status, version_no,
			created_by_user_id, updated_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'active', 1, ?, ?)
	`, tenantID, planID, key, title, content, goal, transition, executionMode, sourceQuote,
		sourceType, sourceRef, actorUserID, actorUserID)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	referenceID, err := insertResult.LastInsertId()
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_script_reference_revisions (
			tenant_id, plan_id, reference_id, version_no, action, reference_key, title, content_text,
			goal, transition_text, execution_mode, status, source_quote, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, 1, 'adopt', ?, ?, ?, ?, ?, ?, 'active', ?, ?, ?, ?)
	`, tenantID, planID, referenceID, key, title, content, goal, transition, executionMode,
		sourceQuote, sourceType, sourceRef, actorUserID)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	return scanLiveAgentPlanScriptReference(s.db.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=?
	`, referenceID, tenantID, planID))
}

func (s *Store) UpdateLiveAgentPlanScriptReference(
	ctx context.Context,
	tenantID, planID, referenceID, actorUserID int64,
	input model.UpdateLiveAgentPlanScriptReferenceInput,
) (model.LiveAgentPlanScriptReference, error) {
	key := strings.TrimSpace(input.ReferenceKey)
	title := strings.TrimSpace(input.Title)
	content := strings.TrimSpace(input.ContentText)
	goal := strings.TrimSpace(input.Goal)
	transition := strings.TrimSpace(input.Transition)
	executionMode := normalizeScriptReferenceExecutionMode(input.ExecutionMode)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanScriptReference(tx.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE
	`, referenceID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceNotFound
	}
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	if input.ExpectedVersionNo > 0 && current.VersionNo != input.ExpectedVersionNo {
		return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceVersionConflict
	}
	if current.ReferenceKey != key {
		var conflictID int64
		err = tx.QueryRowContext(ctx, `
			SELECT id FROM live_agent_plan_script_references
			WHERE tenant_id=? AND plan_id=? AND reference_key=? AND id<>? LIMIT 1
		`, tenantID, planID, key, referenceID).Scan(&conflictID)
		if err == nil {
			return model.LiveAgentPlanScriptReference{}, ErrLiveAgentPlanScriptReferenceAlreadyExists
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return model.LiveAgentPlanScriptReference{}, err
		}
	}
	if current.ReferenceKey == key && current.Title == title && current.ContentText == content &&
		current.Goal == goal && current.Transition == transition && current.ExecutionMode == executionMode {
		return current, nil
	}
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_script_references
		SET reference_key=?, title=?, content_text=?, goal=?, transition_text=?, execution_mode=?,
		    source_type='system_agent_edit', source_ref='', version_no=?, updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, key, title, content, goal, transition, executionMode, nextVersion, actorUserID, referenceID, tenantID, planID)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_script_reference_revisions (
			tenant_id, plan_id, reference_id, version_no, action, reference_key, title, content_text,
			goal, transition_text, execution_mode, status, source_quote, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'update', ?, ?, ?, ?, ?, ?, 'active', ?, 'system_agent_edit', '', ?)
	`, tenantID, planID, referenceID, nextVersion, key, title, content, goal, transition,
		executionMode, current.SourceQuote, actorUserID)
	if err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.LiveAgentPlanScriptReference{}, err
	}
	return scanLiveAgentPlanScriptReference(s.db.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=?
	`, referenceID, tenantID, planID))
}

func (s *Store) DeleteLiveAgentPlanScriptReferenceWithExpectedVersion(
	ctx context.Context,
	tenantID, planID, referenceID, actorUserID, expectedVersionNo int64,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := scanLiveAgentPlanScriptReference(tx.QueryRowContext(ctx, liveAgentPlanScriptReferenceSelect+`
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active' FOR UPDATE
	`, referenceID, tenantID, planID))
	if errors.Is(err, sql.ErrNoRows) {
		return ErrLiveAgentPlanScriptReferenceNotFound
	}
	if err != nil {
		return err
	}
	if expectedVersionNo > 0 && current.VersionNo != expectedVersionNo {
		return ErrLiveAgentPlanScriptReferenceVersionConflict
	}
	nextVersion := current.VersionNo + 1
	_, err = tx.ExecContext(ctx, `
		UPDATE live_agent_plan_script_references
		SET status='disabled', source_type='system_agent_delete', source_ref='', version_no=?,
		    updated_by_user_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND plan_id=? AND status='active'
	`, nextVersion, actorUserID, referenceID, tenantID, planID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO live_agent_plan_script_reference_revisions (
			tenant_id, plan_id, reference_id, version_no, action, reference_key, title, content_text,
			goal, transition_text, execution_mode, status, source_quote, source_type, source_ref, actor_user_id
		) VALUES (?, ?, ?, ?, 'delete', ?, ?, ?, ?, ?, ?, 'disabled', ?, 'system_agent_delete', '', ?)
	`, tenantID, planID, referenceID, nextVersion, current.ReferenceKey, current.Title, current.ContentText,
		current.Goal, current.Transition, current.ExecutionMode, current.SourceQuote, actorUserID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
