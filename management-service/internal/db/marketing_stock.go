package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"livecompanion/management/internal/model"
)

var (
	ErrMarketingCampaignStockBelowUsage = errors.New("库存数量不能低于已参与和待支付占用的数量")
	ErrMarketingCampaignStockChanged    = errors.New("库存设置已变化，请刷新后重新调整")
)

func (s *Store) UpdateWorkspaceMarketingStock(ctx context.Context, campaignID, actorID int64, limit, previous uint32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var status string
	if err := tx.QueryRowContext(ctx, "SELECT status FROM mkt_campaigns WHERE id=? FOR UPDATE", campaignID).Scan(&status); err != nil {
		return err
	}
	if status == "archived" {
		return ErrMarketingCampaignArchived
	}
	current, err := readMarketingStockLimitTx(ctx, tx, campaignID)
	if err != nil || current == limit {
		return err
	}
	if current != previous {
		return ErrMarketingCampaignStockChanged
	}
	if err := saveMarketingStockLimitTx(ctx, tx, campaignID, &limit); err != nil {
		return err
	}
	if err := insertCommercialAuditTx(ctx, tx, actorID, "marketing.campaign.stock.update", "marketing_campaign", fmt.Sprint(campaignID), current, limit); err != nil {
		return err
	}
	return tx.Commit()
}

type marketingStockQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Usage is recorded per item, while a campaign's stock counts one per order.
// Refunds leave the original participation consumed; cancelled orders only
// release a pending hold.
func readMarketingStock(ctx context.Context, q marketingStockQueryer, campaignID, excludeOrderID int64, limit uint32, forUpdate bool) (model.MarketingCampaignStock, error) {
	stock := model.MarketingCampaignStock{Limit: limit}
	query := `
		SELECT COUNT(DISTINCT CASE WHEN status='consumed' THEN order_id END)
		         + COUNT(CASE WHEN status='consumed' AND order_id IS NULL THEN 1 END),
		       COUNT(DISTINCT CASE WHEN status='pending' THEN order_id END)
		FROM mkt_campaign_usage
		WHERE campaign_id=? AND status IN ('consumed','pending')
		  AND (order_id IS NULL OR order_id<>?)`
	if forUpdate {
		// A checkout may already have a consistent-read snapshot. Locking
		// reads see the reservations made before it acquired the campaign lock.
		query += " FOR UPDATE"
	}
	if err := q.QueryRowContext(ctx, query, campaignID, excludeOrderID).Scan(&stock.Used, &stock.Reserved); err != nil {
		return stock, err
	}
	if committed := stock.Used + stock.Reserved; committed < uint64(stock.Limit) {
		stock.Remaining = uint64(stock.Limit) - committed
	}
	return stock, nil
}

func readMarketingStockLimitTx(ctx context.Context, tx *sql.Tx, campaignID int64) (uint32, error) {
	var limit uint32
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(controls_json, '$.stock_limit')) AS UNSIGNED),0)
		FROM mkt_campaign_controls WHERE campaign_id=? FOR UPDATE
	`, campaignID).Scan(&limit)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return limit, err
}

func saveMarketingStockLimitTx(ctx context.Context, tx *sql.Tx, campaignID int64, limit *uint32) error {
	if limit == nil {
		return nil
	}
	if *limit > 0 {
		current, err := readMarketingStock(ctx, tx, campaignID, 0, *limit, true)
		if err != nil {
			return err
		}
		if current.Used+current.Reserved > uint64(*limit) {
			return ErrMarketingCampaignStockBelowUsage
		}
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO mkt_campaign_controls (campaign_id, controls_json)
		VALUES (?,JSON_OBJECT('stock_limit', ?))
		ON DUPLICATE KEY UPDATE controls_json=JSON_SET(controls_json, '$.stock_limit', ?)
	`, campaignID, *limit, *limit)
	return err
}
