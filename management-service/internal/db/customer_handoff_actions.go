package db

import (
	"context"
	"time"
)

func validCustomerHandoffTransition(from, to string) bool {
	if from == to {
		return from == "pending" || from == "accepted" || from == "in_progress" || from == "completed"
	}
	return (from == "pending" && to == "accepted") || (from == "accepted" && to == "in_progress") || (from == "in_progress" && to == "completed")
}

type CustomerHandoffEvent struct {
	ID           int64     `json:"id"`
	OperatorName string    `json:"operator_name"`
	Status       string    `json:"status"`
	Content      string    `json:"content"`
	CreatedAt    time.Time `json:"created_at"`
}

func (s *Store) ListCustomerHandoffEvents(ctx context.Context, id int64, page, size int) ([]CustomerHandoffEvent, int64, error) {
	page, size = normalizeBusinessPage(page, size, 100)
	var total int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM crm_customer_handoff_events WHERE handoff_id=?", id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT e.id,COALESCE(u.display_name,u.username,''),e.status,e.content,e.created_at FROM crm_customer_handoff_events e LEFT JOIN mgmt_users u ON u.id=e.operator_user_id WHERE e.handoff_id=? ORDER BY e.id DESC LIMIT ? OFFSET ?`, id, size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []CustomerHandoffEvent{}
	for rows.Next() {
		var v CustomerHandoffEvent
		if err := rows.Scan(&v.ID, &v.OperatorName, &v.Status, &v.Content, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, v)
	}
	return items, total, rows.Err()
}
