package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"livecompanion/management/internal/model"
)

var (
	ErrWechatRefundUnavailable = errors.New("insufficient refundable recharge principal")
	ErrWechatRefundConflict    = errors.New("refund request changed or conflicts with existing request")
	ErrWechatRefundHistory     = errors.New("wallet source history cannot be verified")
	ErrWechatRefundMismatch    = errors.New("provider refund does not match frozen allocation")
)

type refundLot struct {
	model.WechatCashRefundItem
	PaidAt    time.Time
	Remaining uint64
	Count     int
	Active    int
	Latest    time.Time
}

type refundLedgerEvent struct {
	Before, After int64
	Amount        uint64
	Business      string
	BusinessID    int64
}

// Cash consumption conservatively spends recharge principal first, FIFO.
// Other credits (gifts, offline adjustments, product refunds) are never promoted
// to refundable principal. Missing downward history consumes principal too.
// Holds/releases preserve the EXACT source allocation, including payment age.
func replayRefundPrincipal(lots []*refundLot, events []refundLedgerEvent, holds map[int64][]model.WechatCashRefundItem, releases map[int64]model.WechatCashRefundItem, balance int64) error {
	byID := map[int64]*refundLot{}
	seen := map[int64]bool{}
	for _, lot := range lots {
		byID[lot.RechargeID] = lot
		lot.Remaining = 0
	}
	consume := func(amount uint64) {
		for _, lot := range lots {
			n := min(amount, lot.Remaining)
			lot.Remaining -= n
			amount -= n
			if amount == 0 {
				break
			}
		}
	}
	var previous int64
	for _, e := range events {
		if e.Before < 0 || e.After < 0 || e.Amount > math.MaxInt64 {
			return ErrWechatRefundHistory
		}
		if previous > e.Before {
			consume(uint64(previous - e.Before))
		}
		delta := e.After - e.Before
		if delta != 0 && uint64(absRefundDelta(delta)) != e.Amount {
			return ErrWechatRefundHistory
		}
		switch e.Business {
		case "recharge":
			if lot := byID[e.BusinessID]; lot != nil {
				if seen[e.BusinessID] || delta <= 0 || uint64(delta) != lot.TotalCents {
					return ErrWechatRefundHistory
				}
				seen[e.BusinessID] = true
				lot.Remaining = uint64(delta)
			} else if delta < 0 {
				consume(uint64(-delta))
			}
		case "wechat_refund_hold":
			var sum uint64
			for _, item := range holds[e.BusinessID] {
				lot := byID[item.RechargeID]
				if lot == nil || lot.Remaining < item.AmountCents {
					return ErrWechatRefundHistory
				}
				lot.Remaining -= item.AmountCents
				sum += item.AmountCents
			}
			if delta >= 0 || sum != uint64(-delta) {
				return ErrWechatRefundHistory
			}
		case "wechat_refund_release":
			item, ok := releases[e.BusinessID]
			lot := byID[item.RechargeID]
			if !ok || lot == nil || delta <= 0 || uint64(delta) != item.AmountCents || item.Status != "closed" || lot.Remaining > lot.TotalCents-item.AmountCents {
				return ErrWechatRefundHistory
			}
			lot.Remaining += item.AmountCents
		default:
			if delta < 0 {
				consume(uint64(-delta))
			}
		}
		previous = e.After
	}
	if balance < 0 {
		return ErrWechatRefundHistory
	}
	if previous > balance {
		consume(uint64(previous - balance))
	}
	return nil
}

func absRefundDelta(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// wallet row MUST be locked before reading history. All cash debits/credits use
// this same row lock, so eligibility, allocation and freezing are atomic.
func refundLotsTx(ctx context.Context, tx *sql.Tx, tenantID, walletID, balance int64) ([]*refundLot, error) {
	rows, err := tx.QueryContext(ctx, `
	 SELECT r.id,r.recharge_no,p.payment_no,p.provider_transaction_id,p.provider_app_id,p.provider_mch_id,p.provider_payer_hash,p.expected_amount_cents,p.paid_at,
	 (SELECT COUNT(*) FROM fin_wechat_cash_refund_items i WHERE i.recharge_id=r.id),
	 (SELECT MAX(COALESCE(i.submitted_at,i.created_at)) FROM fin_wechat_cash_refund_items i WHERE i.recharge_id=r.id),
	 (SELECT COUNT(*) FROM fin_wechat_cash_refund_items i WHERE i.recharge_id=r.id AND i.status IN ('queued','processing','abnormal'))
	 FROM fin_recharge_orders r JOIN fin_payment_transactions p ON p.recharge_order_id=r.id
	 WHERE r.tenant_id=? AND r.status='paid' AND r.currency='CNY' AND r.credited_amount_cents=r.requested_amount_cents
	 AND p.order_id=0 AND p.channel='wechat' AND p.status='paid' AND p.currency='CNY'
	 AND p.expected_amount_cents=r.credited_amount_cents AND p.paid_amount_cents=p.expected_amount_cents
	 AND p.provider_transaction_id=r.external_trade_no AND p.provider_transaction_id<>'' AND p.paid_at IS NOT NULL
	 ORDER BY r.id`, tenantID)
	if err != nil {
		return nil, err
	}
	lots := []*refundLot{}
	for rows.Next() {
		lot := new(refundLot)
		var latest sql.NullTime
		err = rows.Scan(&lot.RechargeID, &lot.RechargeNo, &lot.PaymentNo, &lot.TransactionID, &lot.AppID, &lot.MchID, &lot.PayerHash, &lot.TotalCents, &lot.PaidAt, &lot.Count, &latest, &lot.Active)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if lot.TotalCents == 0 || lot.TotalCents > MaxWechatRechargeCents {
			rows.Close()
			return nil, ErrWechatRefundHistory
		}
		lot.Latest = latest.Time
		lots = append(lots, lot)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Reorder sources by their actual credit-ledger sequence, not bank timestamps.
	creditOrder := map[int64]int{}
	rows, err = tx.QueryContext(ctx, `SELECT balance_before_cents,balance_after_cents,amount_cents,business_type,COALESCE(business_id,0) FROM fin_wallet_ledger WHERE wallet_account_id=? AND tenant_id=? ORDER BY id`, walletID, tenantID)
	if err != nil {
		return nil, err
	}
	events := []refundLedgerEvent{}
	for rows.Next() {
		var e refundLedgerEvent
		if err = rows.Scan(&e.Before, &e.After, &e.Amount, &e.Business, &e.BusinessID); err != nil {
			rows.Close()
			return nil, err
		}
		if e.Business == "recharge" {
			creditOrder[e.BusinessID] = len(events)
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Stable insertion sort; missing credits remain zero and cannot be refunded.
	for i := 1; i < len(lots); i++ {
		for j := i; j > 0 && creditOrder[lots[j].RechargeID] < creditOrder[lots[j-1].RechargeID]; j-- {
			lots[j], lots[j-1] = lots[j-1], lots[j]
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT i.id,i.request_id,i.recharge_id,i.amount_cents,i.status FROM fin_wechat_cash_refund_items i JOIN fin_wechat_cash_refunds r ON r.id=i.request_id WHERE r.wallet_account_id=? AND r.tenant_id=?`, walletID, tenantID)
	if err != nil {
		return nil, err
	}
	holds := map[int64][]model.WechatCashRefundItem{}
	releases := map[int64]model.WechatCashRefundItem{}
	for rows.Next() {
		var item model.WechatCashRefundItem
		if err = rows.Scan(&item.ID, &item.RequestID, &item.RechargeID, &item.AmountCents, &item.Status); err != nil {
			rows.Close()
			return nil, err
		}
		holds[item.RequestID] = append(holds[item.RequestID], item)
		releases[item.ID] = item
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = replayRefundPrincipal(lots, events, holds, releases, balance); err != nil {
		return nil, err
	}
	return lots, nil
}

func selectRefundLots(lots []*refundLot, amount uint64, appID, mchID string, now time.Time) ([]model.WechatCashRefundItem, uint64, error) {
	var available uint64
	selected := []model.WechatCashRefundItem{}
	left := amount
	for _, lot := range lots {
		if lot.AppID != appID || lot.MchID != mchID || lot.PayerHash == "" || lot.PaidAt.After(now) || !lot.PaidAt.Add(365*24*time.Hour).After(now) || lot.Count >= 50 || lot.Active > 0 || (!lot.Latest.IsZero() && lot.Latest.Add(time.Minute).After(now)) {
			continue
		}
		available += lot.Remaining
		if left > 0 && lot.Remaining > 0 {
			item := lot.WechatCashRefundItem
			item.AmountCents = min(left, lot.Remaining)
			selected = append(selected, item)
			left -= item.AmountCents
		}
	}
	if left > 0 || len(selected) > 20 {
		return nil, available, ErrWechatRefundUnavailable
	}
	return selected, available, nil
}

func refundRequestKey(tenantID int64, key string) string {
	return fmt.Sprintf("cash-refund:%d:%s", tenantID, hashWechatOAuthState(key))
}

func (s *Store) FindWechatCashRefund(ctx context.Context, tenantID int64, amount uint64, key string) (model.WechatCashRefund, error) {
	var id int64
	var saved uint64
	err := s.db.QueryRowContext(ctx, `SELECT id,amount_cents FROM fin_wechat_cash_refunds WHERE tenant_id=? AND idempotency_key=?`, tenantID, refundRequestKey(tenantID, key)).Scan(&id, &saved)
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	if amount != saved {
		return model.WechatCashRefund{}, ErrWechatRefundConflict
	}
	return s.GetWechatCashRefund(ctx, tenantID, id)
}

func (s *Store) PlanWechatCashRefund(ctx context.Context, tenantID int64, amount uint64, appID, mchID string) ([]model.WechatCashRefundItem, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	walletID, balance, err := lockWalletAccountTx(ctx, tx, tenantID, "cash")
	if err != nil {
		return nil, err
	}
	if amount == 0 || amount > MaxWechatRechargeCents || balance < int64(amount) {
		return nil, ErrWechatRefundUnavailable
	}
	lots, err := refundLotsTx(ctx, tx, tenantID, walletID, balance)
	if err != nil {
		return nil, err
	}
	items, _, err := selectRefundLots(lots, amount, appID, mchID, time.Now().UTC())
	return items, err
}

func (s *Store) CreateWechatCashRefund(ctx context.Context, tenantID, userID int64, amount uint64, key, appID, mchID string, verified []model.WechatRefundPaymentVerification) (model.WechatCashRefund, error) {
	if err := ValidateWechatRechargeInput(amount, key); err != nil {
		return model.WechatCashRefund{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	defer tx.Rollback()
	walletID, balance, err := lockWalletAccountTx(ctx, tx, tenantID, "cash")
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	var existing int64
	var saved uint64
	err = tx.QueryRowContext(ctx, `SELECT id,amount_cents FROM fin_wechat_cash_refunds WHERE tenant_id=? AND idempotency_key=?`, tenantID, refundRequestKey(tenantID, key)).Scan(&existing, &saved)
	if err == nil {
		if saved != amount {
			return model.WechatCashRefund{}, ErrWechatRefundConflict
		}
		tx.Rollback()
		return s.GetWechatCashRefund(ctx, tenantID, existing)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.WechatCashRefund{}, err
	}
	if balance < int64(amount) {
		return model.WechatCashRefund{}, ErrWechatRefundUnavailable
	}
	lots, err := refundLotsTx(ctx, tx, tenantID, walletID, balance)
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	items, _, err := selectRefundLots(lots, amount, appID, mchID, time.Now().UTC())
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	for _, item := range items {
		ok := false
		for _, v := range verified {
			if v.PaymentNo == item.PaymentNo && v.TransactionID == item.TransactionID && v.AppID == item.AppID && v.MchID == item.MchID && hashWechatOAuthState(v.OpenID) == item.PayerHash && v.TotalCents == item.TotalCents && v.PayerAmountKnown && v.PayerCents == v.TotalCents && !v.SuccessTime.IsZero() && !v.SuccessTime.After(time.Now().UTC()) && v.SuccessTime.Add(365*24*time.Hour).After(time.Now().UTC()) {
				ok = true
				break
			}
		}
		if !ok {
			return model.WechatCashRefund{}, ErrWechatRefundConflict
		}
	}
	no, err := newFinanceReference("WCR")
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO fin_wechat_cash_refunds(refund_no,tenant_id,wallet_account_id,user_id,amount_cents,idempotency_key) VALUES(?,?,?,?,?,?)`, no, tenantID, walletID, userID, amount, refundRequestKey(tenantID, key))
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	for _, item := range items {
		itemNo, e := newFinanceReference("WRF")
		if e != nil {
			return model.WechatCashRefund{}, e
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fin_wechat_cash_refund_items(request_id,recharge_id,recharge_no,payment_no,transaction_id,app_id,mch_id,payer_hash,total_cents,amount_cents,refund_no) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, id, item.RechargeID, item.RechargeNo, item.PaymentNo, item.TransactionID, item.AppID, item.MchID, item.PayerHash, item.TotalCents, item.AmountCents, itemNo)
		if err != nil {
			return model.WechatCashRefund{}, err
		}
	}
	res, err = tx.ExecContext(ctx, `UPDATE fin_wallet_accounts SET balance_cents=balance_cents-?,frozen_balance_cents=frozen_balance_cents+? WHERE id=? AND status='active' AND currency='CNY' AND balance_cents>=? AND frozen_balance_cents>=0 AND frozen_balance_cents<=?`, amount, amount, walletID, amount, int64(math.MaxInt64)-int64(amount))
	if err != nil {
		return model.WechatCashRefund{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return model.WechatCashRefund{}, ErrWechatRefundUnavailable
	}
	if err = refundLedgerTx(ctx, tx, tenantID, walletID, userID, id, "wechat_refund_hold", no, amount, balance, balance-int64(amount)); err != nil {
		return model.WechatCashRefund{}, err
	}
	if err = s.commitInboxTx(ctx, tx, "finance"); err != nil {
		return model.WechatCashRefund{}, err
	}
	return s.GetWechatCashRefund(ctx, tenantID, id)
}

func refundLedgerTx(ctx context.Context, tx *sql.Tx, tenantID, walletID, userID, businessID int64, business, no string, amount uint64, before, after int64) error {
	direction := "debit"
	if after > before {
		direction = "credit"
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO fin_wallet_ledger(external_id,tenant_id,wallet_account_id,direction,amount_cents,balance_before_cents,balance_after_cents,business_type,business_id,order_no,operator_user_id,reason,idempotency_key,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,UTC_TIMESTAMP(3))`, business+":"+no, tenantID, walletID, direction, amount, before, after, business, businessID, no, userID, "微信充值本金原路退回", business+":"+no)
	return err
}

const refundItemSelect = `SELECT i.id,i.request_id,r.tenant_id,r.wallet_account_id,i.refund_no,i.recharge_id,i.recharge_no,i.payment_no,i.transaction_id,i.app_id,i.mch_id,i.payer_hash,i.total_cents,i.amount_cents,i.status,i.received_account,i.message,i.success_time,i.attempts,i.lease_token FROM fin_wechat_cash_refund_items i JOIN fin_wechat_cash_refunds r ON r.id=i.request_id `

type refundScanner interface{ Scan(...any) error }

func scanRefundItem(row refundScanner) (model.WechatCashRefundItem, error) {
	var i model.WechatCashRefundItem
	var success sql.NullTime
	err := row.Scan(&i.ID, &i.RequestID, &i.TenantID, &i.WalletID, &i.RefundNo, &i.RechargeID, &i.RechargeNo, &i.PaymentNo, &i.TransactionID, &i.AppID, &i.MchID, &i.PayerHash, &i.TotalCents, &i.AmountCents, &i.Status, &i.ReceivedAccount, &i.Message, &success, &i.Attempts, &i.LeaseToken)
	if success.Valid {
		i.SuccessTime = &success.Time
	}
	return i, err
}

func summarizeCashRefund(r *model.WechatCashRefund) {
	r.RefundedCents = 0
	r.ReleasedCents = 0
	r.Status = "processing"
	for _, i := range r.Items {
		switch i.Status {
		case "success":
			r.RefundedCents += i.AmountCents
		case "closed":
			r.ReleasedCents += i.AmountCents
		case "abnormal":
			r.Status = "abnormal"
		}
	}
	r.FrozenCents = r.AmountCents - r.RefundedCents - r.ReleasedCents
	if r.FrozenCents == 0 {
		if r.RefundedCents == r.AmountCents {
			r.Status = "success"
		} else if r.ReleasedCents == r.AmountCents {
			r.Status = "closed"
		} else {
			r.Status = "partially_refunded"
		}
	}
}

func (s *Store) GetWechatCashRefund(ctx context.Context, tenantID, id int64) (model.WechatCashRefund, error) {
	return getWechatCashRefund(ctx, s.db, tenantID, id)
}

type refundReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func getWechatCashRefund(ctx context.Context, reader refundReader, tenantID, id int64) (model.WechatCashRefund, error) {
	var r model.WechatCashRefund
	err := reader.QueryRowContext(ctx, `SELECT id,refund_no,amount_cents,created_at FROM fin_wechat_cash_refunds WHERE id=? AND tenant_id=?`, id, tenantID).Scan(&r.ID, &r.RefundNo, &r.AmountCents, &r.CreatedAt)
	if err != nil {
		return r, err
	}
	rows, err := reader.QueryContext(ctx, refundItemSelect+`WHERE r.id=? AND r.tenant_id=? ORDER BY i.id`, id, tenantID)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	r.Items = []model.WechatCashRefundItem{}
	for rows.Next() {
		i, e := scanRefundItem(rows)
		if e != nil {
			return r, e
		}
		r.Items = append(r.Items, i)
	}
	if err = rows.Err(); err != nil {
		return r, err
	}
	summarizeCashRefund(&r)
	return r, nil
}

func (s *Store) GetWechatRefundWallet(ctx context.Context, tenantID int64, appID, mchID string) (model.WechatRefundWallet, error) {
	result := model.WechatRefundWallet{Records: []model.WechatCashRefund{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	walletID, balance, err := lockWalletAccountTx(ctx, tx, tenantID, "cash")
	if err != nil {
		return result, err
	}
	var frozen int64
	err = tx.QueryRowContext(ctx, `SELECT frozen_balance_cents FROM fin_wallet_accounts WHERE id=?`, walletID).Scan(&frozen)
	if err != nil {
		return result, err
	}
	if balance < 0 || frozen < 0 {
		return result, ErrWechatRefundHistory
	}
	result.AvailableCents = uint64(balance)
	result.FrozenCents = uint64(frozen)
	lots, err := refundLotsTx(ctx, tx, tenantID, walletID, balance)
	if err != nil {
		return result, err
	}
	_, available, err := selectRefundLots(lots, 0, appID, mchID, time.Now().UTC())
	if err != nil {
		return result, err
	}
	result.RefundableCents = min(available, result.AvailableCents)
	// Keep the wallet lock until records have been read. Settlement uses this
	// same lock, so a SUCCESS cannot be paired with the pre-settlement frozen sum.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM fin_wechat_cash_refunds WHERE tenant_id=? ORDER BY id DESC LIMIT 100`, tenantID)
	if err != nil {
		return result, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, id := range ids {
		r, e := getWechatCashRefund(ctx, tx, tenantID, id)
		if e != nil {
			return result, e
		}
		result.Records = append(result.Records, r)
	}
	return result, tx.Commit()
}

// A database lease serializes all callers and survives HTTP/process failures.
func (s *Store) ClaimWechatRefundItem(ctx context.Context, id int64) (model.WechatCashRefundItem, error) {
	token, err := newFinanceReference("LEASE")
	if err != nil {
		return model.WechatCashRefundItem{}, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE fin_wechat_cash_refund_items SET lease_token=?,lease_until=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 90 SECOND),attempts=attempts+1,next_check_at=DATE_ADD(UTC_TIMESTAMP(3),INTERVAL 1 MINUTE) WHERE id=? AND status IN ('queued','processing','abnormal') AND next_check_at<=UTC_TIMESTAMP(3) AND (lease_until IS NULL OR lease_until<UTC_TIMESTAMP(3))`, token, id)
	if err != nil {
		return model.WechatCashRefundItem{}, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return model.WechatCashRefundItem{}, sql.ErrNoRows
	}
	return scanRefundItem(s.db.QueryRowContext(ctx, refundItemSelect+`WHERE i.id=? AND i.lease_token=?`, id, token))
}

func (s *Store) ListStaffWechatCashRefunds(ctx context.Context) ([]model.WechatCashRefund, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id,r.tenant_id,t.name FROM fin_wechat_cash_refunds r JOIN mgmt_tenants t ON t.id=r.tenant_id ORDER BY r.id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	type ref struct {
		id, tenant int64
		name       string
	}
	refs := []ref{}
	for rows.Next() {
		var v ref
		if err = rows.Scan(&v.id, &v.tenant, &v.name); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []model.WechatCashRefund{}
	for _, v := range refs {
		r, e := s.GetWechatCashRefund(ctx, v.tenant, v.id)
		if e != nil {
			return nil, e
		}
		r.TenantID = v.tenant
		r.TenantName = v.name
		items = append(items, r)
	}
	return items, nil
}

func (s *Store) WechatCashRefundTenant(ctx context.Context, id int64) (int64, error) {
	var tenant int64
	err := s.db.QueryRowContext(ctx, `SELECT tenant_id FROM fin_wechat_cash_refunds WHERE id=?`, id).Scan(&tenant)
	return tenant, err
}

func refundPollDelay(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return time.Minute
	case attempts == 2:
		return 5 * time.Minute
	case attempts == 3:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

// Record the dispatch time before sending: retries and the next partial refund
// must observe the provider's one-minute spacing, not merely creation time.
func (s *Store) MarkWechatRefundSubmission(ctx context.Context, i model.WechatCashRefundItem) error {
	r, err := s.db.ExecContext(ctx, `UPDATE fin_wechat_cash_refund_items SET submitted_at=UTC_TIMESTAMP(3) WHERE id=? AND lease_token=? AND status='queued'`, i.ID, i.LeaseToken)
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) DeferWechatRefundItem(ctx context.Context, i model.WechatCashRefundItem) error {
	_, err := s.db.ExecContext(ctx, `UPDATE fin_wechat_cash_refund_items SET lease_until=NULL,lease_token='',next_check_at=?,message='微信退款结果待核验，金额保持冻结；请勿重复发起' WHERE id=? AND lease_token=? AND status IN ('queued','processing','abnormal')`, time.Now().UTC().Add(refundPollDelay(i.Attempts)), i.ID, i.LeaseToken)
	return err
}

func (s *Store) ListDueWechatRefundItems(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM fin_wechat_cash_refund_items WHERE status IN ('queued','processing','abnormal') AND next_check_at<=UTC_TIMESTAMP(3) AND (lease_until IS NULL OR lease_until<UTC_TIMESTAMP(3)) ORDER BY next_check_at,id LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func validateRefundResult(i model.WechatCashRefundItem, r model.WechatRefundResult) error {
	if r.RefundNo != i.RefundNo || r.ProviderRefundID == "" || r.PaymentNo != i.PaymentNo || r.TransactionID != i.TransactionID || (r.MchID != "" && r.MchID != i.MchID) || r.Currency != "CNY" || r.TotalCents != i.TotalCents || r.RefundCents != i.AmountCents || r.PayerTotalCents != i.TotalCents || (r.PayerRefundCents != 0 && r.PayerRefundCents != i.AmountCents) {
		return ErrWechatRefundMismatch
	}
	switch r.Status {
	case "SUCCESS":
		if r.SuccessTime.IsZero() || r.PayerRefundCents != i.AmountCents {
			return ErrWechatRefundMismatch
		}
	case "CLOSED", "PROCESSING", "ABNORMAL":
	default:
		return ErrWechatRefundMismatch
	}
	return nil
}

// Called ONLY with a verified SDK response or a signature-verified callback.
// Lock order is wallet -> request -> item; no network call occurs in this tx.
func (s *Store) CompleteVerifiedWechatRefund(ctx context.Context, r model.WechatRefundResult) error {
	i, err := scanRefundItem(s.db.QueryRowContext(ctx, refundItemSelect+`WHERE i.refund_no=?`, r.RefundNo))
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var balance, frozen int64
	if err = tx.QueryRowContext(ctx, `SELECT balance_cents,frozen_balance_cents FROM fin_wallet_accounts WHERE id=? AND tenant_id=? FOR UPDATE`, i.WalletID, i.TenantID).Scan(&balance, &frozen); err != nil {
		return err
	}
	var userID int64
	if err = tx.QueryRowContext(ctx, `SELECT user_id FROM fin_wechat_cash_refunds WHERE id=? FOR UPDATE`, i.RequestID).Scan(&userID); err != nil {
		return err
	}
	i, err = scanRefundItem(tx.QueryRowContext(ctx, refundItemSelect+`WHERE i.id=? FOR UPDATE`, i.ID))
	if err != nil {
		return err
	}
	if err = validateRefundResult(i, r); err != nil {
		return err
	}
	var saved sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT provider_refund_id FROM fin_wechat_cash_refund_items WHERE id=?`, i.ID).Scan(&saved); err != nil {
		return err
	}
	if saved.Valid && saved.String != r.ProviderRefundID {
		return ErrWechatRefundMismatch
	}
	state := strings.ToLower(r.Status)
	if i.Status == "success" || i.Status == "closed" {
		if i.Status != state && (state == "success" || state == "closed") {
			return ErrWechatRefundMismatch
		}
		return nil
	}
	// An older PROCESSING response must not erase an ABNORMAL notification.
	if i.Status == "abnormal" && state == "processing" {
		state = "abnormal"
	}
	if state == "success" || state == "closed" {
		if frozen < int64(i.AmountCents) || balance < 0 {
			return ErrWechatRefundHistory
		}
		after := balance
		if state == "closed" {
			if balance > math.MaxInt64-int64(i.AmountCents) {
				return ErrWechatRefundHistory
			}
			after += int64(i.AmountCents)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE fin_wallet_accounts SET balance_cents=?,frozen_balance_cents=frozen_balance_cents-? WHERE id=?`, after, i.AmountCents, i.WalletID); err != nil {
			return err
		}
		if state == "closed" {
			if err = refundLedgerTx(ctx, tx, i.TenantID, i.WalletID, userID, i.ID, "wechat_refund_release", i.RefundNo, i.AmountCents, balance, after); err != nil {
				return err
			}
		} else {
			// The hold already debited available cash. Only record the external
			// expense now; never debit the customer's balance a second time.
			if err = insertOperatingEntryTx(ctx, tx, "expense", "customer_recharge_refund", i.AmountCents, "wechat_cash_refund", &i.ID, i.RefundNo, "wechat_cash_refund:"+r.ProviderRefundID, "客户充值本金退回", "wechat_refund", "微信充值本金原路退回", userID, r.SuccessTime); err != nil {
				return err
			}
		}
	}
	var success any
	if state == "success" {
		success = r.SuccessTime.UTC()
	}
	message := "微信正在原路退回，请以实际到账为准"
	if state == "closed" {
		message = "微信已确认退款关闭，冻结金额已退回钱包"
	} else if state == "abnormal" {
		message = "退款账户异常，金额保持冻结，请联系客服处理"
	} else if state == "success" {
		message = "微信已确认原路退款成功"
	}
	_, err = tx.ExecContext(ctx, `UPDATE fin_wechat_cash_refund_items SET status=?,provider_refund_id=?,received_account=?,message=?,success_time=?,lease_until=NULL,lease_token='',next_check_at=? WHERE id=?`, state, r.ProviderRefundID, r.ReceivedAccount, message, success, time.Now().UTC().Add(refundPollDelay(i.Attempts)), i.ID)
	if err != nil {
		return err
	}
	return s.commitInboxTx(ctx, tx, "finance")
}
