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

const MaxWechatRechargeCents uint64 = math.MaxInt32

var ErrWechatRechargeInput = errors.New("invalid recharge amount or idempotency key")
var ErrWechatRechargeConflict = errors.New("recharge request conflicts with existing request")

func ValidateWechatRechargeInput(amount uint64, key string) error {
	if amount == 0 || amount > MaxWechatRechargeCents || len(key) < 16 || len(key) > 96 {
		return ErrWechatRechargeInput
	}
	for _, ch := range key {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || strings.ContainsRune("-_:.", ch)) {
			return ErrWechatRechargeInput
		}
	}
	return nil
}

const wechatRechargeSelect = `SELECT id,recharge_no,requested_amount_cents,credited_amount_cents,payment_method,status,paid_at,created_at
	FROM fin_recharge_orders WHERE tenant_id=? AND id=? AND payment_method IN ('wechat_jsapi','wechat_native')`

func scanWechatRecharge(row interface{ Scan(...any) error }) (model.RechargeRecord, error) {
	var item model.RechargeRecord
	err := row.Scan(&item.ID, &item.RechargeNo, &item.RequestedAmountCents, &item.CreditedAmountCents,
		&item.PaymentMethod, &item.Status, &item.PaidAt, &item.CreatedAt)
	return item, err
}

func (s *Store) GetWechatRecharge(ctx context.Context, tenantID, id int64) (model.RechargeRecord, error) {
	return scanWechatRecharge(s.db.QueryRowContext(ctx, wechatRechargeSelect, tenantID, id))
}

// Creating a recharge never credits money or creates an approval task. Reusing
// the same client key after a timeout returns exactly the same immutable amount.
func (s *Store) CreateWechatRecharge(ctx context.Context, tenantID, userID int64, amount uint64, key string, methods ...string) (model.RechargeRecord, error) {
	method := "wechat_jsapi"
	if len(methods) > 1 {
		return model.RechargeRecord{}, ErrWechatRechargeInput
	}
	if len(methods) == 1 && methods[0] != "" {
		method = methods[0]
	}
	if method != "wechat_jsapi" && method != "wechat_native" {
		return model.RechargeRecord{}, ErrWechatRechargeInput
	}
	if tenantID <= 0 || userID <= 0 || ValidateWechatRechargeInput(amount, key) != nil {
		return model.RechargeRecord{}, ErrWechatRechargeInput
	}
	key = fmt.Sprintf("wx-recharge:%d:%s", tenantID, hashWechatOAuthState(key))
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.RechargeRecord{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM mgmt_tenants WHERE id=? FOR UPDATE`, tenantID).Scan(&id); err != nil {
		return model.RechargeRecord{}, err
	}
	var storedAmount uint64
	var storedMethod string
	err = tx.QueryRowContext(ctx, `SELECT id,requested_amount_cents,payment_method FROM fin_recharge_orders WHERE tenant_id=? AND idempotency_key=?`, tenantID, key).Scan(&id, &storedAmount, &storedMethod)
	if err == nil {
		if storedAmount != amount || storedMethod != method {
			return model.RechargeRecord{}, ErrWechatRechargeConflict
		}
		_ = tx.Rollback()
		return s.GetWechatRecharge(ctx, tenantID, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return model.RechargeRecord{}, err
	}
	no, err := newFinanceReference("WRC")
	if err != nil {
		return model.RechargeRecord{}, err
	}
	r, err := tx.ExecContext(ctx, `INSERT INTO fin_recharge_orders(recharge_no,tenant_id,currency,requested_amount_cents,payment_method,status,operator_user_id,idempotency_key)
		VALUES (?,?,'CNY',?,?,'pending',?,?)`, no, tenantID, amount, method, userID, key)
	if err != nil {
		return model.RechargeRecord{}, err
	}
	id, err = r.LastInsertId()
	if err != nil {
		return model.RechargeRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.RechargeRecord{}, err
	}
	return s.GetWechatRecharge(ctx, tenantID, id)
}

func (s *Store) PrepareWechatRechargePayment(ctx context.Context, tenantID, userID, rechargeID int64, identity model.WechatPaymentIdentity) (model.WechatPaymentPreparation, error) {
	return s.prepareWechatRechargePayment(ctx, tenantID, userID, rechargeID, identity, "wechat_jsapi")
}

func (s *Store) PrepareWechatNativeRechargePayment(ctx context.Context, tenantID, userID, rechargeID int64, identity model.WechatPaymentIdentity) (model.WechatPaymentPreparation, error) {
	return s.prepareWechatRechargePayment(ctx, tenantID, userID, rechargeID, identity, "wechat_native")
}

func (s *Store) prepareWechatRechargePayment(ctx context.Context, tenantID, userID, rechargeID int64, identity model.WechatPaymentIdentity, method string) (model.WechatPaymentPreparation, error) {
	item := model.WechatPaymentPreparation{RechargeID: rechargeID, OrderType: "recharge", Currency: "CNY", Description: "小蓝直播搭子钱包充值"}
	if identity.AppID == "" || identity.MchID == "" || (method == "wechat_jsapi" && identity.OpenID == "") || (method == "wechat_native" && identity.OpenID != "") {
		return item, ErrWechatPaymentMismatch
	}
	expectedPayerHash := ""
	if method == "wechat_jsapi" {
		expectedPayerHash = hashWechatOAuthState(identity.OpenID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return item, err
	}
	defer tx.Rollback()
	var status, currency string
	if err := tx.QueryRowContext(ctx, `SELECT recharge_no,requested_amount_cents,status,currency FROM fin_recharge_orders
		WHERE id=? AND tenant_id=? AND payment_method=? FOR UPDATE`, rechargeID, tenantID, method).Scan(&item.OrderNo, &item.AmountCents, &status, &currency); err != nil {
		return item, err
	}
	if status == "paid" {
		return item, ErrShopOrderAlreadyPaid
	}
	if status != "pending" || currency != "CNY" || item.AmountCents == 0 || item.AmountCents > MaxWechatRechargeCents {
		return item, ErrWechatPaymentMismatch
	}
	var appID, mchID, payerHash, storedMethod string
	err = tx.QueryRowContext(ctx, `SELECT id,payment_no,provider_prepay_id,provider_code_url,expires_at,provider_app_id,provider_mch_id,provider_payer_hash,payment_method
		FROM fin_payment_transactions WHERE tenant_id=? AND recharge_order_id=? AND order_id=0 AND channel='wechat' AND status='pending'
		ORDER BY id DESC LIMIT 1 FOR UPDATE`, tenantID, rechargeID).Scan(&item.PaymentID, &item.PaymentNo, &item.PrepayID, &item.CodeURL, &item.ExpiresAt, &appID, &mchID, &payerHash, &storedMethod)
	if err == nil {
		if appID != identity.AppID || mchID != identity.MchID || payerHash != expectedPayerHash || storedMethod != method {
			return item, ErrWechatPaymentMismatch
		}
		return item, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return item, err
	}
	item.PaymentNo, err = newFinanceReference("WXP")
	if err != nil {
		return item, err
	}
	item.ExpiresAt = time.Now().UTC().Add(15 * time.Minute)
	// order_id=0 is a non-shop sentinel: legacy order_id is NOT NULL. Explicit
	// recharge_order_id keeps recharge IDs disjoint from all biz_orders IDs.
	r, err := tx.ExecContext(ctx, `INSERT INTO fin_payment_transactions(payment_no,tenant_id,order_id,recharge_order_id,order_no,channel,payment_method,currency,
		expected_amount_cents,input_amount_cents,paid_amount_cents,status,operator_user_id,idempotency_key,expires_at,provider_app_id,provider_mch_id,provider_payer_hash)
		VALUES (?,?,0,?,?,'wechat',?,'CNY',?,?,0,'pending',?,?,?,?,?,?)`, item.PaymentNo, tenantID, rechargeID, item.OrderNo, method,
		item.AmountCents, item.AmountCents, userID, method+":"+item.PaymentNo, item.ExpiresAt, identity.AppID, identity.MchID, expectedPayerHash)
	if err != nil {
		return item, err
	}
	item.PaymentID, err = r.LastInsertId()
	if err != nil {
		return item, err
	}
	return item, tx.Commit()
}

func (s *Store) GetPendingWechatRechargePayment(ctx context.Context, tenantID, rechargeID int64) (model.WechatPaymentPreparation, error) {
	var item model.WechatPaymentPreparation
	err := s.db.QueryRowContext(ctx, `SELECT id,payment_no,recharge_order_id,order_no,expires_at FROM fin_payment_transactions
		WHERE tenant_id=? AND recharge_order_id=? AND order_id=0 AND channel='wechat' AND status='pending' ORDER BY id DESC LIMIT 1`, tenantID, rechargeID).
		Scan(&item.PaymentID, &item.PaymentNo, &item.RechargeID, &item.OrderNo, &item.ExpiresAt)
	return item, err
}

func (s *Store) CompleteVerifiedWechatPayment(ctx context.Context, result model.WechatPaymentResult) error {
	var rechargeID sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT recharge_order_id FROM fin_payment_transactions WHERE payment_no=? AND channel='wechat'`, result.OutTradeNo).Scan(&rechargeID); err != nil {
		return err
	}
	if rechargeID.Valid {
		return s.CompleteWechatRechargePayment(ctx, result)
	}
	_, err := s.CompleteWechatShopPayment(ctx, result)
	return err
}

// Lock order is recharge -> payment -> wallet. Callback and active query share
// this transaction; balances, append-only ledger, receipts and income commit
// together or all roll back. A duplicate notification does not credit again.
func (s *Store) CompleteWechatRechargePayment(ctx context.Context, result model.WechatPaymentResult) error {
	var paymentID, rechargeID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id,recharge_order_id FROM fin_payment_transactions WHERE payment_no=? AND channel='wechat' AND order_id=0`, result.OutTradeNo).Scan(&paymentID, &rechargeID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tenantID, userID int64
	var no, status, currency, rechargeMethod string
	var amount, credited uint64
	if err := tx.QueryRowContext(ctx, `SELECT tenant_id,recharge_no,status,currency,requested_amount_cents,credited_amount_cents,operator_user_id,payment_method FROM fin_recharge_orders
		WHERE id=? AND payment_method IN ('wechat_jsapi','wechat_native') FOR UPDATE`, rechargeID).Scan(&tenantID, &no, &status, &currency, &amount, &credited, &userID, &rechargeMethod); err != nil {
		return err
	}
	var pstatus, pno, pcurrency, appID, mchID, payerHash, paymentMethod string
	var pamount uint64
	var providerID sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT status,order_no,currency,expected_amount_cents,provider_app_id,provider_mch_id,provider_payer_hash,provider_transaction_id,payment_method
		FROM fin_payment_transactions WHERE id=? AND tenant_id=? AND recharge_order_id=? AND order_id=0 AND channel='wechat' FOR UPDATE`, paymentID, tenantID, rechargeID).
		Scan(&pstatus, &pno, &pcurrency, &pamount, &appID, &mchID, &payerHash, &providerID, &paymentMethod); err != nil {
		return err
	}
	if result.TradeState != "SUCCESS" || strings.TrimSpace(result.TransactionID) == "" || result.SuccessTime.IsZero() ||
		result.AppID != appID || result.MchID != mchID || result.Currency != "CNY" || currency != "CNY" || pcurrency != "CNY" ||
		result.AmountCents != amount || pamount != amount || pno != no || amount == 0 || amount > MaxWechatRechargeCents ||
		result.PayerOpenID == "" || paymentMethod != rechargeMethod ||
		(paymentMethod == "wechat_native" && result.TradeType != "NATIVE") ||
		(paymentMethod == "wechat_jsapi" && hashWechatOAuthState(result.PayerOpenID) != payerHash) ||
		(paymentMethod == "wechat_native" && payerHash != "" && hashWechatOAuthState(result.PayerOpenID) != payerHash) {
		return ErrWechatPaymentMismatch
	}
	if pstatus == "paid" || pstatus == "paid_unapplied" {
		if providerID.Valid && providerID.String == result.TransactionID {
			return nil
		}
		return ErrWechatPaymentMismatch
	}
	if pstatus != "pending" && pstatus != "closed" {
		return ErrWechatPaymentMismatch
	}
	// Native learns the payer only from a verified SUCCESS, never from the
	// browser. Bind once under the payment lock for original-route refunds.
	if paymentMethod == "wechat_native" && payerHash == "" {
		if _, err := tx.ExecContext(ctx, `UPDATE fin_payment_transactions SET provider_payer_hash=? WHERE id=?`, hashWechatOAuthState(result.PayerOpenID), paymentID); err != nil {
			return err
		}
	}
	postedStatus, failureReason := "paid", ""
	unapplied := status != "pending" || credited != 0
	if unapplied {
		postedStatus, failureReason = "paid_unapplied", "recharge_not_pending_requires_review"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE fin_payment_transactions SET paid_amount_cents=?,status=?,failure_reason=?,external_trade_no=?,
		provider_transaction_id=?,provider_trade_state='SUCCESS',paid_at=?,notified_at=UTC_TIMESTAMP(3) WHERE id=?`,
		amount, postedStatus, failureReason, result.TransactionID, result.TransactionID, result.SuccessTime.UTC(), paymentID); err != nil {
		return err
	}
	category, reason := "customer_recharge", "微信钱包充值 · "+no
	if unapplied {
		category, reason = "unapplied_wechat_payment", "微信已收款但充值单状态异常，请财务核查 · "+no
	}
	var counterparty string
	if err := tx.QueryRowContext(ctx, `SELECT name FROM mgmt_tenants WHERE id=?`, tenantID).Scan(&counterparty); err != nil {
		return err
	}
	if err := insertOperatingEntryTx(ctx, tx, "income", category, amount, "recharge", &rechargeID, no, "wechat_payment:"+result.TransactionID,
		counterparty, paymentMethod, reason, userID, result.SuccessTime.UTC()); err != nil {
		return err
	}
	if !unapplied {
		_, before, err := lockWalletAccountTx(ctx, tx, tenantID, "cash")
		if err != nil {
			return err
		}
		if before > math.MaxInt64-int64(amount) {
			return ErrWechatPaymentMismatch
		}
		if err := applyRechargeTx(ctx, tx, tenantID, rechargeID, no, amount, userID, "微信支付到账 · "+result.TransactionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fin_recharge_orders SET external_trade_no=?,paid_at=? WHERE id=?`, result.TransactionID, result.SuccessTime.UTC(), rechargeID); err != nil {
			return err
		}
		if err := recordWechatPlatformReceiptTx(ctx, tx, tenantID, nil, &rechargeID, paymentID, userID, result); err != nil {
			return err
		}
	}
	// Recharge itself earns no referral commission; product purchase handles it.
	return s.commitInboxTx(ctx, tx, "finance", "sales")
}
