package db

import (
	"context"
	"database/sql"
)

func cancelMarketingArchiveOrdersTx(ctx context.Context, tx *sql.Tx, campaignID, actorID int64) ([]int64, error) {
	// Checkout locks the campaign row before reserving any new orders for it.
	rows, err := tx.QueryContext(ctx, `
		SELECT o.id
		FROM (
			SELECT order_id FROM mkt_campaign_order_snapshots WHERE campaign_id=?
			UNION
			SELECT order_id FROM mkt_campaign_usage WHERE campaign_id=? AND order_id IS NOT NULL
		) linked JOIN biz_orders o ON o.id=linked.order_id
		WHERE o.status='pending'
		ORDER BY o.id
	`, campaignID, campaignID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orderIDs []int64
	for rows.Next() {
		var orderID int64
		if err := rows.Scan(&orderID); err != nil {
			return nil, err
		}
		orderIDs = append(orderIDs, orderID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	cancelled := make([]int64, 0, len(orderIDs))
	for _, orderID := range orderIDs {
		var status, kind string
		var hasSettlement bool
		if err := tx.QueryRowContext(ctx, `
			SELECT status, order_type, paid_at IS NOT NULL OR paid_amount_cents>0 OR refunded_amount_cents>0
			FROM biz_orders WHERE id=? FOR UPDATE
		`, orderID).Scan(&status, &kind, &hasSettlement); err != nil {
			return nil, err
		}
		if status != "pending" {
			continue
		}
		if hasSettlement {
			return nil, ErrMarketingCampaignArchivePaymentUnconfirmed
		}

		// Settlement and direct receipt submission share this order lock.
		var uncertain bool
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM fin_payment_transactions WHERE order_id=?
				AND (status NOT IN ('failed','closed') OR paid_amount_cents>0 OR paid_at IS NOT NULL
				     OR provider_trade_state='SUCCESS')
			) OR EXISTS (
				SELECT 1 FROM fin_customer_receipts WHERE order_id=? AND purpose='order' AND status<>'rejected'
			)
		`, orderID, orderID).Scan(&uncertain); err != nil {
			return nil, err
		}
		if uncertain {
			return nil, ErrMarketingCampaignArchivePaymentUnconfirmed
		}
		if err := cancelUnpaidShopOrderTx(ctx, tx, orderID, kind, actorID, "营销活动归档取消未支付订单，释放锁库"); err != nil {
			return nil, err
		}
		cancelled = append(cancelled, orderID)
	}

	var remaining bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM mkt_campaign_usage WHERE campaign_id=? AND status='pending'
	)`, campaignID).Scan(&remaining); err != nil {
		return nil, err
	}
	if remaining {
		return nil, ErrMarketingCampaignPendingOrders
	}
	return cancelled, nil
}
