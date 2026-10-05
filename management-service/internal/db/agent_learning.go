package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"livecompanion/management/internal/agentmemory"
	"livecompanion/management/internal/model"
)

func normalizeAgentMemoryKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Join(strings.Fields(value), "")
	if value == "" {
		return "default"
	}
	runes := []rune(value)
	if len(runes) > 240 {
		runes = runes[:240]
	}
	return string(runes)
}

func scanAgentLearningSession(scanner interface{ Scan(...any) error }) (model.AgentLearningSession, error) {
	var item model.AgentLearningSession
	var adoptedMemoryItemID sql.NullInt64
	var adoptedAt sql.NullTime
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.RoomID, &item.SourceType, &item.SourceRef,
		&item.Question, &item.OriginalReply, &item.Target, &item.Status, &item.MemoryType,
		&adoptedMemoryItemID, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt, &adoptedAt,
	)
	if err != nil {
		return model.AgentLearningSession{}, err
	}
	if adoptedMemoryItemID.Valid {
		value := adoptedMemoryItemID.Int64
		item.AdoptedMemoryItemID = &value
	}
	if adoptedAt.Valid {
		value := adoptedAt.Time
		item.AdoptedAt = &value
	}
	return item, nil
}

const agentLearningSessionColumns = `
	id, tenant_id, room_id, source_type, source_ref, question, original_reply,
	target, status, memory_type, adopted_memory_item_id, created_by_user_id,
	created_at, updated_at, adopted_at
`

func (s *Store) CreateAgentLearningSession(
	ctx context.Context,
	actorUserID, tenantID, roomID int64,
	input model.CreateAgentLearningSessionInput,
) (model.AgentLearningSession, error) {
	input.SourceType = strings.TrimSpace(input.SourceType)
	if input.SourceType == "" {
		input.SourceType = "direct_correction"
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_learning_sessions (
			tenant_id, room_id, source_type, source_ref, question, original_reply,
			target, status, memory_type, created_by_user_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'editing', '', ?)
	`,
		tenantID, roomID, input.SourceType, strings.TrimSpace(input.SourceRef),
		strings.TrimSpace(input.Question), strings.TrimSpace(input.OriginalReply),
		strings.TrimSpace(input.Target), actorUserID,
	)
	if err != nil {
		return model.AgentLearningSession{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return model.AgentLearningSession{}, err
	}
	return s.GetAgentLearningSession(ctx, tenantID, roomID, id)
}

func (s *Store) GetAgentLearningSession(
	ctx context.Context,
	tenantID, roomID, sessionID int64,
) (model.AgentLearningSession, error) {
	return scanAgentLearningSession(s.db.QueryRowContext(ctx, `
		SELECT `+agentLearningSessionColumns+`
		FROM agent_learning_sessions
		WHERE id=? AND tenant_id=? AND room_id=?
	`, sessionID, tenantID, roomID))
}

func (s *Store) ListAgentLearningSessions(
	ctx context.Context,
	tenantID, roomID int64,
	limit int,
) ([]model.AgentLearningSession, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+agentLearningSessionColumns+`
		FROM agent_learning_sessions
		WHERE tenant_id=? AND room_id=?
		ORDER BY updated_at DESC, id DESC
		LIMIT ?
	`, tenantID, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentLearningSession, 0)
	for rows.Next() {
		item, err := scanAgentLearningSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAgentLearningResult(scanner interface{ Scan(...any) error }) (model.AgentLearningResult, error) {
	var item model.AgentLearningResult
	var matchedMemoryItemID sql.NullInt64
	var structuredRaw []byte
	err := scanner.Scan(
		&item.ID, &item.SessionID, &item.EvidenceID, &item.TurnNo, &item.MemoryType,
		&item.Target, &item.MemoryKey, &matchedMemoryItemID, &item.ResultText, &structuredRaw,
		&item.ModelProvider, &item.ModelName, &item.LatencyMS, &item.CreatedAt,
	)
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	if matchedMemoryItemID.Valid {
		value := matchedMemoryItemID.Int64
		item.MatchedMemoryItemID = &value
	}
	item.Structured = map[string]any{}
	if len(structuredRaw) > 0 {
		_ = json.Unmarshal(structuredRaw, &item.Structured)
	}
	return item, nil
}

const agentLearningResultColumns = `
	id, session_id, evidence_id, turn_no, memory_type, target, memory_key,
	matched_memory_item_id, result_text, structured_json, model_provider, model_name, latency_ms, created_at
`

func (s *Store) ListAgentLearningTimeline(
	ctx context.Context,
	sessionID int64,
) ([]model.AgentLearningTimelineItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			e.id, e.session_id, e.turn_no, e.feedback, e.created_at,
			r.id, r.session_id, r.evidence_id, r.turn_no, r.memory_type, r.target,
			r.memory_key, r.matched_memory_item_id, r.result_text, r.structured_json, r.model_provider,
			r.model_name, r.latency_ms, r.created_at
		FROM agent_learning_evidence e
		JOIN agent_learning_results r ON r.evidence_id=e.id
		WHERE e.session_id=?
		ORDER BY e.turn_no ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentLearningTimelineItem, 0)
	for rows.Next() {
		var evidence model.AgentLearningEvidence
		var result model.AgentLearningResult
		var matchedMemoryItemID sql.NullInt64
		var structuredRaw []byte
		if err := rows.Scan(
			&evidence.ID, &evidence.SessionID, &evidence.TurnNo, &evidence.Feedback, &evidence.CreatedAt,
			&result.ID, &result.SessionID, &result.EvidenceID, &result.TurnNo,
			&result.MemoryType, &result.Target, &result.MemoryKey, &matchedMemoryItemID, &result.ResultText,
			&structuredRaw, &result.ModelProvider, &result.ModelName, &result.LatencyMS, &result.CreatedAt,
		); err != nil {
			return nil, err
		}
		result.Structured = map[string]any{}
		if matchedMemoryItemID.Valid {
			value := matchedMemoryItemID.Int64
			result.MatchedMemoryItemID = &value
		}
		if len(structuredRaw) > 0 {
			_ = json.Unmarshal(structuredRaw, &result.Structured)
		}
		items = append(items, model.AgentLearningTimelineItem{Evidence: evidence, Result: result})
	}
	return items, rows.Err()
}

func (s *Store) GetLatestAgentLearningResult(ctx context.Context, sessionID int64) (model.AgentLearningResult, error) {
	return scanAgentLearningResult(s.db.QueryRowContext(ctx, `
		SELECT `+agentLearningResultColumns+`
		FROM agent_learning_results
		WHERE session_id=?
		ORDER BY turn_no DESC
		LIMIT 1
	`, sessionID))
}

func (s *Store) GetAgentLearningResult(ctx context.Context, resultID int64) (model.AgentLearningResult, error) {
	return scanAgentLearningResult(s.db.QueryRowContext(ctx, `
		SELECT `+agentLearningResultColumns+`
		FROM agent_learning_results
		WHERE id=?
	`, resultID))
}

func (s *Store) AppendAgentLearningTurn(
	ctx context.Context,
	session model.AgentLearningSession,
	feedback string,
	result model.AgentLearningResult,
) (model.AgentLearningResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	defer tx.Rollback()

	var nextTurn uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(turn_no), 0) + 1
		FROM agent_learning_evidence
		WHERE session_id=?
	`, session.ID).Scan(&nextTurn); err != nil {
		return model.AgentLearningResult{}, err
	}
	evidenceResult, err := tx.ExecContext(ctx, `
		INSERT INTO agent_learning_evidence (session_id, turn_no, feedback)
		VALUES (?, ?, ?)
	`, session.ID, nextTurn, strings.TrimSpace(feedback))
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	evidenceID, err := evidenceResult.LastInsertId()
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	structuredRaw, err := json.Marshal(result.Structured)
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	memoryKey := normalizeAgentMemoryKey(result.MemoryKey)
	inserted, err := tx.ExecContext(ctx, `
		INSERT INTO agent_learning_results (
			session_id, evidence_id, turn_no, memory_type, target, memory_key,
			matched_memory_item_id, result_text, structured_json, model_provider, model_name, latency_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		session.ID, evidenceID, nextTurn, strings.TrimSpace(result.MemoryType),
		strings.TrimSpace(result.Target), memoryKey, result.MatchedMemoryItemID, strings.TrimSpace(result.ResultText),
		string(structuredRaw), strings.TrimSpace(result.ModelProvider),
		strings.TrimSpace(result.ModelName), result.LatencyMS,
	)
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	resultID, err := inserted.LastInsertId()
	if err != nil {
		return model.AgentLearningResult{}, err
	}
	result.ID = resultID
	result.SessionID = session.ID
	if err := recordAgentMemoryEvidenceObservationTx(ctx, tx, session, resultID, result, feedback); err != nil {
		return model.AgentLearningResult{}, fmt.Errorf("record agent memory evidence observation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_learning_sessions
		SET target=?, memory_type=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND status='editing'
	`, strings.TrimSpace(result.Target), strings.TrimSpace(result.MemoryType), session.ID); err != nil {
		return model.AgentLearningResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.AgentLearningResult{}, err
	}
	return s.GetAgentLearningResult(ctx, resultID)
}

func scanAgentMemoryVersion(scanner interface{ Scan(...any) error }) (model.AgentMemoryVersion, error) {
	var item model.AgentMemoryVersion
	var structuredRaw []byte
	err := scanner.Scan(
		&item.ID, &item.MemoryItemID, &item.VersionNo, &item.Status, &item.ContentText,
		&structuredRaw, &item.SourceSessionID, &item.SourceResultID, &item.CreatedByUserID, &item.CreatedAt,
	)
	if err != nil {
		return model.AgentMemoryVersion{}, err
	}
	item.Structured = map[string]any{}
	if len(structuredRaw) > 0 {
		_ = json.Unmarshal(structuredRaw, &item.Structured)
	}
	return item, nil
}

const agentMemoryVersionColumns = `
	id, memory_item_id, version_no, status, content_text, structured_json,
	source_session_id, source_result_id, created_by_user_id, created_at
`

func (s *Store) scanAgentMemoryItem(scanner interface{ Scan(...any) error }) (model.AgentMemoryItem, error) {
	var item model.AgentMemoryItem
	var currentVersionID sql.NullInt64
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.RoomID, &item.MemoryType, &item.MemoryKey,
		&item.Target, &item.Status, &currentVersionID, &item.CreatedByUserID,
		&item.CreatedAt, &item.UpdatedAt,
	)
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	if currentVersionID.Valid {
		value := currentVersionID.Int64
		item.CurrentVersionID = &value
	}
	return item, nil
}

const agentMemoryItemColumns = `
	id, tenant_id, room_id, memory_type, memory_key, target, status,
	current_version_id, created_by_user_id, created_at, updated_at
`

func (s *Store) GetAgentMemoryItem(
	ctx context.Context,
	tenantID, roomID, memoryItemID int64,
) (model.AgentMemoryItem, error) {
	item, err := s.scanAgentMemoryItem(s.db.QueryRowContext(ctx, `
		SELECT `+agentMemoryItemColumns+`
		FROM agent_memory_items
		WHERE id=? AND tenant_id=? AND room_id=?
	`, memoryItemID, tenantID, roomID))
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	if item.CurrentVersionID != nil {
		version, versionErr := scanAgentMemoryVersion(s.db.QueryRowContext(ctx, `
			SELECT `+agentMemoryVersionColumns+`
			FROM agent_memory_versions
			WHERE id=? AND memory_item_id=?
		`, *item.CurrentVersionID, item.ID))
		if versionErr == nil {
			item.CurrentVersion = &version
		}
	}
	return item, nil
}

func (s *Store) ListActiveAgentMemories(
	ctx context.Context,
	tenantID, roomID int64,
) ([]model.AgentMemoryItem, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			i.id, i.tenant_id, i.room_id, i.memory_type, i.memory_key, i.target, i.status,
			i.current_version_id, i.created_by_user_id, i.created_at, i.updated_at,
			v.id, v.memory_item_id, v.version_no, v.status, v.content_text, v.structured_json,
			v.source_session_id, v.source_result_id, v.created_by_user_id, v.created_at
		FROM agent_memory_items i
		JOIN agent_memory_versions v ON v.id=i.current_version_id
		WHERE i.tenant_id=? AND i.room_id=? AND i.status='active'
		ORDER BY FIELD(i.memory_type, 'semantic', 'fact', 'wording', 'style'), i.updated_at DESC, i.id DESC
	`, tenantID, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentMemoryItem, 0)
	for rows.Next() {
		var item model.AgentMemoryItem
		var currentVersionID sql.NullInt64
		var version model.AgentMemoryVersion
		var structuredRaw []byte
		if err := rows.Scan(
			&item.ID, &item.TenantID, &item.RoomID, &item.MemoryType, &item.MemoryKey,
			&item.Target, &item.Status, &currentVersionID, &item.CreatedByUserID, &item.CreatedAt, &item.UpdatedAt,
			&version.ID, &version.MemoryItemID, &version.VersionNo, &version.Status, &version.ContentText,
			&structuredRaw, &version.SourceSessionID, &version.SourceResultID,
			&version.CreatedByUserID, &version.CreatedAt,
		); err != nil {
			return nil, err
		}
		if currentVersionID.Valid {
			value := currentVersionID.Int64
			item.CurrentVersionID = &value
		}
		version.Structured = map[string]any{}
		if len(structuredRaw) > 0 {
			_ = json.Unmarshal(structuredRaw, &version.Structured)
		}
		item.CurrentVersion = &version
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ListAgentMemoryVersions(
	ctx context.Context,
	tenantID, roomID, memoryItemID int64,
) ([]model.AgentMemoryVersion, error) {
	if _, err := s.GetAgentMemoryItem(ctx, tenantID, roomID, memoryItemID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+agentMemoryVersionColumns+`
		FROM agent_memory_versions
		WHERE memory_item_id=?
		ORDER BY version_no DESC
	`, memoryItemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentMemoryVersion, 0)
	for rows.Next() {
		item, err := scanAgentMemoryVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanAgentMemoryEvidenceStat(scanner interface{ Scan(...any) error }) (model.AgentMemoryEvidenceStat, error) {
	var item model.AgentMemoryEvidenceStat
	var lastSessionID sql.NullInt64
	var lastResultID sql.NullInt64
	var lastAdoptedAt sql.NullTime
	err := scanner.Scan(
		&item.ID, &item.TenantID, &item.RoomID, &item.MemoryType, &item.MemoryKey,
		&item.ValueSignature, &item.ValueText, &item.OccurrenceCount, &item.ConsecutiveCount,
		&item.ExplicitCorrectionCount, &item.AdoptedCount, &lastSessionID, &lastResultID,
		&item.FirstSeenAt, &item.LastSeenAt, &lastAdoptedAt,
	)
	if err != nil {
		return model.AgentMemoryEvidenceStat{}, err
	}
	if lastSessionID.Valid {
		item.LastSessionID = lastSessionID.Int64
	}
	if lastResultID.Valid {
		item.LastResultID = lastResultID.Int64
	}
	if lastAdoptedAt.Valid {
		value := lastAdoptedAt.Time
		item.LastAdoptedAt = &value
	}
	return item, nil
}

const agentMemoryEvidenceStatColumns = `
	id, tenant_id, room_id, memory_type, memory_key, value_signature, value_text,
	occurrence_count, consecutive_count, explicit_correction_count, adopted_count,
	last_session_id, last_result_id, first_seen_at, last_seen_at, last_adopted_at
`

func (s *Store) ListAgentMemoryEvidenceStats(
	ctx context.Context,
	tenantID, roomID int64,
	memoryType, memoryKey string,
	limit int,
) ([]model.AgentMemoryEvidenceStat, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+agentMemoryEvidenceStatColumns+`
		FROM agent_memory_evidence_stats
		WHERE tenant_id=? AND room_id=? AND memory_type=? AND memory_key=?
		ORDER BY adopted_count DESC, explicit_correction_count DESC,
			consecutive_count DESC, occurrence_count DESC, last_seen_at DESC, id DESC
		LIMIT ?
	`, tenantID, roomID, strings.TrimSpace(memoryType), normalizeAgentMemoryKey(memoryKey), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]model.AgentMemoryEvidenceStat, 0)
	for rows.Next() {
		item, scanErr := scanAgentMemoryEvidenceStat(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func recordAgentMemoryEvidenceObservationTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.AgentLearningSession,
	resultID int64,
	result model.AgentLearningResult,
	feedback string,
) error {
	memoryType := strings.TrimSpace(result.MemoryType)
	memoryKey := normalizeAgentMemoryKey(result.MemoryKey)
	valueText := agentmemory.ObservedEvidenceValue(memoryType, result.ResultText, result.Structured)
	valueSignature := agentmemory.EvidenceSignature(valueText)
	if memoryType == "" || memoryKey == "" || valueText == "" || valueSignature == "" {
		return nil
	}

	var previousSignature string
	previousErr := tx.QueryRowContext(ctx, `
		SELECT value_signature
		FROM agent_memory_evidence_stats
		WHERE tenant_id=? AND room_id=? AND memory_type=? AND memory_key=?
		ORDER BY last_seen_at DESC, id DESC
		LIMIT 1
		FOR UPDATE
	`, session.TenantID, session.RoomID, memoryType, memoryKey).Scan(&previousSignature)
	if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
		return previousErr
	}
	sameAsPrevious := previousErr == nil && previousSignature == valueSignature
	if !sameAsPrevious {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_memory_evidence_stats
			SET consecutive_count=0
			WHERE tenant_id=? AND room_id=? AND memory_type=? AND memory_key=?
				AND value_signature<>? AND consecutive_count<>0
		`, session.TenantID, session.RoomID, memoryType, memoryKey, valueSignature); err != nil {
			return err
		}
	}

	explicitCorrection := uint64(0)
	if agentmemory.LooksLikeExplicitCorrection(feedback) {
		explicitCorrection = 1
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO agent_memory_evidence_stats (
			tenant_id, room_id, memory_type, memory_key, value_signature, value_text,
			occurrence_count, consecutive_count, explicit_correction_count, adopted_count,
			last_session_id, last_result_id, first_seen_at, last_seen_at
		) VALUES (?, ?, ?, ?, ?, ?, 1, 1, ?, 0, ?, ?, CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3))
		ON DUPLICATE KEY UPDATE
			value_text=VALUES(value_text),
			occurrence_count=occurrence_count+1,
			consecutive_count=IF(?, consecutive_count+1, 1),
			explicit_correction_count=explicit_correction_count+VALUES(explicit_correction_count),
			last_session_id=VALUES(last_session_id),
			last_result_id=VALUES(last_result_id),
			last_seen_at=CURRENT_TIMESTAMP(3)
	`,
		session.TenantID, session.RoomID, memoryType, memoryKey, valueSignature, valueText,
		explicitCorrection, session.ID, resultID, sameAsPrevious,
	)
	return err
}

func recordAgentMemoryEvidenceAdoptionTx(
	ctx context.Context,
	tx *sql.Tx,
	session model.AgentLearningSession,
	result model.AgentLearningResult,
) error {
	memoryType := strings.TrimSpace(result.MemoryType)
	memoryKey := normalizeAgentMemoryKey(result.MemoryKey)
	valueText := agentmemory.EvidenceValue(memoryType, result.ResultText, result.Structured)
	valueSignature := agentmemory.EvidenceSignature(valueText)
	if memoryType == "" || memoryKey == "" || valueText == "" || valueSignature == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO agent_memory_evidence_stats (
			tenant_id, room_id, memory_type, memory_key, value_signature, value_text,
			occurrence_count, consecutive_count, explicit_correction_count, adopted_count,
			last_session_id, last_result_id, first_seen_at, last_seen_at, last_adopted_at
		) VALUES (?, ?, ?, ?, ?, ?, 0, 0, 0, 1, ?, ?, CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3))
		ON DUPLICATE KEY UPDATE
			value_text=VALUES(value_text),
			adopted_count=adopted_count+1,
			last_session_id=VALUES(last_session_id),
			last_result_id=VALUES(last_result_id),
			last_adopted_at=CURRENT_TIMESTAMP(3)
	`, session.TenantID, session.RoomID, memoryType, memoryKey, valueSignature, valueText, session.ID, result.ID)
	return err
}

func sourceAgentMemoryID(sourceRef string) (int64, bool) {
	sourceRef = strings.TrimSpace(sourceRef)
	if !strings.HasPrefix(sourceRef, "agent_memory:") {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(sourceRef, "agent_memory:"), 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func (s *Store) AdoptAgentLearningSession(
	ctx context.Context,
	tenantID, roomID, sessionID, actorUserID int64,
) (model.AdoptAgentLearningOutput, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	defer tx.Rollback()

	session, err := scanAgentLearningSession(tx.QueryRowContext(ctx, `
		SELECT `+agentLearningSessionColumns+`
		FROM agent_learning_sessions
		WHERE id=? AND tenant_id=? AND room_id=?
		FOR UPDATE
	`, sessionID, tenantID, roomID))
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if session.Status == model.AgentLearningStatusAdopted && session.AdoptedMemoryItemID != nil {
		if err := tx.Commit(); err != nil {
			return model.AdoptAgentLearningOutput{}, err
		}
		memory, memoryErr := s.GetAgentMemoryItem(ctx, tenantID, roomID, *session.AdoptedMemoryItemID)
		if memoryErr != nil {
			return model.AdoptAgentLearningOutput{}, memoryErr
		}
		if memory.CurrentVersion == nil {
			return model.AdoptAgentLearningOutput{}, errors.New("adopted memory has no current version")
		}
		return model.AdoptAgentLearningOutput{Session: session, Memory: memory, Version: *memory.CurrentVersion}, nil
	}
	if session.Status != model.AgentLearningStatusEditing {
		return model.AdoptAgentLearningOutput{}, fmt.Errorf("learning session is not editable")
	}

	latest, err := scanAgentLearningResult(tx.QueryRowContext(ctx, `
		SELECT `+agentLearningResultColumns+`
		FROM agent_learning_results
		WHERE session_id=?
		ORDER BY turn_no DESC
		LIMIT 1
		FOR UPDATE
	`, session.ID))
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	structuredRaw, err := json.Marshal(latest.Structured)
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	memoryKey := normalizeAgentMemoryKey(latest.MemoryKey)

	var reclassifiedFromMemoryID int64
	if sourceMemoryID, ok := sourceAgentMemoryID(session.SourceRef); ok {
		var sourceMemoryType string
		if sourceErr := tx.QueryRowContext(ctx, `
			SELECT memory_type
			FROM agent_memory_items
			WHERE id=? AND tenant_id=? AND room_id=?
			FOR UPDATE
		`, sourceMemoryID, tenantID, roomID).Scan(&sourceMemoryType); sourceErr == nil && strings.TrimSpace(sourceMemoryType) != strings.TrimSpace(latest.MemoryType) {
			reclassifiedFromMemoryID = sourceMemoryID
			if latest.Structured == nil {
				latest.Structured = map[string]any{}
			}
			autoReview, _ := latest.Structured["auto_review"].(map[string]any)
			if autoReview == nil {
				autoReview = map[string]any{}
			}
			autoReview["reclassified_from_memory_id"] = sourceMemoryID
			autoReview["reclassified_from_type"] = strings.TrimSpace(sourceMemoryType)
			autoReview["reclassified_to_type"] = strings.TrimSpace(latest.MemoryType)
			latest.Structured["auto_review"] = autoReview
			structuredRaw, err = json.Marshal(latest.Structured)
			if err != nil {
				return model.AdoptAgentLearningOutput{}, err
			}
		}
	}

	var memoryItemID int64
	var currentVersionID sql.NullInt64
	if latest.MatchedMemoryItemID != nil && *latest.MatchedMemoryItemID > 0 {
		err = tx.QueryRowContext(ctx, `
			SELECT id, current_version_id
			FROM agent_memory_items
			WHERE id=? AND tenant_id=? AND room_id=? AND memory_type=?
			FOR UPDATE
		`, *latest.MatchedMemoryItemID, tenantID, roomID, latest.MemoryType).Scan(&memoryItemID, &currentVersionID)
	}
	if latest.MatchedMemoryItemID == nil || errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `
			SELECT id, current_version_id
			FROM agent_memory_items
			WHERE tenant_id=? AND room_id=? AND memory_type=? AND memory_key=?
			FOR UPDATE
		`, tenantID, roomID, latest.MemoryType, memoryKey).Scan(&memoryItemID, &currentVersionID)
	}
	if errors.Is(err, sql.ErrNoRows) {
		created, createErr := tx.ExecContext(ctx, `
			INSERT INTO agent_memory_items (
				tenant_id, room_id, memory_type, memory_key, target, status, created_by_user_id
			) VALUES (?, ?, ?, ?, ?, 'active', ?)
		`, tenantID, roomID, latest.MemoryType, memoryKey, latest.Target, actorUserID)
		if createErr != nil {
			return model.AdoptAgentLearningOutput{}, createErr
		}
		memoryItemID, err = created.LastInsertId()
		if err != nil {
			return model.AdoptAgentLearningOutput{}, err
		}
	} else if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}

	var nextVersion uint64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM agent_memory_versions
		WHERE memory_item_id=?
	`, memoryItemID).Scan(&nextVersion); err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if currentVersionID.Valid {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_memory_versions
			SET status='superseded'
			WHERE id=? AND memory_item_id=? AND status='active'
		`, currentVersionID.Int64, memoryItemID); err != nil {
			return model.AdoptAgentLearningOutput{}, err
		}
	}
	createdVersion, err := tx.ExecContext(ctx, `
		INSERT INTO agent_memory_versions (
			memory_item_id, version_no, status, content_text, structured_json,
			source_session_id, source_result_id, created_by_user_id
		) VALUES (?, ?, 'active', ?, ?, ?, ?, ?)
	`, memoryItemID, nextVersion, latest.ResultText, string(structuredRaw), session.ID, latest.ID, actorUserID)
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	versionID, err := createdVersion.LastInsertId()
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_memory_items
		SET target=?, status='active', current_version_id=?, updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, latest.Target, versionID, memoryItemID); err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if reclassifiedFromMemoryID > 0 && reclassifiedFromMemoryID != memoryItemID {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_memory_items
			SET status='inactive', updated_at=CURRENT_TIMESTAMP(3)
			WHERE id=? AND tenant_id=? AND room_id=? AND status='active'
		`, reclassifiedFromMemoryID, tenantID, roomID); err != nil {
			return model.AdoptAgentLearningOutput{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM semantic_documents
		WHERE tenant_id=? AND room_id=? AND content_type='correction'
		  AND source_id IN (?, ?)
	`, tenantID, roomID, fmt.Sprintf("memory:%d", memoryItemID), fmt.Sprintf("memory:%d", reclassifiedFromMemoryID)); err != nil {
		return model.AdoptAgentLearningOutput{}, fmt.Errorf("invalidate changed correction vectors: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_learning_sessions
		SET status='adopted', memory_type=?, target=?, adopted_memory_item_id=?,
			adopted_at=CURRENT_TIMESTAMP(3), updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, latest.MemoryType, latest.Target, memoryItemID, session.ID); err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if err := recordAgentMemoryEvidenceAdoptionTx(ctx, tx, session, latest); err != nil {
		return model.AdoptAgentLearningOutput{}, fmt.Errorf("record agent memory evidence adoption: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}

	updatedSession, err := s.GetAgentLearningSession(ctx, tenantID, roomID, session.ID)
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	memory, err := s.GetAgentMemoryItem(ctx, tenantID, roomID, memoryItemID)
	if err != nil {
		return model.AdoptAgentLearningOutput{}, err
	}
	if memory.CurrentVersion == nil {
		return model.AdoptAgentLearningOutput{}, errors.New("adopted memory version missing")
	}
	return model.AdoptAgentLearningOutput{Session: updatedSession, Memory: memory, Version: *memory.CurrentVersion}, nil
}

func (s *Store) RollbackAgentMemoryVersion(
	ctx context.Context,
	tenantID, roomID, memoryItemID, versionID, actorUserID int64,
) (model.AgentMemoryItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	defer tx.Rollback()
	var currentVersionID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT current_version_id
		FROM agent_memory_items
		WHERE id=? AND tenant_id=? AND room_id=?
		FOR UPDATE
	`, memoryItemID, tenantID, roomID).Scan(&currentVersionID); err != nil {
		return model.AgentMemoryItem{}, err
	}
	var targetVersionID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM agent_memory_versions
		WHERE id=? AND memory_item_id=?
		FOR UPDATE
	`, versionID, memoryItemID).Scan(&targetVersionID); err != nil {
		return model.AgentMemoryItem{}, err
	}
	if currentVersionID.Valid && currentVersionID.Int64 != targetVersionID {
		if _, err := tx.ExecContext(ctx, `
			UPDATE agent_memory_versions
			SET status='rolled_back'
			WHERE id=? AND memory_item_id=?
		`, currentVersionID.Int64, memoryItemID); err != nil {
			return model.AgentMemoryItem{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_memory_versions
		SET status='active'
		WHERE id=? AND memory_item_id=?
	`, targetVersionID, memoryItemID); err != nil {
		return model.AgentMemoryItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE agent_memory_items
		SET current_version_id=?, status='active', updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=?
	`, targetVersionID, memoryItemID); err != nil {
		return model.AgentMemoryItem{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM semantic_documents WHERE tenant_id=? AND room_id=?
		  AND content_type='correction' AND source_id=?
	`, tenantID, roomID, fmt.Sprintf("memory:%d", memoryItemID)); err != nil {
		return model.AgentMemoryItem{}, fmt.Errorf("invalidate rolled-back correction vector: %w", err)
	}
	_ = actorUserID
	if err := tx.Commit(); err != nil {
		return model.AgentMemoryItem{}, err
	}
	return s.GetAgentMemoryItem(ctx, tenantID, roomID, memoryItemID)
}

func (s *Store) DeactivateAgentMemory(
	ctx context.Context,
	tenantID, roomID, memoryItemID, actorUserID int64,
) (model.AgentMemoryItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE agent_memory_items
		SET status='inactive', updated_at=CURRENT_TIMESTAMP(3)
		WHERE id=? AND tenant_id=? AND room_id=? AND status='active'
	`, memoryItemID, tenantID, roomID)
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return model.AgentMemoryItem{}, err
	}
	if affected == 0 {
		var exists int
		if getErr := tx.QueryRowContext(ctx, `
			SELECT 1 FROM agent_memory_items WHERE id=? AND tenant_id=? AND room_id=?
		`, memoryItemID, tenantID, roomID).Scan(&exists); getErr != nil {
			return model.AgentMemoryItem{}, getErr
		}
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM semantic_documents WHERE tenant_id=? AND room_id=?
		  AND content_type='correction' AND source_id=?
	`, tenantID, roomID, fmt.Sprintf("memory:%d", memoryItemID)); err != nil {
		return model.AgentMemoryItem{}, fmt.Errorf("delete deactivated correction vector: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return model.AgentMemoryItem{}, err
	}
	_ = actorUserID
	return s.GetAgentMemoryItem(ctx, tenantID, roomID, memoryItemID)
}
