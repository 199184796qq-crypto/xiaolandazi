package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"time"
)

var ErrTimeCardRefundUnavailable = errors.New("这张时长卡当前不能申请退款")
var ErrTimeCardRefundAmount = errors.New("退款金额与按购卡面额和到期剩余时长计算的金额不一致")

const timeCardResidualRefund = "time_card_expiry"
const timeCardActivationRefund = "time_card_activation_expiry"
const timeCardPaidCentsSQL = `(i.unit_paid_price_cents + IF(i.quantity>0 AND
    a.asset_sequence<=GREATEST(CAST(o.paid_amount_cents AS SIGNED)-CAST(i.quantity*i.unit_paid_price_cents AS SIGNED),0),1,0))`

func proportionalTimeCardCents(face, remainder, duration uint64) uint64 {
	if duration == 0 || remainder > duration {
		return 0
	}
	high, low := bits.Mul64(face, remainder)
	amount, fraction := bits.Div64(high, low, duration)
	if fraction >= duration/2+duration%2 {
		amount++
	}
	return amount
}

func (s *Store) RequestTimeCardExpiryRefund(ctx context.Context, productID, assetID, staffID int64, amount uint64, reason, supervisor string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var tenantID, orderID int64
	var face, original, remaining uint64
	var saleNo, status string
	var activated sql.NullTime
	if err := tx.QueryRowContext(ctx, `
        SELECT a.tenant_id, a.source_order_id, a.status, a.activated_at, a.original_seconds,
               i.unit_list_price_cents, sale.sale_no
        FROM biz_time_card_assets a
        JOIN biz_order_items i ON i.id=a.source_order_item_id
        JOIN biz_time_card_sales sale ON sale.asset_id=a.id
        WHERE a.id=? AND a.product_id=? FOR UPDATE OF a
    `, assetID, productID).Scan(&tenantID, &orderID, &status, &activated, &original, &face, &saleNo); err != nil {
		return err
	}
	if status != "expired" || !activated.Valid {
		return ErrTimeCardRefundUnavailable
	}
	if err := tx.QueryRowContext(ctx, `
        SELECT COALESCE(MAX(remaining_before_seconds),0) FROM quota_ledger
        WHERE business_type='time_card_expired' AND business_id=? AND tenant_id=?
    `, assetID, tenantID).Scan(&remaining); err != nil {
		return err
	}
	calculated := proportionalTimeCardCents(face, remaining, original)
	if calculated == 0 {
		return ErrTimeCardRefundUnavailable
	}
	if amount != calculated {
		return ErrTimeCardRefundAmount
	}

	// The asset lock serializes applications. A rejected application may be
	// corrected and resubmitted, but a pending or paid application is unique.
	var exists bool
	if err := tx.QueryRowContext(ctx, `
        SELECT EXISTS(SELECT 1 FROM fin_refund_orders WHERE source_type=? AND source_id=? AND status<>'rejected')
    `, timeCardResidualRefund, assetID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrTimeCardRefundUnavailable
	}

	refundNo, err := newInternalCode(ctx, tx, "tcr", time.Now())
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
        INSERT INTO fin_refund_orders (refund_no, tenant_id, source_type, source_id, refund_amount_cents,
            refund_method, status, reason, operator_user_id)
        VALUES (?, ?, ?, ?, ?, 'withdrawable_wallet', 'pending_approval', ?, ?)
    `, refundNo, tenantID, timeCardResidualRefund, assetID, amount, reason, staffID)
	if err != nil {
		return err
	}
	refundID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO biz_time_card_sale_refunds (asset_id, refund_id, amount_cents, supervisor) VALUES (?, ?, ?, ?)
    `, assetID, refundID, amount, supervisor); err != nil {
		return err
	}
	_, err = createFinanceApprovalTaskTx(ctx, tx, nil, "finance.refund", staffID, tenantID, amount, financeTaskPayload{
		OrderID: refundID, SourceOrderID: orderID, AmountCents: amount, PaymentMethod: "withdrawable_wallet",
		Reason: fmt.Sprintf("时长卡到期剩余退款 · 销售编号 %s · 上级主管 %s · %s", saleNo, supervisor, reason),
	}, true)
	if err != nil {
		return err
	}
	return s.commitInboxTx(ctx, tx, "finance")
}

func applyTimeCardResidualRefundTx(ctx context.Context, tx *sql.Tx, tenantID, refundID, operator int64, expected uint64) error {
	var amount uint64
	var status, reason, orderNo string
	if err := tx.QueryRowContext(ctx, `
        SELECT r.refund_amount_cents, r.status, r.reason, o.order_no
        FROM fin_refund_orders r
        JOIN biz_time_card_sale_refunds sr ON sr.refund_id=r.id AND sr.asset_id=r.source_id
        JOIN biz_time_card_assets a ON a.id=sr.asset_id AND a.tenant_id=r.tenant_id
        JOIN biz_orders o ON o.id=a.source_order_id
        WHERE r.id=? AND r.tenant_id=? AND r.source_type=? AND r.refund_method='withdrawable_wallet'
    `, refundID, tenantID, timeCardResidualRefund).Scan(&amount, &status, &reason, &orderNo); err != nil {
		return err
	}
	if status != "pending_approval" || expected != amount || amount == 0 {
		return ErrTimeCardRefundUnavailable
	}
	if err := creditTimeCardRefundTx(ctx, tx, tenantID, refundID, operator, amount, "reward", timeCardResidualRefund, orderNo, reason); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE fin_refund_orders SET status='success',processed_at=UTC_TIMESTAMP(3) WHERE id=?", refundID)
	return err
}

func creditTimeCardRefundTx(ctx context.Context, tx *sql.Tx, tenantID, refundID, operator int64,
	amount uint64, wallet, source, orderNo, reason string) error {
	if amount == 0 {
		return nil
	}
	walletID, before, err := lockWalletAccountTx(ctx, tx, tenantID, wallet)
	if err != nil {
		return err
	}
	after := before + int64(amount)
	if after < before {
		return fmt.Errorf("wallet balance overflow")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE fin_wallet_accounts SET balance_cents=? WHERE id=?", after, walletID); err != nil {
		return err
	}
	key := fmt.Sprintf("%s:%d", source, refundID)
	_, err = tx.ExecContext(ctx, `
        INSERT INTO fin_wallet_ledger (external_id, tenant_id, wallet_account_id, direction, amount_cents,
            balance_before_cents, balance_after_cents, business_type, business_id, order_no, operator_user_id,
            reason, idempotency_key, occurred_at)
        VALUES (?, ?, ?, 'credit', ?, ?, ?, ?, ?, ?, NULLIF(?,0), ?, ?, UTC_TIMESTAMP(3))
    `, key, tenantID, walletID, amount, before, after, source, refundID, orderNo, operator, reason, key)
	return err
}

func (s *Store) SweepTimeCardExpirations(ctx context.Context, now time.Time) error {
	rows, err := s.db.QueryContext(ctx, `
        SELECT DISTINCT tenant_id FROM biz_time_card_assets
        WHERE status='active' AND expires_at<=? ORDER BY tenant_id LIMIT 100
    `, now)
	if err != nil {
		return err
	}
	tenants, err := scanTimeCardIDs(rows)
	if err != nil {
		return err
	}
	var failures error
	for _, tenantID := range tenants {
		if err := s.expireTimeCardsForTenant(ctx, tenantID, now); err != nil {
			failures = errors.Join(failures, fmt.Errorf("expire activated time cards for tenant %d: %w", tenantID, err))
		}
	}
	rows, err = s.db.QueryContext(ctx, `
        SELECT a.id FROM biz_time_card_sales sale
        JOIN biz_time_card_assets a ON a.id=sale.asset_id
        WHERE sale.auto_refund=1 AND a.activated_at IS NULL AND a.status IN ('unactivated','expired')
          AND a.activation_deadline_at<=?
        ORDER BY a.activation_deadline_at,a.id LIMIT 100
    `, now)
	if err != nil {
		return errors.Join(failures, err)
	}
	assets, err := scanTimeCardIDs(rows)
	if err != nil {
		return errors.Join(failures, err)
	}
	for _, assetID := range assets {
		if err := s.refundUnactivatedTimeCard(ctx, assetID, now); err != nil {
			failures = errors.Join(failures, fmt.Errorf("refund expired unactivated time card %d: %w", assetID, err))
		}
	}
	return failures
}

func scanTimeCardIDs(rows *sql.Rows) ([]int64, error) {
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) expireTimeCardsForTenant(ctx context.Context, tenantID int64, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockTenantAIResourceTx(ctx, tx, tenantID); err != nil {
		return err
	}
	if _, err := expireTimeCardAssetsTx(ctx, tx, tenantID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) refundUnactivatedTimeCard(ctx context.Context, assetID int64, now time.Time) error {
	var orderID int64
	if err := s.db.QueryRowContext(ctx, "SELECT source_order_id FROM biz_time_card_assets WHERE id=?", assetID).Scan(&orderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var orderNo, orderStatus string
	var tenantID int64
	var paid, refunded uint64
	if err := tx.QueryRowContext(ctx, `
        SELECT order_no, tenant_id, status, paid_amount_cents, refunded_amount_cents
        FROM biz_orders WHERE id=? FOR UPDATE
    `, orderID).Scan(&orderNo, &tenantID, &orderStatus, &paid, &refunded); err != nil {
		return err
	}
	var price uint64
	err = tx.QueryRowContext(ctx, `
        SELECT `+timeCardPaidCentsSQL+`
        FROM biz_time_card_assets a
        JOIN biz_time_card_sales sale ON sale.asset_id=a.id AND sale.auto_refund=1
        JOIN biz_orders o ON o.id=a.source_order_id
        JOIN biz_order_items i ON i.id=a.source_order_item_id
        WHERE a.id=? AND a.source_order_id=? AND a.tenant_id=? AND a.activated_at IS NULL
          AND a.status IN ('unactivated','expired') AND a.activation_deadline_at<=? FOR UPDATE OF a
    `, assetID, orderID, tenantID, now).Scan(&price)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var already uint64
	if err := tx.QueryRowContext(ctx, `
        SELECT COALESCE(SUM(sr.amount_cents),0) FROM biz_time_card_sale_refunds sr
        JOIN fin_refund_orders r ON r.id=sr.refund_id
        WHERE sr.asset_id=? AND r.status IN ('success','completed')
    `, assetID).Scan(&already); err != nil {
		return err
	}
	amount := price - min(price, already)
	if refunded > paid || amount > paid-refunded {
		return fmt.Errorf("unmatched prior refund for time card %d", assetID)
	}

	reason := "超过激活期限，自动退卡"
	refundNo, err := newInternalCode(ctx, tx, "tcr", now)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `
        INSERT INTO fin_refund_orders (refund_no,tenant_id,source_type,source_id,refund_amount_cents,refund_method,
            status,reason,idempotency_key,processed_at)
        VALUES (?, ?, ?, ?, ?, 'wallet_balance', 'success', ?, ?, ?)
    `, refundNo, tenantID, timeCardActivationRefund, assetID, amount, reason, fmt.Sprintf("%s:%d", timeCardActivationRefund, assetID), now)
	if err != nil {
		return err
	}
	refundID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
        INSERT INTO biz_time_card_sale_refunds (asset_id,refund_id,amount_cents) VALUES (?,?,?)
    `, assetID, refundID, amount); err != nil {
		return err
	}
	if err := creditTimeCardRefundTx(ctx, tx, tenantID, refundID, 0, amount, "cash", timeCardActivationRefund, orderNo, reason); err != nil {
		return err
	}
	if amount > 0 {
		status := "partially_refunded"
		if refunded+amount == paid {
			status = "refunded"
		}
		if !strings.Contains("|paid|fulfilled|completed|partially_refunded|", "|"+orderStatus+"|") {
			return fmt.Errorf("order %d is not refundable", orderID)
		}
		if _, err := tx.ExecContext(ctx, "UPDATE biz_orders SET refunded_amount_cents=?,status=? WHERE id=?", refunded+amount, status, orderID); err != nil {
			return err
		}
		// Use the existing earnings reversal so a refunded purchase cannot
		// still release commissions or promotional rewards.
		if err := reverseOrderIncentivesForRefundTx(ctx, tx, orderID, refundID, refundNo, amount, paid); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
        UPDATE biz_time_card_assets SET status='refunded', remaining_seconds=0,
            expires_at=COALESCE(expires_at,activation_deadline_at),updated_at=? WHERE id=?
    `, now, assetID); err != nil {
		return err
	}
	return tx.Commit()
}
