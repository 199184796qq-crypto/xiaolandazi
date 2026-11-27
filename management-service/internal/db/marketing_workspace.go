package db

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"livecompanion/management/internal/model"
)

type MarketingWorkspaceCampaign struct {
	model.MarketingCampaign
	Stock                    *model.MarketingCampaignStock `json:"stock,omitempty"`
	ParticipationCount       int64                         `json:"participation_count"`
	ParticipationAmountCents uint64                        `json:"participation_amount_cents"`
	HasOrders                bool                          `json:"has_orders"`
	HasPendingOrders         bool                          `json:"has_pending_orders"`
}

func (s *Store) ListMarketingWorkspace(ctx context.Context, start, end time.Time) ([]MarketingWorkspaceCampaign, error) {
	campaigns, err := s.listNormalizedMarketingCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]MarketingWorkspaceCampaign, len(campaigns))
	if !start.IsZero() {
		start, end = start.UTC(), end.UTC()
	}
	period := ""
	var args []any
	if !start.IsZero() {
		period = "WHERE participated_at >= ? AND participated_at < ?"
		args = []any{start, end}
	}
	// A participation is one settled order or one successful orderless grant.
	// The usage and snapshot tables may describe the same order.
	rows, err := s.db.QueryContext(ctx, `
		WITH campaign_orders AS (
			SELECT campaign_id, order_id FROM mkt_campaign_order_snapshots
			UNION
			SELECT campaign_id, order_id FROM mkt_campaign_usage WHERE order_id IS NOT NULL
		), participations AS (
			SELECT co.campaign_id, o.paid_at AS participated_at,
			       o.paid_amount_cents - LEAST(o.paid_amount_cents, o.refunded_amount_cents) AS net_cents
			FROM campaign_orders co JOIN biz_orders o ON o.id=co.order_id
			WHERE o.paid_at IS NOT NULL
			  AND o.status IN ('paid','fulfilled','completed','partially_refunded','refunded')
			UNION ALL
			SELECT campaign_id, created_at, 0
			FROM mkt_campaign_usage WHERE order_id IS NULL AND status='consumed'
		), totals AS (
			SELECT campaign_id, COUNT(*) AS quantity, SUM(net_cents) AS amount
			FROM participations
	`+period+`
			GROUP BY campaign_id
		)
		SELECT c.id, COALESCE(t.quantity,0), COALESCE(t.amount,0),
		       EXISTS (SELECT 1 FROM mkt_campaign_usage u WHERE u.campaign_id=c.id)
		        OR EXISTS (SELECT 1 FROM campaign_orders co WHERE co.campaign_id=c.id),
		       EXISTS (SELECT 1 FROM mkt_campaign_usage u WHERE u.campaign_id=c.id AND u.status='pending')
		        OR EXISTS (SELECT 1 FROM campaign_orders co JOIN biz_orders o ON o.id=co.order_id
		                   WHERE co.campaign_id=c.id AND o.status='pending')
		FROM mkt_campaigns c LEFT JOIN totals t ON t.campaign_id=c.id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	totalsByID := make(map[int64]MarketingWorkspaceCampaign, len(campaigns))
	for rows.Next() {
		var id int64
		var item MarketingWorkspaceCampaign
		if err := rows.Scan(&id, &item.ParticipationCount, &item.ParticipationAmountCents, &item.HasOrders, &item.HasPendingOrders); err != nil {
			return nil, err
		}
		totalsByID[id] = item
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index, campaign := range campaigns {
		items[index] = totalsByID[campaign.ID]
		items[index].MarketingCampaign = campaign
		items[index].Stock = campaign.Stock
	}
	return items, nil
}

var ErrMarketingCampaignOrderChanged = errors.New("营销计划顺序或列表已变化，请重新打开排序面板")
var ErrMarketingCampaignArchivePaymentUnconfirmed = errors.New("marketing campaign order payment unconfirmed")

func (s *Store) ReorderWorkspaceMarketingCampaigns(ctx context.Context, actorID int64, ids, previous []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM mkt_campaigns WHERE status<>'archived' ORDER BY sort_order, id FOR UPDATE
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	current := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		current = append(current, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if !slices.Equal(current, previous) {
		return ErrMarketingCampaignOrderChanged
	}
	if slices.Equal(current, ids) {
		return nil
	}
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	slices.Sort(current)
	if !slices.Equal(current, sorted) {
		return ErrMarketingCampaignOrderChanged
	}

	for rank, id := range ids {
		if _, err := tx.ExecContext(ctx, "UPDATE mkt_campaigns SET sort_order=?, updated_by_user_id=? WHERE id=?",
			(rank+1)*10, actorID, id); err != nil {
			return err
		}
	}
	if err := insertCommercialAuditTx(ctx, tx, actorID, "marketing.campaign.reorder", "marketing_campaign", "catalog", previous, ids); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetMarketingCampaignStatus(ctx context.Context, campaignID, actorID int64, next string) error {
	// Payment checks must see transactions committed while waiting on the order lock.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var endsAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `
		SELECT status, ends_at FROM mkt_campaigns WHERE id=? FOR UPDATE
	`, campaignID).Scan(&status, &endsAt); err != nil {
		return err
	}
	if status == "archived" && next == "archived" {
		return nil
	}
	if status == "archived" {
		return ErrMarketingCampaignArchived
	}
	ended := endsAt.Valid && !time.Now().Before(endsAt.Time)
	var stock model.MarketingCampaignStock
	if next == "active" || (next == "archived" && status == "active" && !ended) {
		limit, err := readMarketingStockLimitTx(ctx, tx, campaignID)
		if err != nil {
			return err
		}
		if limit > 0 {
			stock, err = readMarketingStock(ctx, tx, campaignID, 0, limit, false)
			if err != nil {
				return err
			}
		}
	}
	switch next {
	case "active":
		if ended {
			return ErrMarketingCampaignUnavailable
		}
		if stock.SoldOut() {
			return ErrMarketingCampaignStockInsufficient
		}
	case "inactive":
	case "archived":
		if status == "active" && !ended && !stock.SoldOut() {
			return ErrMarketingCampaignMustUnlist
		}
		cancelled, err := cancelMarketingArchiveOrdersTx(ctx, tx, campaignID, actorID)
		if err != nil {
			return err
		}
		if len(cancelled) > 0 {
			if err := insertCommercialAuditTx(ctx, tx, actorID, "marketing.campaign.archive", "marketing_campaign",
				strconv.FormatInt(campaignID, 10),
				map[string]string{"status": status},
				map[string]any{"status": next, "cancelled_order_ids": cancelled},
			); err != nil {
				return err
			}
		}
	default:
		return ErrMarketingCampaignUnavailable
	}
	if status == next {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mkt_campaigns SET status=?, updated_by_user_id=? WHERE id=?
	`, next, actorID, campaignID); err != nil {
		return err
	}
	return tx.Commit()
}
