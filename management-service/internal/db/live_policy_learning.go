package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"livecompanion/management/internal/model"
)

func scanLivePolicyLearningCandidate(scanner interface{ Scan(...any) error }) (model.LivePolicyLearningCandidate, error) {
	var item model.LivePolicyLearningCandidate
	var tenantID, roomID, adoptedVersionID, reviewedBy sql.NullInt64
	var reviewedAt sql.NullTime
	var historyRaw string
	var observedReply, learningMeta sql.NullString
	err := scanner.Scan(
		&item.ID,
		&item.EvidenceType,
		&item.SourceRef,
		&item.SourceLayer,
		&item.IndustryCode,
		&tenantID,
		&roomID,
		&item.Question,
		&observedReply,
		&item.FinalReply,
		&item.Feedback,
		&historyRaw,
		&item.RecommendedLayer,
		&item.RecommendationReason,
		&item.AbsorbRecommended,
		&item.Confidence,
		&item.RuleTitle,
		&item.RuleText,
		&item.ExecutionMode,
		&item.Status,
		&item.ModelProvider,
		&item.Model,
		&item.LatencyMS,
		&learningMeta,
		&adoptedVersionID,
		&item.CreatedByUserID,
		&reviewedBy,
		&item.ReviewNote,
		&item.CreatedAt,
		&item.UpdatedAt,
		&reviewedAt,
	)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	if observedReply.Valid {
		item.ObservedReply = observedReply.String
	}
	item.LearningMeta = map[string]any{}
	if learningMeta.Valid && strings.TrimSpace(learningMeta.String) != "" {
		_ = json.Unmarshal([]byte(learningMeta.String), &item.LearningMeta)
	}
	item.History = []model.LivePolicyLearningHistoryItem{}
	_ = json.Unmarshal([]byte(historyRaw), &item.History)
	if item.History == nil {
		item.History = []model.LivePolicyLearningHistoryItem{}
	}
	if tenantID.Valid {
		value := tenantID.Int64
		item.TenantID = &value
	}
	if roomID.Valid {
		value := roomID.Int64
		item.RoomID = &value
	}
	if adoptedVersionID.Valid {
		value := adoptedVersionID.Int64
		item.AdoptedVersionID = &value
	}
	if reviewedBy.Valid {
		value := reviewedBy.Int64
		item.ReviewedByUserID = &value
	}
	if reviewedAt.Valid {
		value := reviewedAt.Time
		item.ReviewedAt = &value
	}
	return item, nil
}

const livePolicyLearningCandidateColumns = `
	id, evidence_type, source_ref, source_layer, industry_code, tenant_id, room_id,
	question, observed_reply, final_reply, feedback, history_json,
	recommended_layer, recommendation_reason, absorb_recommended, confidence,
	rule_title, rule_text, execution_mode, status,
	model_provider, model_name, latency_ms, learning_meta_json, adopted_version_id,
	created_by_user_id, reviewed_by_user_id, review_note,
	created_at, updated_at, reviewed_at
`

func (s *Store) CreateLivePolicyLearningCandidate(
	ctx context.Context,
	actorUserID int64,
	tenantID *int64,
	input model.CreateLivePolicyLearningCandidateInput,
	recommendedLayer, reason, ruleTitle, ruleText, executionMode, modelProvider, modelName string,
	absorbRecommended bool,
	confidence int,
	latencyMS int64,
	learningMeta map[string]any,
) (model.LivePolicyLearningCandidate, error) {
	history := input.History
	if history == nil {
		history = []model.LivePolicyLearningHistoryItem{}
	}
	historyRaw, err := json.Marshal(history)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	if learningMeta == nil {
		learningMeta = map[string]any{}
	}
	learningMetaRaw, err := json.Marshal(learningMeta)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	var tenantValue any
	if tenantID != nil && *tenantID > 0 {
		tenantValue = *tenantID
	}
	var roomValue any
	if input.RoomID > 0 {
		roomValue = input.RoomID
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO live_policy_learning_candidates (
			evidence_type, source_ref, source_layer, industry_code, tenant_id, room_id,
			question, observed_reply, final_reply, feedback, history_json,
			recommended_layer, recommendation_reason, absorb_recommended, confidence,
			rule_title, rule_text, execution_mode, status,
			model_provider, model_name, latency_ms, learning_meta_json, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?)
	`,
		input.EvidenceType,
		input.SourceRef,
		input.SourceLayer,
		input.IndustryCode,
		tenantValue,
		roomValue,
		input.Question,
		input.ObservedReply,
		input.FinalReply,
		input.Feedback,
		string(historyRaw),
		recommendedLayer,
		reason,
		absorbRecommended,
		confidence,
		ruleTitle,
		ruleText,
		executionMode,
		modelProvider,
		modelName,
		latencyMS,
		string(learningMetaRaw),
		actorUserID,
	)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	return s.GetLivePolicyLearningCandidate(ctx, id)
}

func (s *Store) GetLivePolicyLearningCandidate(
	ctx context.Context,
	id int64,
) (model.LivePolicyLearningCandidate, error) {
	return scanLivePolicyLearningCandidate(s.db.QueryRowContext(
		ctx,
		"SELECT "+livePolicyLearningCandidateColumns+" FROM live_policy_learning_candidates WHERE id=?",
		id,
	))
}

func (s *Store) ListLivePolicyLearningCandidates(
	ctx context.Context,
	status string,
	limit int,
) ([]model.LivePolicyLearningCandidate, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	query := "SELECT " + livePolicyLearningCandidateColumns + " FROM live_policy_learning_candidates"
	args := []any{}
	if status != "" && status != "all" {
		query += " WHERE status=?"
		args = append(args, status)
	}
	query += " ORDER BY CASE status WHEN 'pending' THEN 0 ELSE 1 END, id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.LivePolicyLearningCandidate, 0)
	for rows.Next() {
		item, err := scanLivePolicyLearningCandidate(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) MarkLivePolicyLearningCandidateAdopted(
	ctx context.Context,
	id, reviewerUserID, versionID int64,
	targetLayer, reviewNote string,
) (model.LivePolicyLearningCandidate, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_policy_learning_candidates
		SET status='adopted',
			recommended_layer=?,
			adopted_version_id=?,
			reviewed_by_user_id=?,
			review_note=?,
			reviewed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status='pending'
	`, targetLayer, versionID, reviewerUserID, strings.TrimSpace(reviewNote), id)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	if affected == 0 {
		return model.LivePolicyLearningCandidate{}, errors.New("learning candidate is no longer pending")
	}
	return s.GetLivePolicyLearningCandidate(ctx, id)
}

func (s *Store) RejectLivePolicyLearningCandidate(
	ctx context.Context,
	id, reviewerUserID int64,
	reviewNote string,
) (model.LivePolicyLearningCandidate, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE live_policy_learning_candidates
		SET status='rejected',
			reviewed_by_user_id=?,
			review_note=?,
			reviewed_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status='pending'
	`, reviewerUserID, strings.TrimSpace(reviewNote), id)
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.LivePolicyLearningCandidate{}, err
	}
	if affected == 0 {
		return model.LivePolicyLearningCandidate{}, errors.New("learning candidate is no longer pending")
	}
	return s.GetLivePolicyLearningCandidate(ctx, id)
}
