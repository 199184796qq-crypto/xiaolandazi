package db

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
			decision_id, source_type, question_text, generated_text, intent_json, suggestion_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			runtime_external_id=VALUES(runtime_external_id),
			source_type=VALUES(source_type),
			question_text=VALUES(question_text),
			generated_text=VALUES(generated_text),
			intent_json=VALUES(intent_json),
			suggestion_json=VALUES(suggestion_json)
	`, input.TenantID, input.RoomID, input.RuntimeSessionID, input.RuntimeExternalID,
		input.DecisionID, input.SourceType, input.QuestionText, input.GeneratedText, input.IntentJSON, input.SuggestionJSON)
	return err
}

func (s *Store) ensureGeneratedSpeechHistoryColumns(ctx context.Context) error {
	for _, column := range []struct{ name, ddl string }{
		{"intent_json", "ALTER TABLE live_generated_speech_history ADD COLUMN intent_json MEDIUMTEXT NULL"},
		{"suggestion_json", "ALTER TABLE live_generated_speech_history ADD COLUMN suggestion_json MEDIUMTEXT NULL"},
	} {
		var count int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='live_generated_speech_history' AND column_name=?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := s.db.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
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
		WHEN 'interrupt_failed' THEN '未完成播报'
		ELSE h.source_type END`
}

const playedAnswerFilter = ` AND EXISTS (
	SELECT 1 FROM live_session_speech_assets a
	WHERE a.tenant_id=h.tenant_id AND a.room_id=h.room_id AND a.session_id=h.runtime_session_id
	  AND a.source_ref=CONCAT('reply:',h.decision_id) AND a.kind='interaction'
	  AND a.played_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR
)`

type LiveRecord struct {
	SessionID       int64      `json:"session_id"`
	RoomID          int64      `json:"room_id"`
	RoomName        string     `json:"room_name"`
	PlanName        string     `json:"plan_name"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationSeconds int64      `json:"duration_seconds"`
	BilledSeconds   uint64     `json:"billed_seconds"`
	AnswerCount     int        `json:"answer_count"`
	AnswersExpired  bool       `json:"answers_expired"`
}

func (s *Store) ListLiveRecords(ctx context.Context, tenantID, roomID int64, realm string, settledUsageOnly bool, limit, offset int) ([]LiveRecord, error) {
	filter := ""
	args := []any{tenantID, model.NormalizeExecutionRealm(realm)}
	if roomID > 0 {
		filter = " AND s.room_id=?"
		args = append(args, roomID)
	}
	if settledUsageOnly {
		filter += " AND s.ended_at IS NOT NULL AND s.total_billed_seconds>0"
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.room_id, room.name,
		       COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(start_event.detail_json,'$.plan_name')),''), plan.name, ''),
		       s.started_at, s.ended_at,
		       GREATEST(0,TIMESTAMPDIFF(SECOND,s.started_at,COALESCE(s.ended_at,UTC_TIMESTAMP(3)))),
		       s.total_billed_seconds,
		       s.ended_at IS NOT NULL AND s.ended_at<=UTC_TIMESTAMP(3)-INTERVAL 24 HOUR,
		       (SELECT COUNT(*) FROM live_generated_speech_history h
		        WHERE h.tenant_id=s.tenant_id AND h.room_id=s.room_id AND h.runtime_session_id=s.id
		          AND h.created_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR`+playedAnswerFilter+`)
		FROM live_runtime_sessions s
		JOIN core_rooms room ON room.tenant_id=s.tenant_id AND room.id=s.room_id
		LEFT JOIN live_runtime_events start_event ON start_event.id=(
			SELECT e.id FROM live_runtime_events e WHERE e.session_id=s.id AND e.tenant_id=s.tenant_id
			  AND e.room_id=s.room_id AND e.event_code='AI_RUNTIME_STARTED' ORDER BY e.id LIMIT 1
		)
		LEFT JOIN live_agent_room_plan_selections selection ON selection.tenant_id=s.tenant_id
		  AND selection.room_id=s.room_id AND selection.selected_at<=s.started_at
		LEFT JOIN live_agent_plans plan ON plan.tenant_id=s.tenant_id AND plan.id=selection.plan_id
		WHERE s.tenant_id=? AND s.execution_realm=?`+filter+`
		  AND NOT EXISTS (SELECT 1 FROM mgmt_room_deletions d WHERE d.tenant_id=s.tenant_id AND d.room_id=s.room_id)
		ORDER BY s.started_at DESC,s.id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []LiveRecord{}
	for rows.Next() {
		var item LiveRecord
		if err := rows.Scan(&item.SessionID, &item.RoomID, &item.RoomName, &item.PlanName, &item.StartedAt,
			&item.EndedAt, &item.DurationSeconds, &item.BilledSeconds, &item.AnswersExpired, &item.AnswerCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListGeneratedSpeechHistory(
	ctx context.Context,
	tenantID, roomID, runtimeSessionID int64,
	query string,
	page, pageSize int,
	playedOnly bool,
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
	filter := " AND h.created_at>UTC_TIMESTAMP(3)-INTERVAL 24 HOUR"
	if playedOnly {
		filter += playedAnswerFilter
	}
	args := []any{tenantID, roomID, runtimeSessionID}
	if query != "" {
		filter += ` AND (
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
			h.decision_id, h.source_type, h.question_text, h.generated_text,
			COALESCE(h.intent_json,''), COALESCE(h.suggestion_json,''), h.created_at,
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
			&item.DecisionID, &item.SourceType, &item.QuestionText, &item.GeneratedText,
			&item.IntentJSON, &item.SuggestionJSON, &item.CreatedAt,
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
