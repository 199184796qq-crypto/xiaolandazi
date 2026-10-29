package db

import (
	"context"
	"strings"

	"livecompanion/management/internal/model"
)

// ListRecentInteractionTurns returns the room's latest viewer chat lines for
// intent context. Generated warm-up/filler prompts are deliberately excluded;
// chat history only resolves pronouns/follow-ups and never becomes fact
// evidence.
func (s *Store) ListRecentInteractionTurns(ctx context.Context, tenantID, roomID int64, userID string, limit int) ([]model.InteractionRecentTurn, error) {
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	userID = strings.TrimSpace(userID)
	query := `
SELECT content, occurred_at
FROM live_room_event_archive
WHERE tenant_id=? AND room_id=? AND event_type='chat' AND content IS NOT NULL AND content <> ''`
	args := []any{tenantID, roomID}
	if userID != "" {
		query += ` AND user_id=?`
		args = append(args, userID)
	}
	query += ` ORDER BY occurred_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]model.InteractionRecentTurn, 0, limit)
	for rows.Next() {
		var item model.InteractionRecentTurn
		if err := rows.Scan(&item.Question, &item.At); err != nil {
			return nil, err
		}
		item.Question = strings.TrimSpace(item.Question)
		if item.Question != "" {
			out = append(out, item)
		}
	}
	// DB returned newest first; prompt reads oldest -> newest.
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out, rows.Err()
}
