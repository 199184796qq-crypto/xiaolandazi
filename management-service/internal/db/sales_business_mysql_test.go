package db

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"livecompanion/management/internal/model"
)

var salesTestEnvFile = flag.String("sales-test-env-file", "", "Explicit opt-in environment file for an isolated sales integration schema; never tests against the configured business schema")

// No business schema is selected or mutated. A fresh random schema is always
// created and dropped, even when an assertion fails. Do not accept a schema name.
func salesIsolatedMySQL(t *testing.T) *Store {
	t.Helper()
	if *salesTestEnvFile == "" {
		t.Skip("isolated MySQL test requires explicit -sales-test-env-file")
	}
	f, err := os.Open(*salesTestEnvFile)
	if err != nil {
		t.Fatal("read requested integration config failed")
	}
	defer f.Close()
	values := map[string]string{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			values[strings.TrimSpace(k)] = v
		}
	}
	if scan.Err() != nil {
		t.Fatal("read integration config failed")
	}
	if values["DB_HOST"] == "" || values["DB_USER"] == "" {
		t.Fatal("integration DB connection is not configured")
	}
	cfg := mysql.NewConfig()
	cfg.User = values["DB_USER"]
	cfg.Passwd = values["DB_PASSWORD"]
	cfg.Net = "tcp"
	port := values["DB_PORT"]
	if port == "" {
		port = "3306"
	}
	cfg.Addr = net.JoinHostPort(values["DB_HOST"], port)
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 15 * time.Second
	cfg.WriteTimeout = 15 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var database *sql.DB
	if *salesTestPrefixedTables {
		cfg.DBName = values["DB_NAME"]
		if cfg.DBName == "" {
			t.Fatal("test database name is not configured")
		}
		database = salesPrefixedDatabase(t, cfg)
	} else {
		cfg.DBName = ""
		admin, err := sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal("open integration connection failed")
		}
		raw := make([]byte, 8)
		if _, err = rand.Read(raw); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		name := "lc_sales_test_" + hex.EncodeToString(raw)
		if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci"); err != nil {
			admin.Close()
			t.Fatalf("cannot create isolated integration schema: %v", err)
		}
		t.Cleanup(func() {
			if database != nil {
				database.Close()
			}
			c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if _, err := admin.ExecContext(c, "DROP DATABASE `"+name+"`"); err != nil {
				t.Errorf("cleanup isolated schema %s failed: %v", name, err)
			}
			admin.Close()
		})
		cfg.DBName = name
		database, err = sql.Open("mysql", cfg.FormatDSN())
		if err != nil {
			t.Fatal(err)
		}
	}
	database.SetMaxOpenConns(4)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := database.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("isolated fixture setup failed: %v", err)
		}
	}
	exec(`CREATE TABLE mgmt_system_settings(setting_key VARCHAR(96) PRIMARY KEY,value_text TEXT NOT NULL,updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0)`)
	exec(`CREATE TABLE staff_approval_tasks(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,operation_code VARCHAR(64),requester_user_id BIGINT UNSIGNED,target_id BIGINT UNSIGNED,status VARCHAR(32),payload_json JSON,approver_user_id BIGINT UNSIGNED NULL,decided_at DATETIME(3) NULL,created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_tenants(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,parent_id BIGINT UNSIGNED NULL,org_type VARCHAR(32),level INT,code VARCHAR(64) UNIQUE,name VARCHAR(128),status VARCHAR(32),created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),updated_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE mgmt_users(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,tenant_id BIGINT UNSIGNED NULL,username VARCHAR(128) UNIQUE,password_hash VARCHAR(255),must_change_password TINYINT DEFAULT 0,display_name VARCHAR(128),phone VARCHAR(128) UNIQUE,email VARCHAR(254) DEFAULT '',province VARCHAR(64) DEFAULT '',city VARCHAR(64) DEFAULT '',district VARCHAR(64) DEFAULT '',address VARCHAR(255) DEFAULT '',role VARCHAR(32),status VARCHAR(32),created_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(`CREATE TABLE staff_employees(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,user_id BIGINT UNSIGNED,primary_group_id BIGINT UNSIGNED,employment_status VARCHAR(32))`)
	exec(`CREATE TABLE mgmt_sessions(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,user_id BIGINT UNSIGNED)`)
	exec(`CREATE TABLE org_resource_accounts(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,organization_id BIGINT UNSIGNED,resource_type VARCHAR(64),unit VARCHAR(32),balance BIGINT,reserved BIGINT,status VARCHAR(32),UNIQUE KEY unique_resource(organization_id,resource_type))`)
	exec(`CREATE TABLE iam_invite_codes(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,code VARCHAR(64) UNIQUE,owner_user_id BIGINT UNSIGNED UNIQUE,owner_tenant_id BIGINT UNSIGNED,owner_role VARCHAR(32),status VARCHAR(32))`)
	wanted := map[string]bool{"fin_recharge_orders": true, "fin_refund_orders": true, "fin_wallet_ledger": true, "fin_payment_transactions": true, "biz_orders": true, "biz_order_items": true, "biz_time_card_assets": true, "crm_sales_teams": true, "crm_sales_staff": true, "crm_customer_sales_assignments": true, "crm_sales_followups": true, "crm_sales_leads": true, "crm_sales_lead_activities": true, "crm_customer_handoffs": true, "crm_customer_handoff_events": true, "crm_sales_handover_batches": true, "crm_sales_handover_items": true, "crm_customer_profiles": true, "fin_wallet_accounts": true}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) > 5 && fields[0] == "CREATE" && wanted[fields[5]] {
			exec(statement)
			delete(wanted, fields[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatalf("expected production schema fixtures missing: %v", wanted)
	}
	for _, statement := range strings.Split(strings.ReplaceAll(customerBusinessSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.TrimSpace(statement) != "" {
			exec(strings.TrimSuffix(strings.TrimSpace(statement), ";"))
		}
	}
	exec(`CREATE TABLE mkt_campaign_usage(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,order_id BIGINT UNSIGNED,campaign_id BIGINT UNSIGNED,campaign_item_id BIGINT UNSIGNED NULL,quantity INT,status VARCHAR(32))`)
	exec(`INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES(1,'platform',0,'platform','Test platform','active')`)
	exec(`INSERT INTO mgmt_users(id,username,display_name,phone,role,status) VALUES(101,'seller_a','Seller A','test-101','sales_staff','active'),(102,'seller_b','Seller B','test-102','sales_staff','active'),(103,'seller_c','Seller C','test-103','sales_staff','active'),(900,'manager','Manager','test-900','platform_admin','active')`)
	exec(`INSERT INTO crm_sales_staff(id,user_id,employee_code,status) VALUES(1,101,'A','active'),(2,102,'B','active'),(3,103,'C','active')`)
	exec(`INSERT INTO staff_employees(id,user_id,primary_group_id,employment_status) VALUES(1,101,10,'active'),(2,102,10,'active'),(3,103,20,'active')`)
	exec(`INSERT INTO mgmt_sessions(user_id) VALUES(101),(101)`)
	return &Store{db: database}
}

func TestSalesBusinessMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ids := []int64{}
	future := time.Now().UTC().Add(24 * time.Hour)
	for n := 0; n < 14; n++ {
		lead, err := s.CreateSalesLeadByUser(ctx, 101, model.SalesLeadInput{BusinessName: fmt.Sprintf("Test prospect %02d", n), Phone: fmt.Sprintf("1390000%04d", n), PlannedVisitAt: &future})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, lead.ID)
	}
	list, total, _, err := s.ListSalesLeadsByUser(ctx, 101, "", "open", "all", "created-desc", 2, 12)
	if err != nil || total != 14 || len(list) != 2 {
		t.Fatalf("database pagination: len=%d total=%d err=%v", len(list), total, err)
	}
	if _, err = s.GetSalesLeadByUser(ctx, 102, ids[0]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other seller read lead: %v", err)
	}
	if _, err = s.CreateSalesLeadActivityByUser(ctx, 102, ids[0], model.SalesLeadActivityInput{ActivityType: "visit", Content: "unauthorized"}); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other seller wrote lead: %v", err)
	}
	if _, err = s.CreateSalesLeadActivityByUser(ctx, 101, ids[0], model.SalesLeadActivityInput{ActivityType: "visit", Content: "Customer interested", Stage: "interested", NextFollowupAt: &future}); err != nil {
		t.Fatal(err)
	}
	lead, err := s.GetSalesLeadByUser(ctx, 101, ids[0])
	if err != nil || lead.PlannedVisitAt != nil || lead.NextFollowupAt == nil {
		t.Fatalf("visit did not update schedule: %#v %v", lead, err)
	}
	if _, err = s.CloseSalesLeadLostByUser(ctx, 101, ids[1], "Budget not approved"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateSalesLeadActivityByUser(ctx, 101, ids[1], model.SalesLeadActivityInput{ActivityType: "visit", Content: "cannot reopen"}); err == nil {
		t.Fatal("closed lead accepts mutation")
	}
	conversion := model.ConvertSalesLeadInput{Username: "customer001", DisplayName: "Test customer", Phone: "13800138001", Province: "P", City: "C", District: "D", HandoffSummary: "Set up equipment"}
	_, user, handoff, err := s.ConvertSalesLeadByUser(ctx, 101, ids[2], conversion, "test-hash-not-a-live-credential")
	if err != nil {
		t.Fatal(err)
	}
	if user.PasswordHash != "" || !user.MustChangePassword || user.TenantID == nil {
		t.Fatal("conversion credential boundary failed")
	}
	if scalar("SELECT COUNT(*) FROM fin_wallet_accounts WHERE tenant_id=? AND balance_cents=0", *user.TenantID) != 2 {
		t.Fatal("new wallets not initialized to zero")
	}
	if scalar("SELECT COALESCE(SUM(balance),0) FROM org_resource_accounts WHERE organization_id=? AND resource_type='ai_seconds'", *user.TenantID) != 0 {
		t.Fatal("conversion grants time without order")
	}
	if scalar("SELECT COUNT(*) FROM iam_invite_codes WHERE owner_user_id=?", user.ID) != 1 {
		t.Fatal("invite code missing")
	}
	if _, _, _, err = s.ConvertSalesLeadByUser(ctx, 101, ids[2], conversion, "not-used"); err == nil {
		t.Fatal("duplicate conversion accepted")
	}
	tenantsBefore := scalar("SELECT COUNT(*) FROM mgmt_tenants")
	if _, _, _, err = s.ConvertSalesLeadByUser(ctx, 101, ids[3], conversion, "not-used"); err == nil {
		t.Fatal("duplicate account accepted")
	}
	if scalar("SELECT COUNT(*) FROM mgmt_tenants") != tenantsBefore {
		t.Fatal("failed conversion leaked a customer organization")
	}
	if handoff.ID != 0 || scalar("SELECT COUNT(*) FROM crm_customer_handoffs") != 0 {
		t.Fatal("registration must not dispatch operations")
	}
	// Legacy handoff regression fixture, not a registration side effect.
	legacy, err := s.db.ExecContext(ctx, `INSERT INTO crm_customer_handoffs(handoff_no,lead_id,tenant_id,customer_user_id,sales_staff_id,status) VALUES('LEGACY-TEST',?,?,?,1,'pending')`, ids[2], *user.TenantID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	handoff.ID, err = legacy.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "done", handoff.ID, 103, "completed"); err == nil {
		t.Fatal("handoff completed before accepting")
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "taking over", handoff.ID, 103, "accepted"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "claim stolen", handoff.ID, 102, "in_progress"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("another operator took accepted task")
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "configuring", handoff.ID, 103, "in_progress"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "", handoff.ID, 103, "completed"); err == nil {
		t.Fatal("completion without notes")
	}
	if _, err = s.UpdateCustomerHandoffStatus(ctx, false, "setup tested", handoff.ID, 103, "completed"); err != nil {
		t.Fatal(err)
	}
	events, n, err := s.ListCustomerHandoffEvents(ctx, handoff.ID, 1, 12)
	if err != nil || n != 3 || len(events) != 3 {
		t.Fatal("handoff audit events missing", err)
	}
	if _, err = s.CreateSalesFollowupByUser(ctx, 101, model.SalesFollowupInput{TenantID: *user.TenantID, FollowupType: "phone", Content: "Followup before handover", NextFollowupAt: &future}); err != nil {
		t.Fatal(err)
	}
	// A manager of department 10 cannot move the portfolio to department 20.
	scope := model.StaffBusinessScope{Mode: "groups", GroupIDs: []int64{10}}
	if _, err = s.TransferSalesPortfolio(ctx, scope, 1, 3, 900, "cross department denied", nil, nil); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cross department handover accepted", err)
	}
	preview, err := s.SalesPortfolioHandoverPreview(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	wrong := int64(999)
	if _, err = s.TransferSalesPortfolio(ctx, scope, 1, 2, 900, "stale preview", &wrong, &preview.OpenLeadCount); err == nil {
		t.Fatal("stale preview accepted")
	}
	if scalar("SELECT COUNT(*) FROM crm_sales_handover_batches") != 0 {
		t.Fatal("failed preview wrote batch")
	}
	if err = s.DisableStaffEmployee(ctx, 1); err != nil {
		t.Fatal("must allow emergency revocation", err)
	}
	if scalar("SELECT COUNT(*) FROM mgmt_sessions WHERE user_id=101") != 0 {
		t.Fatal("departed user sessions remain")
	}
	batch, err := s.TransferSalesPortfolio(ctx, scope, 1, 2, 900, "Departing seller", &preview.CustomerCount, &preview.OpenLeadCount)
	if err != nil {
		t.Fatal("disabled source must be transferable", err)
	}
	if batch.CustomerCount != 1 || batch.LeadCount != 12 {
		t.Fatalf("unexpected handover counts: %#v", batch)
	}
	if scalar("SELECT source_sales_staff_id FROM crm_customer_profiles WHERE tenant_id=?", *user.TenantID) != 1 {
		t.Fatal("historical acquisition source rewritten")
	}
	if _, _, err := s.ListSalesLeadActivitiesByUser(ctx, 102, ids[2], 1, 12); err != nil {
		t.Fatal("successor cannot read pre-conversion history of transferred customer", err)
	}
	history, n, err := s.ListSalesLeadActivitiesByUser(ctx, 102, ids[0], 1, 12)
	if err != nil || n < 3 {
		t.Fatal("successor cannot see full lead history", err)
	}
	hasOldVisit := false
	for _, v := range history {
		if v.ActivityType == "visit" && v.SalesStaffID == 1 {
			hasOldVisit = true
		}
	}
	if !hasOldVisit {
		t.Fatal("original visit author lost")
	}
	follows, _, err := s.ListSalesFollowupsByUser(ctx, 102)
	if err != nil || len(follows) != 1 || follows[0].SalesStaffID != 1 || follows[0].NextFollowupAt == nil {
		t.Fatal("actual customer followup not transferred intact", err)
	}
	if _, err = s.GetSalesLeadByUser(ctx, 101, ids[0]); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("departed seller retains read access")
	}
	conversion.Username = "customer002"
	conversion.Phone = "13800138002"
	_, user2, _, err := s.ConvertSalesLeadByUser(ctx, 102, ids[0], conversion, "test-hash-not-live")
	if err != nil {
		t.Fatal(err)
	}
	if scalar("SELECT source_sales_staff_id FROM crm_customer_profiles WHERE tenant_id=?", *user2.TenantID) != 1 {
		t.Fatal("converted transferred prospect lost original developer")
	}
	if scalar("SELECT sales_staff_id FROM crm_customer_sales_assignments WHERE tenant_id=? AND status='active'", *user2.TenantID) != 2 {
		t.Fatal("new customer responsibility not given to current seller")
	}
	if _, err = s.TransferSalesPortfolio(ctx, scope, 1, 2, 900, "duplicate repeat", nil, nil); err == nil {
		t.Fatal("duplicate empty batch created")
	}
	t.Log("PASS: prospect create/pagination/ownership/visit/loss/conversion/rollback/zero-wallet/operations workflow/departure transfer/history/source preservation")
}
