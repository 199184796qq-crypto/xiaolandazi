package db

import (
	"context"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
)

func (s *Store) ListInteractionLearningHistory(ctx context.Context, tenant, room, plan int64, status, source, query string, limit, offset int) ([]model.AgentLearningSession, int, int, error) {
	scope := `tenant_id=? AND room_id=? AND review_metadata_json IS NOT NULL AND JSON_EXTRACT(review_metadata_json,'$.plan_id')=?`
	args := []any{tenant, room, plan}
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
	if source != "" {
		if source != "live_uncertain" && source != "initial_simulation" {
			return nil, 0, 0, fmt.Errorf("invalid review source")
		}
		scope += ` AND source_type=?`
		args = append(args, source)
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
	return items, pending, total, rows.Err()
}
