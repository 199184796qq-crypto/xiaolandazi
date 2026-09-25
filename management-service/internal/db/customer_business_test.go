package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
	"time"
)

func TestCustomerBusinessValidation(t *testing.T) {
	now := time.Now().UTC()
	p := model.CustomerReceiptInput{TenantID: 1, Channel: "bank_transfer", Purpose: "recharge", AmountCents: 12345, PayerName: "test", ReceivingAccount: "company", ExternalTradeNo: "bank-ref", Evidence: "verified separately", OccurredAt: now, IdempotencyKey: "test"}
	if err := ValidateCustomerReceipt(&p); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*model.CustomerReceiptInput){func(v *model.CustomerReceiptInput) { v.AmountCents = 0 }, func(v *model.CustomerReceiptInput) { v.Channel = "sandbox" }, func(v *model.CustomerReceiptInput) { v.ExternalTradeNo = "SIM-001" }, func(v *model.CustomerReceiptInput) { v.Purpose = "platform_payment" }, func(v *model.CustomerReceiptInput) { v.Evidence = "" }} {
		v := p
		change(&v)
		if ValidateCustomerReceipt(&v) == nil {
			t.Fatal("invalid receipt accepted")
		}
	}
	if _, ok := SupportTransition("in_progress", "confirm", false); ok {
		t.Fatal("cannot confirm before resolution")
	}
	if s, ok := SupportTransition("in_progress", "resolve", true); !ok || s != "awaiting_confirmation" {
		t.Fatal("ops resolution bypasses customer confirmation")
	}
	if _, ok := SupportTransition("awaiting_confirmation", "confirm", true); ok {
		t.Fatal("ops cannot self-confirm")
	}
	if s, ok := SupportTransition("completed", "reopen", false); !ok || s != "pending" {
		t.Fatal("reopen missing")
	}
	if w, _ := customerScopeSQL(model.CustomerBusinessScope{Role: "agent_admin", UserID: 1}, "t.id"); w != "1=0" {
		t.Fatal("unknown actor scope opened")
	}
}
func TestCustomerFinanceSupportMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	exec := func(q string, args ...any) sql.Result {
		t.Helper()
		v, err := s.db.ExecContext(ctx, q, args...)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	seller := model.CustomerBusinessScope{UserID: 101, Role: "sales_staff"}
	other := model.CustomerBusinessScope{UserID: 102, Role: "sales_staff"}
	finance := model.CustomerBusinessScope{UserID: 900, Role: "platform_admin", Finance: true}
	ops := model.CustomerBusinessScope{UserID: 103, Role: "staff", Operations: true}
	lead, err := s.CreateSalesLeadByUser(ctx, 101, model.SalesLeadInput{BusinessName: "Financial test", Phone: "13900001111"})
	if err != nil {
		t.Fatal(err)
	}
	l, u, h, err := s.ConvertSalesLeadByUser(ctx, 101, lead.ID, model.ConvertSalesLeadInput{Username: "receipt_customer", DisplayName: "Receipt customer", Phone: "13800001111", Province: "P", City: "C", District: "D"}, "not-a-real-password")
	if err != nil {
		t.Fatal(err)
	}
	if l.Status != "registered" || h.ID != 0 {
		t.Fatal("registration incorrectly qualifies or dispatches")
	}
	tenant := *u.TenantID
	customer := model.CustomerBusinessScope{UserID: u.ID, Role: "customer", TenantID: tenant}
	rec, err := s.CustomerRecognition(ctx, seller, tenant)
	if err != nil || rec.Qualified || rec.CollectionStatus != "awaiting_payment" {
		t.Fatal("empty qualification", rec, err)
	}
	if _, err = s.CustomerRecognition(ctx, other, tenant); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other sales read access", err)
	}
	makeInput := func(ref string, amount uint64) model.CustomerReceiptInput {
		return model.CustomerReceiptInput{TenantID: tenant, Channel: "bank_transfer", Purpose: "recharge", AmountCents: amount, PayerName: "Customer", ReceivingAccount: "Company receipts", ExternalTradeNo: ref, Evidence: "Proof record with reviewer reconciliation", OccurredAt: time.Now().Add(-time.Hour), IdempotencyKey: ref}
	}
	p := makeInput("R-001", 12345)
	r, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil || repeated.ID != r.ID {
		t.Fatal("idempotent retry", err)
	}
	p.AmountCents++
	if _, err = s.SubmitCustomerReceipt(ctx, seller, p); err == nil {
		t.Fatal("same key different request accepted")
	}
	p.AmountCents--
	rec, _ = s.CustomerRecognition(ctx, seller, tenant)
	if rec.Qualified || rec.CollectionStatus != "pending" {
		t.Fatal("pending is not posted")
	}
	if _, err = s.ReviewCustomerReceipt(ctx, seller, r.ID, r.Version, "post", "not finance"); err == nil {
		t.Fatal("sales posted receipt")
	}
	self := finance
	self.UserID = 101
	if _, err = s.ReviewCustomerReceipt(ctx, self, r.ID, r.Version, "post", "self approval"); err == nil {
		t.Fatal("self approval allowed")
	}
	todos, err := s.ReceiptTodos(ctx, finance)
	if err != nil || todos["pending"] != 1 {
		t.Fatal("missing finance task", err)
	}
	r, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "needs_info", "need a bank proof reference")
	if err != nil {
		t.Fatal(err)
	}
	r, err = s.ResubmitCustomerReceipt(ctx, finance, r.ID, r.Version, "finance supplied evidence")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "post", "approve own supplement"); err == nil {
		t.Fatal("last submitter self approval")
	}
	secondFinance := finance
	secondFinance.UserID = 102
	r, err = s.ReviewCustomerReceipt(ctx, secondFinance, r.ID, r.Version, "post", "Matched bank record and amount")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "posted" || r.PostedAt == nil || r.RechargeID == nil {
		t.Fatal("posting incomplete")
	}
	rec, err = s.CustomerRecognition(ctx, seller, tenant)
	if err != nil || !rec.Qualified || rec.ConfirmedAmountCents != 12345 {
		t.Fatal("successful posting not visible", err)
	}
	if scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=? AND account_type='cash'", tenant) != 12345 {
		t.Fatal("wallet mismatch")
	}
	if _, err = s.ReviewCustomerReceipt(ctx, secondFinance, r.ID, 1, "post", "repeat"); err != nil {
		t.Fatal("posting replay", err)
	}
	if scalar("SELECT COUNT(*) FROM fin_wallet_ledger WHERE tenant_id=?", tenant) != 1 {
		t.Fatal("duplicate posting")
	}
	if scalar("SELECT COUNT(*) FROM crm_customer_handoffs") != 0 || scalar("SELECT COUNT(*) FROM crm_support_tickets") != 0 {
		t.Fatal("posting automatically dispatches")
	}
	dup := makeInput("R-001", 12345)
	dup.IdempotencyKey = "duplicate-key"
	if _, err = s.SubmitCustomerReceipt(ctx, seller, dup); err == nil {
		t.Fatal("duplicate evidence accepted")
	}
	next, err := s.SubmitCustomerReceipt(ctx, customer, makeInput("R-002", 999))
	if err != nil {
		t.Fatal(err)
	}
	_ = next
	rec, _ = s.CustomerRecognition(ctx, customer, tenant)
	if !rec.Qualified || rec.CollectionStatus != "pending" {
		t.Fatal("repeat recharge demotes customer")
	}
	// Real direct offline purchase consumes the existing order snapshot, not the wallet.
	oid, _ := exec("INSERT INTO biz_orders(order_no,tenant_id,order_type,status,payable_amount_cents) VALUES('O-001',?,'time_card','pending',5000)", tenant).LastInsertId()
	exec("INSERT INTO biz_order_items(order_id,product_type,product_id,product_version_id,product_name_snapshot,quantity,duration_seconds_snapshot,validity_days_snapshot,activation_mode_snapshot,activation_deadline_days_snapshot) VALUES(?,'time_card',1,1,'Test card',1,3600,7,'first_use',30)", oid)
	p = makeInput("OFFLINE-001", 5000)
	p.Channel = "offline"
	p.Purpose = "order"
	p.OrderID = &oid
	direct, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	direct, err = s.ReviewCustomerReceipt(ctx, finance, direct.ID, direct.Version, "post", "Offline cash receipt matched")
	if err != nil {
		t.Fatal(err)
	}
	if direct.PaymentID == nil || scalar("SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=?", oid) != 1 {
		t.Fatal("order not fulfilled")
	}
	if scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=? AND account_type='cash'", tenant) != 12345 {
		t.Fatal("direct order also credited wallet")
	}
	// Invalid snapshot rolls back payment, qualification update and fulfillment.
	badOrder, _ := exec("INSERT INTO biz_orders(order_no,tenant_id,order_type,status,payable_amount_cents) VALUES('O-FAIL',?,'time_card','pending',99)", tenant).LastInsertId()
	p = makeInput("OFFLINE-FAIL", 99)
	p.Channel = "offline"
	p.Purpose = "order"
	p.OrderID = &badOrder
	bad, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReviewCustomerReceipt(ctx, finance, bad.ID, bad.Version, "post", "attempt incomplete snapshot"); err == nil {
		t.Fatal("bad snapshot posted")
	}
	bad, _ = s.GetCustomerReceipt(ctx, seller, bad.ID)
	if bad.Status != "posting_failed" || scalar("SELECT COUNT(*) FROM fin_payment_transactions WHERE order_id=?", badOrder) != 0 {
		t.Fatal("rollback or retry task failed")
	}
	// Simulated success may exist in the legacy payment table but can never qualify.
	pid, _ := exec("INSERT INTO fin_payment_transactions(payment_no,tenant_id,order_id,order_no,channel,expected_amount_cents,input_amount_cents,paid_amount_cents,status,external_trade_no,idempotency_key) VALUES('SIMULATED',?,?,'O-SIM','sandbox',99,99,99,'paid','sim-bank-record','sim-id')", tenant, badOrder).LastInsertId()
	p = makeInput("sim-bank-record", 99)
	p.Channel = "platform"
	p.Purpose = "platform_payment"
	p.VerifiedPaymentID = &pid
	if _, err = s.SubmitCustomerReceipt(ctx, seller, p); err == nil {
		t.Fatal("sandbox accepted as real payment")
	}
	// Existing posting linked for verification never increases balance a second time.
	rid, _ := exec("INSERT INTO fin_recharge_orders(recharge_no,tenant_id,requested_amount_cents,credited_amount_cents,payment_method,status,external_trade_no) VALUES('LEGACY-R',?,200,200,'bank_transfer','paid','legacy-ref')", tenant).LastInsertId()
	p = makeInput("legacy-ref", 200)
	p.Purpose = "existing_recharge"
	p.ExistingRechargeID = &rid
	legacy, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err = s.ReviewCustomerReceipt(ctx, finance, legacy.ID, legacy.Version, "post", "matched existing posted source")
	if err != nil {
		t.Fatal(err)
	}
	if scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=? AND account_type='cash'", tenant) != 12345 {
		t.Fatal("legacy verification double credit")
	}
	rows, total, err := s.ListCustomerMoney(ctx, seller, tenant, "all", "all", "", nil, nil, 1, 12)
	if err != nil || total < 4 || len(rows) == 0 {
		t.Fatal("financial history", err)
	}
	for _, v := range rows {
		if v.Kind == "recharge" && v.ReferenceNo != "" {
			t.Fatal("linked recharge displayed twice")
		}
	}
	// Separate end-user and sales requests feed the same support queue, independent of rooms.
	tp := model.SupportTicketInput{TenantID: tenant, Category: "equipment", Title: "Setup help", Description: "Need help connecting device", ContactName: "customer", ContactPhone: "13800001111", IdempotencyKey: "ticket-1"}
	ticket, err := s.CreateSupportTicket(ctx, seller, tp)
	if err != nil {
		t.Fatal(err)
	}
	tp.IdempotencyKey = "ticket-2"
	tp.Title = "Different issue"
	if _, err = s.CreateSupportTicket(ctx, customer, tp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateSupportTicket(ctx, other, tp); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("other seller ticket", err)
	}
	ticket, err = s.UpdateSupportTicket(ctx, ops, ticket.ID, ticket.Version, "accept", "Accepted request")
	if err != nil {
		t.Fatal(err)
	}
	otherOps := ops
	otherOps.UserID = 102
	if _, err = s.UpdateSupportTicket(ctx, otherOps, ticket.ID, ticket.Version, "start", "steal claim"); err == nil {
		t.Fatal("claim stolen")
	}
	ticket, err = s.UpdateSupportTicket(ctx, ops, ticket.ID, ticket.Version, "start", "working")
	if err != nil {
		t.Fatal(err)
	}
	ticket, err = s.UpdateSupportTicket(ctx, ops, ticket.ID, ticket.Version, "resolve", "Connection verified, please confirm")
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Status != "awaiting_confirmation" {
		t.Fatal("ops self completed")
	}
	ticket, err = s.UpdateSupportTicket(ctx, customer, ticket.ID, ticket.Version, "confirm", "Working correctly")
	if err != nil || ticket.ConfirmedAt == nil {
		t.Fatal("confirmation missing", err)
	}
	ticket, err = s.UpdateSupportTicket(ctx, customer, ticket.ID, ticket.Version, "reopen", "Issue happened again")
	if err != nil || ticket.AssignedUserID != nil {
		t.Fatal("reopen retains old privilege", err)
	}
	// Future access follows handover, while original receipt/request attribution remains immutable.
	_, err = s.TransferSalesPortfolio(ctx, model.StaffBusinessScope{Mode: "all"}, 1, 2, 900, "Transfer test", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CustomerRecognition(ctx, seller, tenant); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old seller retains recognition", err)
	}
	if _, err = s.GetSupportTicket(ctx, seller, ticket.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("old seller retains support", err)
	}
	if _, err = s.GetCustomerReceipt(ctx, other, r.ID); err != nil {
		t.Fatal("new seller cannot follow receipt", err)
	}
	if _, err = s.GetSupportTicket(ctx, other, ticket.ID); err != nil {
		t.Fatal("new seller cannot follow ticket", err)
	}
	if scalar("SELECT requester_user_id FROM fin_customer_receipts WHERE id=?", r.ID) != 101 {
		t.Fatal("original source overwritten")
	}
	for i := 0; i < 14; i++ {
		p = makeInput(fmt.Sprintf("PAGE-%02d", i), 1)
		if _, err = s.SubmitCustomerReceipt(ctx, other, p); err != nil {
			t.Fatal(err)
		}
	}
	_, n, err := s.ListCustomerReceipts(ctx, other, tenant, "PAGE-NOTFOUND", "all", "all", 1, 12)
	if err != nil || n != 0 {
		t.Fatal("filter failed", err)
	}
	all, n, err := s.ListCustomerReceipts(ctx, other, tenant, "", "all", "all", 2, 12)
	if err != nil || n < 14 || len(all) == 0 || len(all) > 12 {
		t.Fatal("server paging failed", err)
	}
	t.Log("PASS registration!=payment, review/post atomicity, self-review denial, replay/dedup, real offline order, sandbox refusal, money timeline, customer/sales support, confirmation/reopen and transfer scope")
}

var _ = strings.TrimSpace
