package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestRefundPrincipalReplay(t *testing.T) {
	now := time.Now().UTC()
	lots := []*refundLot{{WechatCashRefundItem: model.WechatCashRefundItem{RechargeID: 1, TotalCents: 1000, AppID: "wx", MchID: "m", PayerHash: "payer"}, PaidAt: now.Add(-time.Hour)}}
	events := []refundLedgerEvent{{Before: 500, After: 1500, Amount: 1000, Business: "recharge", BusinessID: 1}, {Before: 1500, After: 2000, Amount: 500, Business: "gift"}, {Before: 2000, After: 1700, Amount: 300, Business: "purchase"}}
	if err := replayRefundPrincipal(lots, events, nil, nil, 1700); err != nil || lots[0].Remaining != 700 {
		t.Fatal("gift/spend promoted to principal", lots[0].Remaining, err)
	}
	part := model.WechatCashRefundItem{ID: 3, RequestID: 2, RechargeID: 1, AmountCents: 400, Status: "closed"}
	events = append(events, refundLedgerEvent{Before: 1700, After: 1300, Amount: 400, Business: "wechat_refund_hold", BusinessID: 2}, refundLedgerEvent{Before: 1300, After: 1700, Amount: 400, Business: "wechat_refund_release", BusinessID: 3})
	if err := replayRefundPrincipal(lots, events, map[int64][]model.WechatCashRefundItem{2: {part}}, map[int64]model.WechatCashRefundItem{3: part}, 1700); err != nil || lots[0].Remaining != 700 {
		t.Fatal("closed refund lost original source", err)
	}
	if err := replayRefundPrincipal(lots, events, map[int64][]model.WechatCashRefundItem{2: {part}}, map[int64]model.WechatCashRefundItem{3: part}, 1500); err != nil || lots[0].Remaining != 500 {
		t.Fatal("missing downward history ignored", err)
	}
	if _, _, err := selectRefundLots(lots, 1, "wx", "wrong", now); !errors.Is(err, ErrWechatRefundUnavailable) {
		t.Fatal("wrong merchant eligible")
	}
	lots[0].PaidAt = now.Add(-365 * 24 * time.Hour)
	if _, _, err := selectRefundLots(lots, 1, "wx", "m", now); err == nil {
		t.Fatal("expired payment eligible")
	}
	lots[0].PaidAt = now.Add(-time.Hour)
	lots[0].Count = 50
	if _, _, err := selectRefundLots(lots, 1, "wx", "m", now); err == nil {
		t.Fatal("50 refund limit ignored")
	}
	lots[0].Count = 1
	lots[0].Latest = now.Add(-30 * time.Second)
	if _, _, err := selectRefundLots(lots, 1, "wx", "m", now); err == nil {
		t.Fatal("one-minute gap ignored")
	}
	events[0].Amount = 999
	if err := replayRefundPrincipal(lots, events, nil, nil, 1700); !errors.Is(err, ErrWechatRefundHistory) {
		t.Fatal("corrupt ledger accepted")
	}
}

func TestCashManualWithdrawalDisabled(t *testing.T) {
	if _, err := new(Store).CreateCustomerWalletWithdrawal(context.Background(), 1, 1, "cash", 100); !errors.Is(err, ErrCashWithdrawalUseRefund) {
		t.Fatal("old cash withdrawal bypass remains open", err)
	}
}

// Synthetic provider snapshots; no WeChat API is contacted. The enclosing
// fixture rewrites every table to a random isolated prefix before execution.
func testWechatCashRefundMySQL(t *testing.T, s *Store, identity model.WechatPaymentIdentity) {
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
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if err := s.MigrateWechatRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.MigrateWechatRefunds(ctx); err != nil {
		t.Fatal("migration not repeatable", err)
	}
	exec(`INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES(12,'customer',1,'wx-refund-test','Refund test','active')`)
	verification := []model.WechatRefundPaymentVerification{}
	topup := func(amount uint64, key string) {
		t.Helper()
		r, err := s.CreateWechatRecharge(ctx, 12, 903, amount, key)
		if err != nil {
			t.Fatal(err)
		}
		p, err := s.PrepareWechatRechargePayment(ctx, 12, 903, r.ID, identity)
		if err != nil {
			t.Fatal(err)
		}
		txn := fmt.Sprintf("refund-test-txn-%d", p.PaymentID)
		now := time.Now().UTC()
		err = s.CompleteWechatRechargePayment(ctx, model.WechatPaymentResult{AppID: identity.AppID, MchID: identity.MchID, PayerOpenID: identity.OpenID, OutTradeNo: p.PaymentNo, TransactionID: txn, TradeState: "SUCCESS", Currency: "CNY", AmountCents: amount, SuccessTime: now})
		if err != nil {
			t.Fatal(err)
		}
		verification = append(verification, model.WechatRefundPaymentVerification{PaymentNo: p.PaymentNo, TransactionID: txn, AppID: identity.AppID, MchID: identity.MchID, OpenID: identity.OpenID, TotalCents: amount, PayerCents: amount, PayerAmountKnown: true, SuccessTime: now})
	}
	topup(1000, "refund-integration-topup-one")
	topup(500, "refund-integration-topup-two")
	balance := func() int64 {
		return scalar(`SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=12 AND account_type='cash'`)
	}
	frozen := func() int64 {
		return scalar(`SELECT frozen_balance_cents FROM fin_wallet_accounts WHERE tenant_id=12 AND account_type='cash'`)
	}
	spend := func(amount uint64, no string) error {
		tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err != nil {
			return err
		}
		defer tx.Rollback()
		wallet, before, err := lockWalletAccountTx(ctx, tx, 12, "cash")
		if err != nil {
			return err
		}
		if before < int64(amount) {
			return ErrInsufficientWalletBalance
		}
		_, err = tx.ExecContext(ctx, `UPDATE fin_wallet_accounts SET balance_cents=balance_cents-? WHERE id=?`, amount, wallet)
		if err != nil {
			return err
		}
		if err = refundLedgerTx(ctx, tx, 12, wallet, 903, 1, "test_cash_purchase", no, amount, before, before-int64(amount)); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := spend(200, "refund-test-purchase"); err != nil {
		t.Fatal(err)
	}
	w, err := s.GetWechatRefundWallet(ctx, 12, identity.AppID, identity.MchID)
	if err != nil || w.RefundableCents != 1300 {
		t.Fatal("unspent principal", w, err)
	}
	bad := append([]model.WechatRefundPaymentVerification(nil), verification...)
	bad[0].PayerCents = 900
	if _, err = s.CreateWechatCashRefund(ctx, 12, 903, 1300, "refund-test-invalid-coupon", identity.AppID, identity.MchID, bad); !errors.Is(err, ErrWechatRefundConflict) {
		t.Fatal("coupon refund accepted", err)
	}
	if balance() != 1300 || frozen() != 0 {
		t.Fatal("rejected request changed funds")
	}
	r, err := s.CreateWechatCashRefund(ctx, 12, 903, 1300, "refund-test-request-first", identity.AppID, identity.MchID, verification)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != 2 || r.Items[0].AmountCents != 800 || r.Items[1].AmountCents != 500 || balance() != 0 || frozen() != 1300 {
		t.Fatal("split/hold incorrect", r, balance(), frozen())
	}
	again, err := s.CreateWechatCashRefund(ctx, 12, 903, 1300, "refund-test-request-first", identity.AppID, identity.MchID, verification)
	if err != nil || again.ID != r.ID || frozen() != 1300 {
		t.Fatal("duplicate request", err)
	}
	if _, err = s.CreateWechatCashRefund(ctx, 12, 903, 1299, "refund-test-request-first", identity.AppID, identity.MchID, verification); !errors.Is(err, ErrWechatRefundConflict) {
		t.Fatal("changed key amount", err)
	}
	if _, err = s.GetWechatCashRefund(ctx, 11, r.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant read", err)
	}
	var wg sync.WaitGroup
	claims := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.ClaimWechatRefundItem(ctx, r.Items[0].ID); claims <- e }()
	}
	wg.Wait()
	close(claims)
	winners := 0
	for e := range claims {
		if e == nil {
			winners++
		} else if !errors.Is(e, sql.ErrNoRows) {
			t.Fatal(e)
		}
	}
	if winners != 1 {
		t.Fatal("submission lease not exclusive", winners)
	}
	r, err = s.GetWechatCashRefund(ctx, 12, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkWechatRefundSubmission(ctx, r.Items[0]); err != nil {
		t.Fatal(err)
	}
	if err = s.DeferWechatRefundItem(ctx, r.Items[0]); err != nil {
		t.Fatal(err)
	}
	if balance() != 0 || frozen() != 1300 {
		t.Fatal("transport timeout released frozen money")
	}
	if _, err = s.ClaimWechatRefundItem(ctx, r.Items[0].ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("retry backoff bypassed", err)
	}
	exec(`UPDATE fin_wechat_cash_refund_items SET next_check_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 1 MINUTE) WHERE id=?`, r.Items[0].ID)
	claim, err := s.ClaimWechatRefundItem(ctx, r.Items[0].ID)
	if err != nil || claim.RefundNo != r.Items[0].RefundNo || claim.Attempts != 2 {
		t.Fatal("retry minted different provider refund", claim, err)
	}
	result := func(i model.WechatCashRefundItem, status string) model.WechatRefundResult {
		return model.WechatRefundResult{RefundNo: i.RefundNo, ProviderRefundID: fmt.Sprintf("provider-refund-%d", i.ID), PaymentNo: i.PaymentNo, TransactionID: i.TransactionID, MchID: i.MchID, Currency: "CNY", Status: status, TotalCents: i.TotalCents, RefundCents: i.AmountCents, PayerTotalCents: i.TotalCents, PayerRefundCents: i.AmountCents, SuccessTime: time.Now().UTC(), ReceivedAccount: "支付用户零钱"}
	}
	for _, mutate := range []func(*model.WechatRefundResult){func(v *model.WechatRefundResult) { v.TotalCents++ }, func(v *model.WechatRefundResult) { v.RefundCents++ }, func(v *model.WechatRefundResult) { v.TransactionID = "wrong" }, func(v *model.WechatRefundResult) { v.MchID = "wrong" }, func(v *model.WechatRefundResult) { v.PayerRefundCents-- }, func(v *model.WechatRefundResult) { v.SuccessTime = time.Time{} }} {
		v := result(r.Items[0], "SUCCESS")
		mutate(&v)
		if e := s.CompleteVerifiedWechatRefund(ctx, v); !errors.Is(e, ErrWechatRefundMismatch) {
			t.Fatal("forged/mismatched settlement", e)
		}
	}
	if frozen() != 1300 {
		t.Fatal("mismatch released money")
	}
	v := result(r.Items[0], "PROCESSING")
	if err = s.CompleteVerifiedWechatRefund(ctx, v); err != nil {
		t.Fatal(err)
	}
	if frozen() != 1300 || balance() != 0 {
		t.Fatal("acceptance credited or settled money")
	}
	v = result(r.Items[0], "SUCCESS")
	out := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); out <- s.CompleteVerifiedWechatRefund(ctx, v) }()
	}
	wg.Wait()
	close(out)
	for e := range out {
		if e != nil {
			t.Fatal("concurrent callback", e)
		}
	}
	if frozen() != 500 || balance() != 0 || scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE business_type='wechat_cash_refund' AND business_id=?`, r.Items[0].ID) != 1 {
		t.Fatal("success double debited or expense duplicated")
	}
	if err = s.CompleteVerifiedWechatRefund(ctx, result(r.Items[1], "ABNORMAL")); err != nil {
		t.Fatal(err)
	}
	if err = s.CompleteVerifiedWechatRefund(ctx, result(r.Items[1], "PROCESSING")); err != nil {
		t.Fatal(err)
	}
	r, err = s.GetWechatCashRefund(ctx, 12, r.ID)
	if err != nil || r.Status != "abnormal" || r.FrozenCents != 500 || balance() != 0 {
		t.Fatal("abnormal funds unlocked", r, err)
	}
	for n := 0; n < 2; n++ {
		closed := result(r.Items[1], "CLOSED")
		closed.PayerRefundCents = 0
		if err = s.CompleteVerifiedWechatRefund(ctx, closed); err != nil {
			t.Fatal(err)
		}
	}
	r, err = s.GetWechatCashRefund(ctx, 12, r.ID)
	if err != nil || r.Status != "partially_refunded" || r.RefundedCents != 800 || r.ReleasedCents != 500 || frozen() != 0 || balance() != 500 {
		t.Fatal("closed refund release", r, err)
	}
	exec(`UPDATE fin_wechat_cash_refund_items SET created_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 2 MINUTE),submitted_at=DATE_SUB(UTC_TIMESTAMP(3),INTERVAL 2 MINUTE) WHERE request_id=?`, r.ID)
	w, err = s.GetWechatRefundWallet(ctx, 12, identity.AppID, identity.MchID)
	if err != nil || w.RefundableCents != 500 {
		t.Fatal("released original principal eligibility", w, err)
	}
	topup(600, "refund-integration-topup-three")
	start := make(chan struct{})
	race := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, e := s.CreateWechatCashRefund(ctx, 12, 903, 900, "refund-concurrent-cash-request", identity.AppID, identity.MchID, verification)
		race <- e
	}()
	go func() { defer wg.Done(); <-start; race <- spend(400, "refund-test-concurrent-spend") }()
	close(start)
	wg.Wait()
	close(race)
	winners = 0
	for e := range race {
		if e == nil {
			winners++
		} else if !errors.Is(e, ErrWechatRefundUnavailable) && !errors.Is(e, ErrInsufficientWalletBalance) {
			t.Fatal("concurrent refund/spend", e)
		}
	}
	if winners != 1 || balance() < 0 || frozen() < 0 {
		t.Fatal("concurrent refund and spend overspent", winners, balance(), frozen())
	}
}
