package db

import (
	"context"
	"database/sql"
	"livecompanion/management/internal/model"
	"strings"
)

// Each union branch is tenant-bound. A receipt replaces the corresponding source
// display row, not the underlying append-only ledger. No sum of unlike flows.
const customerMoneyUnion = `
 SELECT CONCAT('receipt:',r.id) entry_key,CONCAT('receipt_',r.purpose) kind,r.receipt_no reference_no,CAST(r.amount_cents AS SIGNED) amount_cents,'in' direction,r.channel,r.status,r.created_at occurred_at,r.posted_at,r.review_note note
 FROM fin_customer_receipts r WHERE r.tenant_id=?
 UNION ALL
 SELECT CONCAT('recharge:',r.id),'recharge',r.recharge_no,CAST(r.requested_amount_cents AS SIGNED),'in',r.payment_method,CASE WHEN r.status='paid' THEN 'historical_posted' ELSE r.status END,r.created_at,r.paid_at,'充值原单；客户认定以财务确认记录为准'
 FROM fin_recharge_orders r WHERE r.tenant_id=? AND NOT EXISTS(SELECT 1 FROM fin_customer_receipts receipt WHERE receipt.recharge_id=r.id OR receipt.existing_recharge_id=r.id)
 UNION ALL
 SELECT CONCAT('payment:',p.id),'order_payment',p.order_no,CAST(p.paid_amount_cents AS SIGNED),'out',p.channel,CASE WHEN p.channel='sandbox' THEN 'simulated' ELSE p.status END,p.created_at,p.paid_at,'订单支付记录；不重复增加钱包余额'
 FROM fin_payment_transactions p WHERE p.tenant_id=? AND NOT EXISTS(SELECT 1 FROM fin_customer_receipts receipt WHERE receipt.payment_id=p.id OR receipt.verified_payment_id=p.id)
 UNION ALL
 SELECT CONCAT('wallet:',w.id),CONCAT('wallet_',w.business_type),COALESCE(w.order_no,w.external_id),CAST(w.amount_cents AS SIGNED),CASE WHEN w.direction='credit' THEN 'in' ELSE 'out' END,'wallet','posted',w.occurred_at,w.occurred_at,'钱包变动；不是新外部收款'
 FROM fin_wallet_ledger w WHERE w.tenant_id=? AND w.business_type NOT IN ('recharge','refund')
 UNION ALL
 SELECT CONCAT('refund:',r.id),'refund',r.refund_no,CAST(r.refund_amount_cents AS SIGNED),'in',r.refund_method,r.status,r.created_at,r.processed_at,'退款关联原业务；不抹去历史客户认定'
 FROM fin_refund_orders r WHERE r.tenant_id=?`

func (s *Store) ListCustomerMoney(ctx context.Context, sc model.CustomerBusinessScope, tenant int64, kind, status, search string, from, to *string, page, size int) ([]model.CustomerMoneyEntry, int64, error) {
	if err := checkCustomerBusinessAccess(ctx, s.db, sc, tenant); err != nil {
		return nil, 0, err
	}
	page, size = businessPage(page, size)
	where := " WHERE 1=1"
	args := []any{tenant, tenant, tenant, tenant, tenant}
	if kind != "" && kind != "all" {
		where += " AND kind=?"
		args = append(args, kind)
	}
	if status != "" && status != "all" {
		where += " AND status=?"
		args = append(args, status)
	}
	if strings.TrimSpace(search) != "" {
		where += " AND reference_no LIKE ? ESCAPE '!'"
		args = append(args, businessLike(search))
	}
	if from != nil {
		where += " AND occurred_at>=?"
		args = append(args, *from)
	}
	if to != nil {
		where += " AND occurred_at<?"
		args = append(args, *to)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	base := " FROM (" + customerMoneyUnion + ") money" + where
	var total int64
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*)"+base, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT entry_key,kind,reference_no,amount_cents,direction,channel,status,occurred_at,posted_at,note"+base+" ORDER BY occurred_at DESC,entry_key DESC LIMIT ? OFFSET ?", append(append([]any{}, args...), size, int64(page-1)*int64(size))...)
	if err != nil {
		return nil, 0, err
	}
	out := []model.CustomerMoneyEntry{}
	for rows.Next() {
		var v model.CustomerMoneyEntry
		if err = rows.Scan(&v.EntryKey, &v.Kind, &v.ReferenceNo, &v.AmountCents, &v.Direction, &v.Channel, &v.Status, &v.OccurredAt, &v.PostedAt, &v.Note); err != nil {
			rows.Close()
			return nil, 0, err
		}
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	return out, total, tx.Commit()
}
