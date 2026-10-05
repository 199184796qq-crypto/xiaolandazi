package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func testWechatNativeRechargeMySQL(t *testing.T, s *Store, identity model.WechatPaymentIdentity) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES(13,'customer',1,'wx-native-test','Native test','active')`); err != nil {
		t.Fatal(err)
	}
	item, err := s.CreateWechatRecharge(ctx, 13, 904, 123, "native-integration-key-001", "wechat_native")
	if err != nil || item.PaymentMethod != "wechat_native" {
		t.Fatal("create Native", item, err)
	}
	if _, err := s.CreateWechatRecharge(ctx, 13, 904, 123, "native-integration-key-001"); !errors.Is(err, ErrWechatRechargeConflict) {
		t.Fatal("method changed on idempotent request", err)
	}
	if _, err := s.CreateWechatRecharge(ctx, 13, 904, 123, "native-integration-key-002", "manual"); !errors.Is(err, ErrWechatRechargeInput) {
		t.Fatal("unknown method accepted", err)
	}
	if _, err := s.PrepareWechatRechargePayment(ctx, 13, 904, item.ID, identity); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("JSAPI reused Native recharge", err)
	}
	nativeIdentity := model.WechatPaymentIdentity{AppID: identity.AppID, MchID: identity.MchID}
	if _, err := s.PrepareWechatNativeRechargePayment(ctx, 11, 902, item.ID, nativeIdentity); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant native", err)
	}
	p, err := s.PrepareWechatNativeRechargePayment(ctx, 13, 904, item.ID, nativeIdentity)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.PrepareWechatNativeRechargePayment(ctx, 13, 904, item.ID, nativeIdentity)
	if err != nil || retry.PaymentID != p.PaymentID {
		t.Fatal("duplicate Native attempt", err)
	}
	codeURL := "weixin://wxpay/bizpayurl?pr=fixture-native"
	for n := 0; n < 2; n++ {
		if err := s.SaveWechatNativeCodeURL(ctx, p.PaymentID, codeURL); err != nil {
			t.Fatal("QR persistence/idempotency", err)
		}
	}
	if err := s.SaveWechatNativeCodeURL(ctx, p.PaymentID, codeURL+"different"); !errors.Is(err, ErrWechatPaymentNotPending) {
		t.Fatal("QR snapshot overwritten", err)
	}
	retry, err = s.PrepareWechatNativeRechargePayment(ctx, 13, 904, item.ID, nativeIdentity)
	if err != nil || retry.CodeURL != codeURL || scalar(`SELECT COUNT(*) FROM fin_wallet_ledger WHERE tenant_id=13`) != 0 {
		t.Fatal("prepay changed wallet or lost QR", err)
	}
	result := model.WechatPaymentResult{AppID: identity.AppID, MchID: identity.MchID, PayerOpenID: "native-scanner", OutTradeNo: p.PaymentNo,
		TransactionID: fmt.Sprintf("native-txn-%d", p.PaymentID), TradeState: "SUCCESS", TradeType: "NATIVE", Currency: "CNY", AmountCents: 123, SuccessTime: time.Now().UTC()}
	for _, mutate := range []func(*model.WechatPaymentResult){
		func(v *model.WechatPaymentResult) { v.AmountCents++ }, func(v *model.WechatPaymentResult) { v.MchID = "wrong" },
		func(v *model.WechatPaymentResult) { v.AppID = "wrong" }, func(v *model.WechatPaymentResult) { v.PayerOpenID = "" },
		func(v *model.WechatPaymentResult) { v.TradeType = "JSAPI" }, func(v *model.WechatPaymentResult) { v.Currency = "USD" },
	} {
		bad := result
		mutate(&bad)
		if err := s.CompleteVerifiedWechatPayment(ctx, bad); !errors.Is(err, ErrWechatPaymentMismatch) {
			t.Fatal("mismatched Native settlement accepted", err)
		}
	}
	var payerHash string
	if err := s.db.QueryRowContext(ctx, `SELECT provider_payer_hash FROM fin_payment_transactions WHERE id=?`, p.PaymentID).Scan(&payerHash); err != nil || payerHash != "" {
		t.Fatal("unverified payer bound", err)
	}
	results := make(chan error, 8)
	for n := 0; n < 8; n++ {
		go func() { results <- s.CompleteVerifiedWechatPayment(ctx, result) }()
	}
	for n := 0; n < 8; n++ {
		if err := <-results; err != nil {
			t.Fatal("concurrent Native settlement", err)
		}
	}
	if scalar(`SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=13 AND account_type='cash'`) != 123 ||
		scalar(`SELECT COUNT(*) FROM fin_wallet_ledger WHERE tenant_id=13 AND business_type='recharge'`) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_customer_receipts WHERE tenant_id=13 AND recharge_id=?`, item.ID) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE business_id=? AND business_type='recharge' AND payment_method='wechat_native'`, item.ID) != 1 {
		t.Fatal("Native credited multiple times or missing postings")
	}
	if err := s.db.QueryRowContext(ctx, `SELECT provider_payer_hash FROM fin_payment_transactions WHERE id=?`, p.PaymentID).Scan(&payerHash); err != nil || payerHash != hashWechatOAuthState("native-scanner") {
		t.Fatal("verified payer not bound for refund", err)
	}
	bad := result
	bad.PayerOpenID = "different-scanner"
	if err := s.CompleteVerifiedWechatPayment(ctx, bad); !errors.Is(err, ErrWechatPaymentMismatch) {
		t.Fatal("verified payer changed", err)
	}
	if err := s.MigrateWechatRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	wallet, err := s.GetWechatRefundWallet(ctx, 13, identity.AppID, identity.MchID)
	if err != nil || wallet.RefundableCents != 123 {
		t.Fatal("Native not refundable", wallet, err)
	}
	verification := []model.WechatRefundPaymentVerification{{PaymentNo: p.PaymentNo, TransactionID: result.TransactionID, AppID: identity.AppID, MchID: identity.MchID,
		OpenID: result.PayerOpenID, TotalCents: 123, PayerCents: 123, PayerAmountKnown: true, SuccessTime: result.SuccessTime}}
	refund, err := s.CreateWechatCashRefund(ctx, 13, 904, 23, "native-refund-fixture-key", identity.AppID, identity.MchID, verification)
	if err != nil || len(refund.Items) != 1 || refund.Items[0].PayerHash != payerHash {
		t.Fatal("Native original-route refund snapshot", refund, err)
	}
	// Wallet reads may race with a verified callback, but cash/frozen/status
	// must always come from the same wallet-locked snapshot. No provider call.
	dashboard, err := s.GetFinanceDashboard(ctx, 13, 100)
	if err != nil || dashboard.CashBalanceCents != 100 {
		t.Fatal("dashboard during refund", dashboard, err)
	}
	wallet, err = s.GetWechatRefundWallet(ctx, 13, identity.AppID, identity.MchID)
	if err != nil || wallet.AvailableCents != 100 || wallet.FrozenCents != 23 || len(wallet.Records) != 1 || wallet.Records[0].FrozenCents != 23 {
		t.Fatal("processing wallet snapshot", wallet, err)
	}
	part := refund.Items[0]
	settled := model.WechatRefundResult{RefundNo: part.RefundNo, ProviderRefundID: fmt.Sprintf("native-provider-refund-%d", part.ID), PaymentNo: part.PaymentNo,
		TransactionID: part.TransactionID, MchID: part.MchID, Currency: "CNY", Status: "SUCCESS", TotalCents: part.TotalCents,
		RefundCents: part.AmountCents, PayerTotalCents: part.TotalCents, PayerRefundCents: part.AmountCents, SuccessTime: time.Now().UTC()}
	callback := make(chan error, 1)
	go func() { callback <- s.CompleteVerifiedWechatRefund(ctx, settled) }()
	for n := 0; n < 20; n++ {
		w, e := s.GetWechatRefundWallet(ctx, 13, identity.AppID, identity.MchID)
		if e != nil || len(w.Records) != 1 {
			t.Fatal("concurrent wallet read", w, e)
		}
		if w.AvailableCents != 100 || w.FrozenCents != w.Records[0].FrozenCents {
			t.Fatal("mixed refund snapshot", w)
		}
	}
	if err := <-callback; err != nil {
		t.Fatal("verified fixture callback", err)
	}
	wallet, err = s.GetWechatRefundWallet(ctx, 13, identity.AppID, identity.MchID)
	if err != nil || wallet.FrozenCents != 0 || wallet.Records[0].Status != "success" || wallet.Records[0].RefundedCents != 23 || wallet.AvailableCents != 100 {
		t.Fatal("completed wallet snapshot", wallet, err)
	}
	dashboard, err = s.GetFinanceDashboard(ctx, 13, 100)
	if err != nil || dashboard.CashBalanceCents != 100 {
		t.Fatal("dashboard after refund", dashboard, err)
	}
}
