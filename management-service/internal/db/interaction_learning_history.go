package db

import (
	"context"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

func (s *Store) ListInteractionLearningHistory(ctx context.Context, tenant, room, plan int64, status, source, query string, limit, offset int) ([]model.AgentLearningSession, int, int, error) {
	scope := `tenant_id=? AND room_id=? AND review_metadata_json IS NOT NULL AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=? AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(review_metadata_json,'$.history_deleted')),'false')<>'true'`
	args := []any{tenant, room, plan}
	if source != "" {
		if source != "live_uncertain" && source != "initial_simulation" {
			return nil, 0, 0, fmt.Errorf("invalid review source")
		}
		scope += ` AND source_type=?`
		args = append(args, source)
	}
	var pending int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_learning_sessions WHERE `+scope+` AND status='editing'`, args...).Scan(&pending); err != nil {
		return nil, 0, 0, err
	}
	switch status {
	case "pending":
		scope += ` AND status='editing'`
	case "closed":
		scope += ` AND status='closed'`
	case "adopted":
		scope += ` AND status='adopted'`
	case "all":
	default:
		return nil, 0, 0, fmt.Errorf("invalid review status")
	}
	if query = strings.TrimSpace(query); query != "" {
		scope += ` AND (LOCATE(?,question)>0 OR LOCATE(?,original_reply)>0 OR LOCATE(?,review_metadata_json)>0)`
		args = append(args, query, query, query)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_learning_sessions WHERE `+scope, args...).Scan(&total); err != nil {
		return nil, 0, 0, err
	}
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, 0, 0, fmt.Errorf("invalid review pagination")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentLearningSessionColumns+` FROM agent_learning_sessions WHERE `+scope+` ORDER BY updated_at DESC,id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()
	items := []model.AgentLearningSession{}
	for rows.Next() {
		item, e := scanAgentLearningSession(rows)
		if e != nil {
			return nil, 0, 0, e
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, 0, err
	}
	if err = s.attachInteractionCorrections(ctx, tenant, room, plan, items); err != nil {
		return nil, 0, 0, err
	}
	return items, pending, total, nil
}

// Read saved user feedback and its matching model result together. Follow-up
// correction drafts also belong to the originating history card; copied model
// suggestions never stand in for what the user actually entered.
func (s *Store) attachInteractionCorrections(ctx context.Context, tenant, room, plan int64, items []model.AgentLearningSession) error {
	if len(items) == 0 {
		return nil
	}
	ids := make([]any, len(items))
	positions := map[int64]int{}
	for i := range items {
		ids[i] = items[i].ID
		positions[items[i].ID] = i
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := append([]any{tenant, room, plan}, ids...)
	args = append(args, ids...)
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,
	 CAST(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(s.review_metadata_json,'$.history_origin_id')),'0') AS UNSIGNED),
	 e.turn_no,r.id,e.feedback,r.result_text,r.created_at
	 FROM agent_learning_sessions s JOIN agent_learning_evidence e ON e.session_id=s.id
	 JOIN agent_learning_results r ON r.evidence_id=e.id AND r.session_id=s.id AND r.turn_no=e.turn_no
	 WHERE s.tenant_id=? AND s.room_id=? AND JSON_EXTRACT(s.review_metadata_json,'$.plan_id')=?
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(s.review_metadata_json,'$.history_deleted')),'false')<>'true'
	 AND (s.id IN (`+placeholders+`) OR JSON_EXTRACT(s.review_metadata_json,'$.history_origin_id') IN (`+placeholders+`))
	 ORDER BY r.created_at ASC,r.id ASC`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var correction model.AgentLearningCorrection
		var origin int64
		if err := rows.Scan(&correction.SessionID, &origin, &correction.TurnNo, &correction.ResultID, &correction.UserFeedback, &correction.PreviewText, &correction.CreatedAt); err != nil {
			return err
		}
		if i, ok := positions[correction.SessionID]; ok {
			items[i].CorrectionHistory = append(items[i].CorrectionHistory, correction)
		}
		if i, ok := positions[origin]; ok && origin != correction.SessionID {
			items[i].CorrectionHistory = append(items[i].CorrectionHistory, correction)
		}
	}
	return rows.Err()
}
