package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestWechatRechargeInput(t *testing.T) {
	for _, amount := range []uint64{0, MaxWechatRechargeCents + 1, math.MaxUint64} {
		if ValidateWechatRechargeInput(amount, "valid-recharge-key-123") == nil {
			t.Fatal("invalid amount accepted")
		}
	}
	for _, key := range []string{"", "short", "key-with-newline\n123", "non-ascii-中文-123456"} {
		if ValidateWechatRechargeInput(1, key) == nil {
			t.Fatal("invalid key accepted")
		}
	}
	for _, amount := range []uint64{1, 100, MaxWechatRechargeCents} {
		if err := ValidateWechatRechargeInput(amount, "valid-recharge-key-123"); err != nil {
			t.Fatal(err)
		}
	}
}

// Called inside the guarded, random-prefixed MySQL fixture. No provider is
// contacted; signed-provider result snapshots are synthetic test inputs only.
func testWechatRechargeMySQL(t *testing.T, s *Store, identity model.WechatPaymentIdentity) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var v int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	balance := func() int64 {
		return scalar(`SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10 AND account_type='cash'`)
	}
	initialBalance := balance()
	initialReceipts := scalar(`SELECT COUNT(*) FROM fin_customer_receipts`)
	initialApprovals := scalar(`SELECT COUNT(*) FROM staff_approval_tasks`)
	item, err := s.CreateWechatRecharge(ctx, 10, 901, 1, "recharge-test-request-001")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.CreateWechatRecharge(ctx, 10, 901, 1, "recharge-test-request-001")
	if err != nil || item.ID != retry.ID {
		t.Fatal("create retry duplicated recharge", err)
	}
	if _, err := s.CreateWechatRecharge(ctx, 10, 901, 2, "recharge-test-request-001"); !errors.Is(err, ErrWechatRechargeConflict) {
		t.Fatal("client key amount changed", err)
	}
	if _, err := s.GetWechatRecharge(ctx, 11, item.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant recharge read", err)
	}
	if _, err := s.PrepareWechatRechargePayment(ctx, 11, 902, item.ID, identity); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant prepay", err)
	}
	attempt, err := s.PrepareWechatRechargePayment(ctx, 10, 901, item.ID, identity)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := s.PrepareWechatRechargePayment(ctx, 10, 901, item.ID, identity)
	if err != nil || reused.PaymentID != attempt.PaymentID || attempt.OrderID != 0 || attempt.RechargeID != item.ID || attempt.AmountCents != 1 {
		t.Fatal("prepay snapshot/idempotency", err)
	}
	if balance() != initialBalance || scalar(`SELECT COUNT(*) FROM staff_approval_tasks`) != initialApprovals {
		t.Fatal("unpaid recharge credited or queued for approval")
	}
	if _, err := s.CompleteWechatShopPayment(ctx, model.WechatPaymentResult{OutTradeNo: attempt.PaymentNo}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("recharge ID used as shop order", err)
	}
	result := model.WechatPaymentResult{AppID: identity.AppID, MchID: identity.MchID, PayerOpenID: identity.OpenID, OutTradeNo: attempt.PaymentNo,
		TransactionID: fmt.Sprintf("recharge-txn-%d", attempt.PaymentID), TradeState: "SUCCESS", Currency: "CNY", AmountCents: 1, SuccessTime: time.Now().UTC()}
	for _, mutate := range []func(*model.WechatPaymentResult){
		func(v *model.WechatPaymentResult) { v.AmountCents++ }, func(v *model.WechatPaymentResult) { v.Currency = "USD" },
		func(v *model.WechatPaymentResult) { v.AppID = "wrong" }, func(v *model.WechatPaymentResult) { v.MchID = "wrong" },
		func(v *model.WechatPaymentResult) { v.PayerOpenID = "wrong" }, func(v *model.WechatPaymentResult) { v.TransactionID = "" },
		func(v *model.WechatPaymentResult) { v.TradeState = "NOTPAY" }, func(v *model.WechatPaymentResult) { v.SuccessTime = time.Time{} },
	} {
		bad := result
		mutate(&bad)
		if err := s.CompleteVerifiedWechatPayment(ctx, bad); !errors.Is(err, ErrWechatPaymentMismatch) {
			t.Fatal("payment mismatch accepted", err)
		}
	}
	if balance() != initialBalance {
		t.Fatal("mismatch changed wallet")
	}
	// Callback and query races produce only one wallet credit and one receipt.
	errorsCh := make(chan error, 8)
	for n := 0; n < 8; n++ {
		go func() { errorsCh <- s.CompleteVerifiedWechatPayment(ctx, result) }()
	}
	for n := 0; n < 8; n++ {
		if err := <-errorsCh; err != nil {
			t.Fatal("concurrent settlement", err)
		}
	}
	if balance() != initialBalance+1 || scalar(`SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_type='recharge' AND business_id=?`, item.ID) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_customer_receipts`) != initialReceipts+1 || scalar(`SELECT COUNT(*) FROM fin_customer_receipts WHERE recharge_id=? AND payment_id=? AND status='posted'`, item.ID, attempt.PaymentID) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE business_type='recharge' AND business_id=? AND category='customer_recharge'`, item.ID) != 1 {
		t.Fatal("duplicate or missing financial postings")
	}
	paid, err := s.GetWechatRecharge(ctx, 10, item.ID)
	if err != nil || paid.Status != "paid" || paid.CreditedAmountCents != 1 {
		t.Fatal("recharge not marked paid", err)
	}
	if _, err := s.PrepareWechatRechargePayment(ctx, 10, 901, item.ID, identity); !errors.Is(err, ErrShopOrderAlreadyPaid) {
		t.Fatal("paid recharge can be paid again", err)
	}
	// An expired/closed attempt cannot be silently replaced until verified closed.
	second, err := s.CreateWechatRecharge(ctx, 10, 901, 100, "recharge-test-request-002")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.PrepareWechatRechargePayment(ctx, 10, 901, second.ID, identity)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE fin_payment_transactions SET expires_at=UTC_TIMESTAMP(3)-INTERVAL 1 MINUTE WHERE id=?`, old.PaymentID)
	expired, err := s.PrepareWechatRechargePayment(ctx, 10, 901, second.ID, identity)
	if err != nil || expired.PaymentID != old.PaymentID {
		t.Fatal("expired bill replaced without close", err)
	}
	if err := s.UpdateWechatTradeState(ctx, old.PaymentNo, "CLOSED"); err != nil {
		t.Fatal(err)
	}
	newAttempt, err := s.PrepareWechatRechargePayment(ctx, 10, 901, second.ID, identity)
	if err != nil || newAttempt.PaymentID == old.PaymentID {
		t.Fatal("verified close did not allow retry", err)
	}
	makeSecond := func(attempt model.WechatPaymentPreparation) model.WechatPaymentResult {
		v := result
		v.OutTradeNo = attempt.PaymentNo
		v.TransactionID = fmt.Sprintf("recharge-txn-%d", attempt.PaymentID)
		v.AmountCents = 100
		return v
	}
	// A duplicate provider transaction ID across two local bills cannot credit.
	duplicate := makeSecond(newAttempt)
	duplicate.TransactionID = result.TransactionID
	if err := s.CompleteVerifiedWechatPayment(ctx, duplicate); err == nil || balance() != initialBalance+1 {
		t.Fatal("provider transaction reused across bills")
	}
	// Rollback on overflow must restore payment, recharge, income and receipt.
	exec(`UPDATE fin_wallet_accounts SET balance_cents=? WHERE tenant_id=10 AND account_type='cash'`, int64(math.MaxInt64))
	if err := s.CompleteVerifiedWechatPayment(ctx, makeSecond(newAttempt)); err == nil {
		t.Fatal("wallet overflow accepted")
	}
	if scalar(`SELECT paid_amount_cents FROM fin_payment_transactions WHERE id=?`, newAttempt.PaymentID) != 0 || scalar(`SELECT credited_amount_cents FROM fin_recharge_orders WHERE id=?`, second.ID) != 0 ||
		scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE business_type='recharge' AND business_id=?`, second.ID) != 0 {
		t.Fatal("partial failed settlement committed")
	}
	exec(`UPDATE fin_wallet_accounts SET balance_cents=? WHERE tenant_id=10 AND account_type='cash'`, initialBalance+1)
	if err := s.CompleteVerifiedWechatPayment(ctx, makeSecond(newAttempt)); err != nil {
		t.Fatal(err)
	}
	// A late SUCCESS for the previous attempt is still real income, but does not
	// credit this already-paid recharge twice. It is retained for finance review.
	if err := s.CompleteVerifiedWechatPayment(ctx, makeSecond(old)); err != nil {
		t.Fatal("late real income discarded", err)
	}
	if err := s.CompleteVerifiedWechatPayment(ctx, makeSecond(old)); err != nil {
		t.Fatal("late callback idempotency", err)
	}
	if balance() != initialBalance+101 || scalar(`SELECT COUNT(*) FROM fin_payment_transactions WHERE id=? AND status='paid_unapplied'`, old.PaymentID) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_type='recharge' AND business_id=?`, second.ID) != 1 {
		t.Fatal("late payment duplicated wallet credit")
	}
	// Background compensation must include recharge attempts, not only shop.
	third, err := s.CreateWechatRecharge(ctx, 11, 902, 200, "recharge-test-request-003")
	if err != nil {
		t.Fatal(err)
	}
	thirdAttempt, err := s.PrepareWechatRechargePayment(ctx, 11, 902, third.ID, identity)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE fin_payment_transactions SET created_at=UTC_TIMESTAMP(3)-INTERVAL 1 MINUTE WHERE id=?`, thirdAttempt.PaymentID)
	items, err := s.ListPendingWechatPayments(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, pending := range items {
		found = found || pending.PaymentNo == thirdAttempt.PaymentNo
	}
	if !found {
		t.Fatal("recharge missing from background reconciliation")
	}
	thirdResult := makeSecond(thirdAttempt)
	thirdResult.AmountCents = 200
	if err := s.CompleteVerifiedWechatPayment(ctx, thirdResult); err != nil {
		t.Fatal("new wallet creation", err)
	}
	if scalar(`SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=11 AND account_type='cash'`) != 200 {
		t.Fatal("new cash wallet not credited")
	}
}
