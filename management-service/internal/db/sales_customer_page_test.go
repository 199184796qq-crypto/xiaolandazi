package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestSalesCustomerPageSortAllowlist(t *testing.T) {
	for _, input := range []string{"", "created-desc", "created-asc", "name-asc", "name-desc", "u.id; DROP TABLE mgmt_users", "unknown"} {
		got := salesCustomerPageOrder(input)
		if strings.Contains(got, ";") || !strings.Contains(got, "u.id") {
			t.Fatalf("unsafe or unstable order: %q", got)
		}
	}
	if salesCustomerPageOrder("malicious") != salesCustomerPageOrder("created-desc") {
		t.Fatal("unknown sort must use safe default")
	}
}

// Explicit opt-in only; the helper creates isolated random-prefixed tables.
func TestSalesCustomersPageMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO mgmt_tenants(id,parent_id,org_type,level,code,name,status) VALUES(2,1,'agent',1,'agent-test','Agent','active')")
	for n := 1; n <= 19; n++ {
		tenantID, userID := int64(300+n), int64(400+n)
		parent := 1
		if n == 19 {
			parent = 2
		}
		exec("INSERT INTO mgmt_tenants(id,parent_id,org_type,level,code,name,status) VALUES(?,?,'customer',1,?,?,'active')", tenantID, parent, fmt.Sprintf("cust-%02d", n), fmt.Sprintf("Customer %02d", n))
		status, source := "active", "sales_lead"
		if n > 12 {
			status = "disabled"
		}
		if n%2 == 0 {
			source = "direct"
		}
		name := fmt.Sprintf("Customer %02d", n)
		if n == 1 {
			name = "Customer 01 100%_!"
		}
		exec("INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,email,province,city,district,address,role,status,created_at) VALUES(?,?,?,?,?,'contact@example.invalid','P','C','D','Street','customer',?,'2026-01-01')", userID, tenantID, fmt.Sprintf("customer%02d", n), name, fmt.Sprintf("test-phone-%02d", n), status)
		exec("INSERT INTO crm_customer_profiles(tenant_id,source_type) VALUES(?,?)", tenantID, source)
		if n == 16 {
			continue
		} // Unassigned customer must not leak.
		staffID, assignmentStatus := 1, "active"
		if n == 17 {
			staffID = 2
		}
		if n == 18 {
			assignmentStatus = "ended"
		}
		exec("INSERT INTO crm_customer_sales_assignments(tenant_id,sales_staff_id,status,effective_from) VALUES(?,?,?,'2026-01-01')", tenantID, staffID, assignmentStatus)
	}
	// A duplicated current assignment must not duplicate customer rows or totals.
	exec("INSERT INTO crm_customer_sales_assignments(tenant_id,sales_staff_id,status,effective_from) VALUES(301,1,'active','2026-01-01')")
	items, total, summary, err := s.ListSalesCustomersPageByUser(ctx, 101, "", "all", "all", "created-desc", 1, 12)
	if err != nil || total != 15 || len(items) != 12 || summary.TotalCount != 15 || summary.ActiveCount != 12 || summary.DisabledCount != 3 {
		t.Fatalf("page1: len=%d total=%d summary=%+v err=%v", len(items), total, summary, err)
	}
	if items[0].UserID != 415 || items[0].Email != "contact@example.invalid" {
		t.Fatal("incorrect order or contact projection")
	}
	page2, _, _, err := s.ListSalesCustomersPageByUser(ctx, 101, "", "all", "all", "created-desc", 2, 12)
	if err != nil || len(page2) != 3 || page2[2].UserID != 401 {
		t.Fatalf("page2 failed: %v", err)
	}
	seen := map[int64]bool{}
	for _, item := range append(items, page2...) {
		if seen[item.UserID] {
			t.Fatal("duplicate user across pages")
		}
		seen[item.UserID] = true
	}
	filtered, n, _, err := s.ListSalesCustomersPageByUser(ctx, 101, "", "disabled", "direct", "name-asc", 1, 12)
	if err != nil || n != 1 || len(filtered) != 1 || filtered[0].UserID != 414 {
		t.Fatalf("status/source filters: %d %v", n, err)
	}
	literal, n, _, err := s.ListSalesCustomersPageByUser(ctx, 101, "100%_!", "all", "all", "name-asc", 1, 12)
	if err != nil || n != 1 || literal[0].UserID != 401 {
		t.Fatalf("literal LIKE escaping failed: %d %v", n, err)
	}
	items, n, _, err = s.ListSalesCustomersPageByUser(ctx, 101, "other seller not found", "all", "all", "created-desc", 1, 12)
	if err != nil || n != 0 || items == nil || len(items) != 0 {
		t.Fatal("empty result must be nonnil array", err)
	}
	items, n, _, err = s.ListSalesCustomersPageByUser(ctx, 102, "", "all", "all", "created-desc", 1, 12)
	if err != nil || n != 1 || items[0].UserID != 417 {
		t.Fatal("ownership filtering failed", err)
	}
	// Simulate reassignment; current scope changes, source profile does not.
	exec("UPDATE crm_customer_sales_assignments SET status='ended',effective_to=CURRENT_TIMESTAMP(3) WHERE tenant_id=301")
	exec("INSERT INTO crm_customer_sales_assignments(tenant_id,sales_staff_id,status,effective_from) VALUES(301,2,'active',CURRENT_TIMESTAMP(3))")
	_, n, _, err = s.ListSalesCustomersPageByUser(ctx, 101, "customer01", "all", "all", "created-desc", 1, 12)
	if err != nil || n != 0 {
		t.Fatal("former seller retains customer access", err)
	}
	items, n, _, err = s.ListSalesCustomersPageByUser(ctx, 102, "customer01", "all", "all", "created-desc", 1, 12)
	if err != nil || n != 1 || items[0].SourceType != "sales_lead" {
		t.Fatal("successor or source preservation failed", err)
	}
	exec("UPDATE mgmt_users SET status='disabled' WHERE id=102")
	_, _, _, err = s.ListSalesCustomersPageByUser(ctx, 102, "", "all", "all", "created-desc", 1, 12)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("disabled seller query was not denied", err)
	}
	t.Log("PASS assigned customer server paging, filters, stable sort, duplicate protection, literal search, transfer scope, disabled seller")
}
