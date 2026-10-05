package db

import (
	"context"
	"fmt"
	"livecompanion/management/internal/model"
)

func (s *Store) ClaimFreeMarketingOrder(ctx context.Context, tenant, user, order int64) (model.CustomerShopOrder, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	defer tx.Rollback()
	var no, kind, status string
	var payable uint64
	if err = tx.QueryRowContext(ctx, `SELECT order_no,order_type,status,payable_amount_cents FROM biz_orders WHERE id=? AND tenant_id=? FOR UPDATE`, order, tenant).Scan(&no, &kind, &status, &payable); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if status == "fulfilled" || status == "completed" || status == "paid" {
		tx.Rollback()
		return s.GetCustomerShopOrder(ctx, tenant, order)
	}
	if status != "pending" || payable != 0 {
		return model.CustomerShopOrder{}, ErrMarketingEligibility
	}
	var usage int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM mkt_campaign_usage WHERE order_id=? AND status='pending' LIMIT 1 FOR UPDATE`, order).Scan(&usage); err != nil {
		return model.CustomerShopOrder{}, ErrMarketingEligibility
	}
	if err = ensureNoWechatPendingTx(ctx, tx, order); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if kind == "device" {
		var expired int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM biz_order_devices WHERE order_id=? AND status='payment_hold' AND hold_expires_at IS NOT NULL AND hold_expires_at<=CURRENT_TIMESTAMP(3)", order).Scan(&expired); err != nil {
			return model.CustomerShopOrder{}, err
		}
		if expired > 0 {
			if _, err = releaseDeviceOrderHoldTx(ctx, tx, user, order, "免费活动订单超时释放"); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE biz_orders SET status='cancelled',cancelled_at=CURRENT_TIMESTAMP(3) WHERE id=? AND tenant_id=?", order, tenant); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if err = releaseMarketingCampaignOrderTx(ctx, tx, order, "expired"); err != nil {
				return model.CustomerShopOrder{}, err
			}
			if err = tx.Commit(); err != nil {
				return model.CustomerShopOrder{}, err
			}
			return model.CustomerShopOrder{}, ErrShopOrderExpired
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE biz_orders SET status='paid',paid_amount_cents=0,paid_at=CURRENT_TIMESTAMP(3) WHERE id=? AND status='pending'`, order); err != nil {
		return model.CustomerShopOrder{}, err
	}
	switch kind {
	case "time_card":
		err = fulfillTimeCardOrderTx(ctx, tx, tenant, user, order, no)
	case "membership":
		err = fulfillMembershipOrderTx(ctx, tx, tenant, user, order, no, "marketing_free")
	case "device":
		err = fulfillDeviceOrderTx(ctx, tx, tenant, user, order, no, false)
	default:
		err = ErrUnsupportedShopProduct
	}
	if err != nil {
		return model.CustomerShopOrder{}, err
	}
	if err = consumeMarketingCampaignOrderTx(ctx, tx, order); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO biz_audit_events(actor_user_id,action,entity_type,entity_id,after_json) VALUES (?,'marketing.free_claim','order',?,JSON_OBJECT('order_no',?,'amount_cents',0))`, user, fmt.Sprint(order), no); err != nil {
		return model.CustomerShopOrder{}, err
	}
	if err = tx.Commit(); err != nil {
		return model.CustomerShopOrder{}, err
	}
	return s.GetCustomerShopOrder(ctx, tenant, order)
}
