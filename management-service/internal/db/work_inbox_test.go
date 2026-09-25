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

func TestInboxPermissionPlans(t *testing.T) {
	scope := model.InboxScope{Actor: model.Actor{UserID: 900, Role: "staff"}, Access: model.StaffAccessContext{Permissions: []string{"finance.dashboard.view"}}}
	if len(inboxQueries(scope)) != 0 {
		t.Fatal("read-only finance got actionable tasks")
	}
	scope.Access.Permissions = append(scope.Access.Permissions, "finance.recharge.approve")
	scope.RequireDistinctReviewer = true
	plans := inboxQueries(scope)
	if len(plans) != 2 {
		t.Fatalf("expected receipt + recharge, got %d", len(plans))
	}
	if !strings.Contains(plans[0].where, "last_submitter_user_id<>") {
		t.Fatal("self review exclusion missing")
	}
	distinct := InboxCacheIdentity(scope, "finance")
	scope.Actor.UserID++
	if distinct == InboxCacheIdentity(scope, "finance") {
		t.Fatal("self-review cache identity leak")
	}
	scope.RequireDistinctReviewer = false
	key := InboxCacheIdentity(scope, "finance")
	scope.Actor.UserID++
	if key != InboxCacheIdentity(scope, "finance") {
		t.Fatal("identical permission-only queue cannot share")
	}
	scope = model.InboxScope{Actor: model.Actor{UserID: 103, Role: "staff"}, Access: model.StaffAccessContext{Permissions: []string{"liveops.configure"}}}
	plans = inboxQueries(scope)
	if len(plans) != 2 || !strings.Contains(plans[1].where, "assigned_user_id=?") {
		t.Fatal("operations assignment scope absent")
	}
	scope.Actor.Role = "customer"
	scope.Access.IsSuperAdmin = true
	if len(inboxQueries(scope)) != 0 || len(InboxTopics(scope)) != 0 {
		t.Fatal("terminal customer must not have inbox queries, even with forged permissions")
	}
	if _, err := (&Store{}).InboxItems(context.Background(), scope, "receipt_supplement", 1, 12, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("disabled customer query touched database or exposed an inbox item", err)
	}
}

func TestWorkInboxMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE inv_rmas(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,rma_no VARCHAR(64),status VARCHAR(32),created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE inv_devices(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,sn VARCHAR(64),lifecycle_status VARCHAR(32),created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE inv_shipments(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,shipment_no VARCHAR(64),shipment_type VARCHAR(32),status VARCHAR(32),created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	if err := s.MigrateWorkInbox(ctx); err != nil {
		t.Fatal(err)
	}
	admin := model.InboxScope{Actor: model.Actor{UserID: 900, Role: "platform_admin"}, RequireDistinctReviewer: true}
	counts := func(sc model.InboxScope, topic string) map[string]int64 {
		t.Helper()
		groups, err := s.InboxTopic(ctx, sc, topic)
		if err != nil {
			t.Fatal(topic, err)
		}
		out := map[string]int64{}
		for _, g := range groups {
			out[g.Key] = g.Count
		}
		return out
	}
	lead, err := s.CreateSalesLeadByUser(ctx, 101, model.SalesLeadInput{BusinessName: "Inbox test", Phone: "13900002222"})
	if err != nil {
		t.Fatal(err)
	}
	_, u, _, err := s.ConvertSalesLeadByUser(ctx, 101, lead.ID, model.ConvertSalesLeadInput{Username: "inbox_customer", DisplayName: "Inbox customer", Phone: "13800002222", Province: "P", City: "C", District: "D"}, "test-only-hash")
	if err != nil {
		t.Fatal(err)
	}
	tenant := *u.TenantID
	seller := model.CustomerBusinessScope{UserID: 101, Role: "sales_staff"}
	p := model.CustomerReceiptInput{TenantID: tenant, Channel: "bank_transfer", Purpose: "recharge", AmountCents: 15000, PayerName: "Test payer", ReceivingAccount: "Test account", ExternalTradeNo: "INBOX-REAL-001", Evidence: "Isolated test only", OccurredAt: time.Now(), IdempotencyKey: "INBOX-001"}
	before, _ := s.InboxRevisions(ctx)
	r, err := s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := s.InboxRevisions(ctx)
	if after["finance"] <= before["finance"] {
		t.Fatal("receipt commit omitted durable invalidation")
	}
	_, err = s.SubmitCustomerReceipt(ctx, seller, p)
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := s.InboxRevisions(ctx)
	if replay["finance"] != after["finance"] {
		t.Fatal("idempotent replay created a task")
	}
	if counts(admin, "finance")["receipt_review"] != 1 {
		t.Fatal("missing review")
	}
	self := admin
	self.Actor.UserID = 101
	if counts(self, "finance")["receipt_review"] != 0 {
		t.Fatal("strict self review counted")
	}
	self.RequireDistinctReviewer = false
	if counts(self, "finance")["receipt_review"] != 1 {
		t.Fatal("permission-only self omitted")
	}
	finance := model.CustomerBusinessScope{UserID: 900, Role: "platform_admin", Finance: true}
	_, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "needs_info", "Need proof")
	if err != nil {
		t.Fatal(err)
	}
	sales := model.InboxScope{Actor: model.Actor{UserID: 101, Role: "sales_staff"}}
	if counts(sales, "finance")["receipt_supplement"] != 1 {
		t.Fatal("supplement not handed back")
	}
	other := sales
	other.Actor.UserID = 102
	if counts(other, "finance")["receipt_supplement"] != 0 {
		t.Fatal("other seller sees task")
	}
	customer := model.InboxScope{Actor: model.Actor{UserID: u.ID, Role: "customer", TenantID: u.TenantID}}
	if len(counts(customer, "finance")) != 0 {
		t.Fatal("terminal customer inbox still available")
	}
	if receipt, err := s.GetCustomerReceipt(ctx, model.CustomerBusinessScope{UserID: u.ID, Role: "customer", TenantID: tenant}, r.ID); err != nil || receipt.Status != "needs_info" {
		t.Fatal("removing inbox must preserve customer receipt progress", err)
	}
	if _, err = s.InboxItems(ctx, customer, "repair_accept", 1, 12, false); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("external group escape", err)
	}
	exec(`INSERT INTO inv_rmas(rma_no,status) VALUES('R-1','SUBMITTED'),('R-2','PROCESSING'),('R-3','REPAIRING'),('R-4','RETURN_PENDING'),('R-5','COMPLETED')`)
	exec(`INSERT INTO inv_devices(sn,lifecycle_status) VALUES('D-1','INBOUND_PENDING'),('D-2','SCRAP_PENDING'),('D-3','IN_STOCK')`)
	inv := counts(admin, "inventory")
	if inv["repair_accept"] != 1 || inv["repair_process"] != 2 || inv["inventory_inbound"] != 1 || inv["inventory_scrap"] != 1 {
		t.Fatal("wrong inventory mapping", inv)
	}
	for i := 0; i < 14; i++ {
		exec("INSERT INTO inv_shipments(shipment_no,shipment_type,status) VALUES(?,'outbound','ready_to_ship')", fmt.Sprintf("S-%02d", i))
	}
	exec(`INSERT INTO inv_shipments(shipment_no,shipment_type,status) VALUES('X-1','outbound','exception'),('X-2','rma_return','ready_to_ship'),('X-3','outbound','in_transit')`)
	logistics := counts(admin, "logistics")
	if logistics["logistics_dispatch"] != 14 || logistics["logistics_exception"] != 1 {
		t.Fatal("external transit counted", logistics)
	}
	pg, err := s.InboxItems(ctx, admin, "logistics_dispatch", 2, 12, false)
	if err != nil || pg.Total != 14 || len(pg.Items) != 2 {
		t.Fatal("server pagination", pg, err)
	}
	for _, item := range pg.Items {
		if item.To != "/resources/logistics" || !strings.HasPrefix(item.Key, "shipment:") {
			t.Fatal("wrong origin link", item)
		}
	}
	// Rollback of a source transaction must roll back its revision as well.
	before, _ = s.InboxRevisions(ctx)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = bumpInboxTx(ctx, tx, "inventory"); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback()
	after, _ = s.InboxRevisions(ctx)
	if before["inventory"] != after["inventory"] {
		t.Fatal("rolled-back mutation published")
	}
	due := time.Now().UTC().Add(-time.Minute)
	_, err = s.CreateSalesLeadByUser(ctx, 101, model.SalesLeadInput{BusinessName: "Due once", Phone: "13900003333", PlannedVisitAt: &due, NextFollowupAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	if counts(sales, "sales")["sales_lead_due"] != 1 {
		t.Fatal("same lead counted twice")
	}
	t.Log("PASS isolated MySQL: scope, strict/permission-only reviewer, transactional revisions+rollback+replay, finance handback, repair/inventory/logistics mapping, due dedup and server pagination")
}
