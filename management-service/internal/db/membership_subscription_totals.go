package db

import (
	"context"
	"time"
)

type MembershipSubscriptionTotal struct {
	Count       int64  `json:"subscription_count"`
	AmountCents uint64 `json:"subscription_amount_cents"`
}

func (s *Store) MembershipSubscriptionTotals(ctx context.Context, start, end time.Time) (map[int64]MembershipSubscriptionTotal, error) {
	var args []any
	period := ""
	if !start.IsZero() {
		period = "AND o.paid_at >= ? AND o.paid_at < ?"
		args = []any{start, end}
	}
	// A signup or renewal is one subscription. The order records the amount
	// actually received, including successful promotional orders paid at zero.
	rows, err := s.db.QueryContext(ctx, `
		SELECT product_id, COUNT(*), SUM(paid_amount_cents)
		FROM (
			SELECT DISTINCT i.product_id, o.id, o.paid_amount_cents
			FROM biz_orders o
			JOIN biz_order_items i ON i.order_id = o.id AND i.product_type = 'membership'
			WHERE o.order_type = 'membership'
			  AND o.status IN ('paid', 'fulfilled', 'completed')
			  AND o.paid_at IS NOT NULL
	`+period+`
		) paid_subscriptions
		GROUP BY product_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := make(map[int64]MembershipSubscriptionTotal)
	for rows.Next() {
		var planID int64
		var total MembershipSubscriptionTotal
		if err := rows.Scan(&planID, &total.Count, &total.AmountCents); err != nil {
			return nil, err
		}
		totals[planID] = total
	}
	return totals, rows.Err()
}
