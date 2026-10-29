package db

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// ListRecentInteractionTurns returns the room's latest viewer question /
// generated answer pairs for intent context. The current question is excluded
// by the caller; history never becomes fact evidence.
func (s *Store) ListRecentInteractionTurns(ctx context.Context, tenantID, roomID int64, limit int) ([]model.InteractionRecentTurn, error) {
	if limit <= 0 || limit > 5 {
		limit = 3
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT question_text, generated_text, created_at
FROM live_generated_speech_history
WHERE tenant_id=? AND room_id=? AND question_text <> ''
ORDER BY id DESC LIMIT ?`, tenantID, roomID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.InteractionRecentTurn, 0, limit)
	for rows.Next() {
		var item model.InteractionRecentTurn
		if err := rows.Scan(&item.Question, &item.Answer, &item.At); err != nil {
			return nil, err
		}
		item.Question = strings.TrimSpace(item.Question)
		item.Answer = strings.TrimSpace(item.Answer)
		if item.Question != "" && item.Answer != "" {
			out = append(out, item)
		}
	}
	// DB returned newest first; prompt reads oldest -> newest.
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out, rows.Err()
}
