package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

// Explicit MySQL opt-in, using the existing guarded random-schema/prefixed
// fixture. Never submits a payment or reads/writes production business rows.
func TestWechatPaymentMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	exec := func(query string, args ...any) sql.Result {
		t.Helper()
		r, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, column := range wechatPaymentColumnMigrations() {
		exec(column.sql)
	}
	exec(`ALTER TABLE fin_payment_transactions ADD UNIQUE KEY uk_fin_payment_transactions_provider_txn (channel,provider_transaction_id)`)
	if err := s.migrateWechatStateTables(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) > 5 && fields[0] == "CREATE" && (fields[5] == "quota_buckets" || fields[5] == "quota_ledger" || fields[5] == "biz_memberships" || fields[5] == "catalog_membership_plans" || fields[5] == "catalog_membership_plan_versions") {
			exec(statement)
		}
	}
	exec(`CREATE TABLE org_resource_ledger(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,organization_id BIGINT UNSIGNED,resource_type VARCHAR(64),change_quantity BIGINT,balance_before BIGINT,balance_after BIGINT,business_type VARCHAR(64),operator_user_id BIGINT UNSIGNED,reason VARCHAR(1024))`)
	for _, statement := range strings.Split(strings.ReplaceAll(operatingFinanceSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.HasPrefix(strings.TrimSpace(statement), "CREATE TABLE IF NOT EXISTS fin_operating_entries") {
			exec(statement)
		}
	}
	exec(`INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES(10,'customer',1,'wx-test','Wechat test','active'),(11,'customer',1,'wx-other','Other customer','active')`)
	exec(`INSERT INTO fin_wallet_accounts(tenant_id,account_type,currency,balance_cents) VALUES(10,'cash','CNY',10000)`)
	identity := model.WechatPaymentIdentity{AppID: "wx-test", MchID: "mch-test", OpenID: "openid-test"}
	makeOrder := func(no string, valid bool) int64 {
		t.Helper()
		id, _ := exec(`INSERT INTO biz_orders(order_no,tenant_id,order_type,status,currency,payable_amount_cents) VALUES(?,10,'time_card','pending','CNY',5000)`, no).LastInsertId()
		if valid {
			exec(`INSERT INTO biz_order_items(order_id,product_type,product_id,product_version_id,product_name_snapshot,quantity,duration_seconds_snapshot,validity_days_snapshot,activation_mode_snapshot,unit_paid_price_cents) VALUES(?,'time_card',1,1,'Test time card',2,3600,365,'first_use',2500)`, id)
		}
		return id
	}
	makeResult := func(attempt model.WechatPaymentPreparation) model.WechatPaymentResult {
		return model.WechatPaymentResult{AppID: identity.AppID, MchID: identity.MchID, PayerOpenID: identity.OpenID, OutTradeNo: attempt.PaymentNo, TransactionID: fmt.Sprintf("wx-txn-%d", attempt.PaymentID), TradeState: "SUCCESS", Currency: "CNY", AmountCents: 5000, SuccessTime: time.Now().UTC()}
	}
	orderID := makeOrder("WX-ORDER", true)
	if _, err := s.PrepareWechatShopPayment(ctx, 11, 901, orderID, identity); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-tenant payment allowed", err)
	}
	attempt, err := s.PrepareWechatShopPayment(ctx, 10, 901, orderID, identity)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempt.PaymentNo) > 32 || attempt.AmountCents != 5000 {
		t.Fatal("invalid server payment snapshot")
	}
	retry, err := s.PrepareWechatShopPayment(ctx, 10, 901, orderID, identity)
	if err != nil || retry.PaymentID != attempt.PaymentID {
		t.Fatal("prepay retry duplicated payment", err)
	}
	if err := s.SaveWechatPrepayID(ctx, attempt.PaymentID, "prepay-test"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWechatPrepayID(ctx, attempt.PaymentID, "prepay-test"); err != nil {
		t.Fatal("prepay idempotency", err)
	}
	if _, err := s.WalletPayCustomerTimeCardOrder(ctx, 10, 901, orderID, "wallet-no-double-pay"); !errors.Is(err, ErrWechatPaymentInProgress) {
		t.Fatal("wallet races wechat", err)
	}
	if _, err := s.CancelCustomerShopOrder(ctx, 10, orderID); !errors.Is(err, ErrWechatPaymentInProgress) {
		t.Fatal("cancel races wechat", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = postReceiptOrderTx(ctx, tx, &model.CustomerReceipt{CustomerReceiptInput: model.CustomerReceiptInput{TenantID: 10, OrderID: &orderID, AmountCents: 5000}}, 900)
	_ = tx.Rollback()
	if !errors.Is(err, ErrWechatPaymentInProgress) {
		t.Fatal("offline receipt races wechat", err)
	}
	result := makeResult(attempt)
	for _, change := range []func(*model.WechatPaymentResult){func(r *model.WechatPaymentResult) { r.AmountCents-- }, func(r *model.WechatPaymentResult) { r.AppID = "wrong" }, func(r *model.WechatPaymentResult) { r.MchID = "wrong" }, func(r *model.WechatPaymentResult) { r.PayerOpenID = "wrong" }, func(r *model.WechatPaymentResult) { r.TradeState = "NOTPAY" }} {
		invalid := result
		change(&invalid)
		if _, err := s.CompleteWechatShopPayment(ctx, invalid); !errors.Is(err, ErrWechatPaymentMismatch) {
			t.Fatal("mismatch accepted", err)
		}
	}
	if scalar(`SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=?`, orderID) != 0 {
		t.Fatal("unpaid order fulfilled")
	}
	// Two concurrent verified notifications must serialize on the order lock.
	errorsCh := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { _, err := s.CompleteWechatShopPayment(ctx, result); errorsCh <- err }()
	}
	for i := 0; i < 2; i++ {
		if err := <-errorsCh; err != nil {
			t.Fatal("concurrent notification", err)
		}
	}
	if scalar(`SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=? AND status='unactivated'`, orderID) != 2 ||
		scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE business_no='WX-ORDER'`) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_customer_receipts WHERE payment_id=? AND status='posted'`, attempt.PaymentID) != 1 ||
		scalar(`SELECT COUNT(*) FROM fin_customer_confirmations WHERE tenant_id=10`) != 1 ||
		scalar(`SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10 AND account_type='cash'`) != 10000 {
		t.Fatal("duplicate fulfillment, finance or wallet double credit")
	}
	// A changed order snapshot must roll back even a verified success.
	badID := makeOrder("WX-BAD", false)
	badAttempt, err := s.PrepareWechatShopPayment(ctx, 10, 901, badID, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteWechatShopPayment(ctx, makeResult(badAttempt)); err == nil {
		t.Fatal("missing item snapshot fulfilled")
	}
	if scalar(`SELECT paid_amount_cents FROM fin_payment_transactions WHERE id=?`, badAttempt.PaymentID) != 0 {
		t.Fatal("failed fulfillment partially committed")
	}
	// A success received after a verified close must still settle exactly once.
	lateID := makeOrder("WX-LATE", true)
	late, err := s.PrepareWechatShopPayment(ctx, 10, 901, lateID, identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateWechatTradeState(ctx, late.PaymentNo, "CLOSED"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteWechatShopPayment(ctx, makeResult(late)); err != nil {
		t.Fatal("late success lost", err)
	}
	if scalar(`SELECT COUNT(*) FROM fin_payment_transactions WHERE id=? AND status='paid'`, late.PaymentID) != 1 {
		t.Fatal("late payment row not updated")
	}
	// Cancellation by an older service/admin is persisted as unapplied real money,
	// visible to finance but never fulfilled or credited to the wallet.
	exceptionID := makeOrder("WX-EXCEPTION", true)
	exception, err := s.PrepareWechatShopPayment(ctx, 10, 901, exceptionID, identity)
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE biz_orders SET status='cancelled' WHERE id=?`, exceptionID)
	if _, err := s.CompleteWechatShopPayment(ctx, makeResult(exception)); err != nil {
		t.Fatal(err)
	}
	if scalar(`SELECT COUNT(*) FROM fin_payment_transactions WHERE id=? AND status='paid_unapplied'`, exception.PaymentID) != 1 || scalar(`SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=?`, exceptionID) != 0 || scalar(`SELECT COUNT(*) FROM fin_operating_entries WHERE category='unapplied_wechat_payment'`) != 1 {
		t.Fatal("late payment exception lost or fulfilled")
	}
	// One-off quarter payment grants three monthly buckets but must not enable
	// automatic wallet debits just because the catalog permits auto renewal.
	exec(`INSERT INTO catalog_membership_plan_versions(id,plan_id,version_no,allow_auto_renew,included_seconds) VALUES(9,9,1,1,600)`)
	memberID, _ := exec(`INSERT INTO biz_orders(order_no,tenant_id,order_type,status,currency,payable_amount_cents,pricing_snapshot_json) VALUES('WX-MEMBER',10,'membership','pending','CNY',5000,JSON_OBJECT('cycle','quarter'))`).LastInsertId()
	exec(`INSERT INTO biz_order_items(order_id,product_type,product_id,product_version_id,product_name_snapshot,quantity,duration_seconds_snapshot,unit_paid_price_cents,metadata_json) VALUES(?,'membership',9,9,'Test member',1,1800,5000,JSON_OBJECT('cycle','quarter','months',3))`, memberID)
	memberAttempt, err := s.PrepareWechatShopPayment(ctx, 10, 901, memberID, identity)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteWechatShopPayment(ctx, makeResult(memberAttempt)); err != nil {
		t.Fatal("membership settlement", err)
	}
	if scalar(`SELECT COUNT(*) FROM biz_memberships WHERE source_order_id=? AND auto_renew=0`, memberID) != 1 || scalar(`SELECT COUNT(*) FROM quota_buckets WHERE source_id=? AND JSON_UNQUOTE(JSON_EXTRACT(metadata_json,'$.payment_channel'))='wechat'`, memberID) != 3 || scalar(`SELECT balance FROM org_resource_accounts WHERE organization_id=10 AND resource_type='ai_seconds'`) != 600 {
		t.Fatal("incorrect membership allowance or automatic renewal")
	}
	exec(`INSERT INTO biz_orders(order_no,tenant_id,order_type,status,currency,payable_amount_cents,pricing_snapshot_json) VALUES('WX-RECURRING',10,'membership','pending','CNY',5000,JSON_OBJECT('cycle','recurring_month'))`)
	var recurringID int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM biz_orders WHERE order_no='WX-RECURRING'`).Scan(&recurringID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareWechatShopPayment(ctx, 10, 901, recurringID, identity); !errors.Is(err, ErrUnsupportedWechatAutoRenew) {
		t.Fatal("recurring JSAPI charge permitted", err)
	}
	// OAuth state is single-use, session-bound and expires; OpenID is scoped to
	// the authenticated session rather than permanently overwriting the user.
	session := strings.Repeat("a", 64)
	if err := s.CreateWechatOAuthState(ctx, 901, session, "random-state", "/shop/checkout?order=1", time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeWechatOAuthState(ctx, 901, strings.Repeat("b", 64), "random-state"); !errors.Is(err, ErrWechatOAuthStateInvalid) {
		t.Fatal("oauth session substitution", err)
	}
	if _, err := s.ConsumeWechatOAuthState(ctx, 901, session, "random-state"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeWechatOAuthState(ctx, 901, session, "random-state"); !errors.Is(err, ErrWechatOAuthStateInvalid) {
		t.Fatal("oauth state replay", err)
	}
	if err := s.BindWechatOpenID(ctx, 901, session, identity.AppID, identity.OpenID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWechatOpenID(ctx, 902, session, identity.AppID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-user payer access", err)
	}
	if _, err := s.GetWechatOpenID(ctx, 901, strings.Repeat("b", 64), identity.AppID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross-session payer access", err)
	}
	if err := s.CreateWechatOAuthState(ctx, 901, session, "expired-state", "/shop/checkout?order=1", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsumeWechatOAuthState(ctx, 901, session, "expired-state"); !errors.Is(err, ErrWechatOAuthStateInvalid) {
		t.Fatal("expired OAuth state accepted", err)
	}
	t.Run("wallet recharge", func(t *testing.T) { testWechatRechargeMySQL(t, s, identity) })
	t.Run("native recharge", func(t *testing.T) { testWechatNativeRechargeMySQL(t, s, identity) })
	t.Run("original route cash refund", func(t *testing.T) { testWechatCashRefundMySQL(t, s, identity) })
}
