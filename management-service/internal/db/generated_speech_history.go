package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"livecompanion/management/internal/model"
)

func (s *Store) RecordGeneratedSpeechHistory(ctx context.Context, input model.GeneratedSpeechHistoryInput) error {
	input.DecisionID = strings.TrimSpace(input.DecisionID)
	input.RuntimeExternalID = strings.TrimSpace(input.RuntimeExternalID)
	input.SourceType = strings.TrimSpace(input.SourceType)
	input.QuestionText = strings.TrimSpace(input.QuestionText)
	input.GeneratedText = strings.TrimSpace(input.GeneratedText)
	if input.TenantID <= 0 || input.RoomID <= 0 || input.RuntimeSessionID <= 0 || input.DecisionID == "" || input.GeneratedText == "" {
		return nil
	}
	if input.SourceType == "" {
		input.SourceType = "interrupt_answer"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO live_generated_speech_history (
			tenant_id, room_id, runtime_session_id, runtime_external_id,
			decision_id, source_type, question_text, generated_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			runtime_external_id=VALUES(runtime_external_id),
			source_type=VALUES(source_type),
			question_text=VALUES(question_text),
			generated_text=VALUES(generated_text)
	`, input.TenantID, input.RoomID, input.RuntimeSessionID, input.RuntimeExternalID,
		input.DecisionID, input.SourceType, input.QuestionText, input.GeneratedText)
	return err
}

func normalizeGeneratedSpeechHistoryPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return page, pageSize
}

func generatedSpeechHistorySourceLabelSQL() string {
	return `CASE h.source_type
		WHEN 'interrupt_quick' THEN '临时插播'
		WHEN 'interrupt_answer' THEN '场控答疑'
		ELSE h.source_type END`
}

func (s *Store) ListGeneratedSpeechHistory(
	ctx context.Context,
	tenantID, roomID, runtimeSessionID int64,
	query string,
	page, pageSize int,
) (model.GeneratedSpeechHistoryPage, error) {
	page, pageSize = normalizeGeneratedSpeechHistoryPage(page, pageSize)
	result := model.GeneratedSpeechHistoryPage{
		Items:            []model.GeneratedSpeechHistoryItem{},
		Page:             page,
		PageSize:         pageSize,
		RuntimeSessionID: runtimeSessionID,
	}
	if tenantID <= 0 || roomID <= 0 || runtimeSessionID <= 0 {
		return result, nil
	}
	query = strings.TrimSpace(query)
	like := "%" + query + "%"
	filter := ""
	args := []any{tenantID, roomID, runtimeSessionID}
	if query != "" {
		filter = ` AND (
			h.generated_text LIKE ? OR h.question_text LIKE ? OR h.decision_id LIKE ? OR ` + generatedSpeechHistorySourceLabelSQL() + ` LIKE ?
		)`
		args = append(args, like, like, like, like)
	}
	countSQL := `
		SELECT COUNT(*)
		FROM live_generated_speech_history h
		WHERE h.tenant_id=? AND h.room_id=? AND h.runtime_session_id=?` + filter
	if err := s.db.QueryRowContext(ctx, countSQL, args...).Scan(&result.Total); err != nil {
		return model.GeneratedSpeechHistoryPage{}, err
	}

	rowsArgs := append([]any{}, args...)
	rowsArgs = append(rowsArgs, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			h.id, h.tenant_id, h.room_id, h.runtime_session_id, h.runtime_external_id,
			h.decision_id, h.source_type, h.question_text, h.generated_text, h.created_at,
			(SELECT COUNT(*) FROM agent_learning_sessions a
			 WHERE a.tenant_id=h.tenant_id AND a.room_id=h.room_id
			   AND a.source_ref=CONCAT('generated_speech:', h.decision_id)),
			(SELECT COUNT(*) FROM agent_learning_sessions a
			 WHERE a.tenant_id=h.tenant_id AND a.room_id=h.room_id
			   AND a.source_ref=CONCAT('generated_speech:', h.decision_id)
			   AND a.status='adopted'),
			(SELECT COUNT(*) FROM agent_learning_sessions a
			 WHERE a.tenant_id=h.tenant_id AND a.room_id=h.room_id
			   AND a.source_ref=CONCAT('generated_speech:', h.decision_id)
			   AND a.status='editing')
		FROM live_generated_speech_history h
		WHERE h.tenant_id=? AND h.room_id=? AND h.runtime_session_id=?`+filter+`
		ORDER BY h.created_at DESC, h.id DESC
		LIMIT ? OFFSET ?
	`, rowsArgs...)
	if err != nil {
		return model.GeneratedSpeechHistoryPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item model.GeneratedSpeechHistoryItem
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.RoomID, &item.RuntimeSessionID, &item.RuntimeExternalID,
			&item.DecisionID, &item.SourceType, &item.QuestionText, &item.GeneratedText, &item.CreatedAt,
			&item.CorrectionCount, &item.AdoptedCorrectionCount, &item.EditingCorrectionCount,
		); err != nil {
			return model.GeneratedSpeechHistoryPage{}, err
		}
		switch {
		case item.AdoptedCorrectionCount > 0:
			item.CorrectionStatus = "adopted"
		case item.EditingCorrectionCount > 0:
			item.CorrectionStatus = "editing"
		case item.CorrectionCount > 0:
			item.CorrectionStatus = "corrected"
		default:
			item.CorrectionStatus = "none"
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return model.GeneratedSpeechHistoryPage{}, err
	}
	return result, nil
}

func (s *Store) GetGeneratedSpeechHistorySession(
	ctx context.Context,
	tenantID, roomID, requestedSessionID int64,
) (model.LiveRuntimeSession, error) {
	if requestedSessionID > 0 {
		session, err := s.GetLiveRuntimeSession(ctx, tenantID, requestedSessionID)
		if err != nil {
			return model.LiveRuntimeSession{}, err
		}
		if session.RoomID != roomID {
			return model.LiveRuntimeSession{}, sql.ErrNoRows
		}
		return session, nil
	}
	session, err := s.GetLiveRuntimeByRoom(ctx, tenantID, roomID)
	if err != nil {
		return model.LiveRuntimeSession{}, fmt.Errorf("resolve runtime session: %w", err)
	}
	return session, nil
}
