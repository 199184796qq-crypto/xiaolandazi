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

func TestFinanceInvitationAccessBeforeStorage(t *testing.T) {
	s := &Store{}
	for _, role := range []string{"customer", "agent_admin", "sales_staff", "staff", ""} {
		access := model.StaffAccessContext{}
		if role == "customer" || role == "agent_admin" {
			access.IsSuperAdmin = true
			access.Permissions = []string{"finance.dashboard.view"}
		}
		actor := model.Actor{UserID: 1, Role: role}
		if _, _, err := s.ListFinanceInvitations(context.Background(), actor, access, "", "all", 1, 12); !errors.Is(err, ErrFinanceInvitationAccess) {
			t.Fatalf("role %s accepted", role)
		}
		if _, _, err := s.FinanceInvitationEarnings(context.Background(), actor, access, 1, 1, 12); !errors.Is(err, ErrFinanceInvitationAccess) {
			t.Fatalf("role %s evidence accepted", role)
		}
	}
	access := model.StaffAccessContext{Permissions: []string{"finance.dashboard.view"}, PrimaryGroupCode: "warehouse_after_sales"}
	if !model.CanReadFinanceInvitations(model.Actor{UserID: 1, Role: "staff"}, access) {
		t.Fatal("explicit finance permission was blocked by department")
	}
	if model.CanReadFinanceInvitations(model.Actor{Role: "staff"}, access) {
		t.Fatal("anonymous accepted")
	}
}
func TestFinanceInvitationsMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE crm_registration_referrals(id BIGINT PRIMARY KEY,invite_code_id BIGINT,inviter_user_id BIGINT,inviter_tenant_id BIGINT NULL,referred_user_id BIGINT,referred_tenant_id BIGINT UNIQUE,source_type VARCHAR(32),parent_org_id BIGINT,bound_at DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3))`)
	wanted := map[string]bool{"inc_earnings": true, "inc_settlement_batches": true, "inc_settlement_items": true}
	for _, q := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(q)
		if len(f) > 5 && wanted[f[5]] {
			exec(q)
			delete(wanted, f[5])
		}
	}
	if len(wanted) > 0 {
		t.Fatal("missing schema", wanted)
	}
	exec(`INSERT INTO iam_invite_codes(id,code,owner_user_id,status) VALUES(1,'REF-A',101,'active'),(2,'REF-B',102,'active')`)
	for i := 1; i <= 14; i++ {
		tenant, user := 600+i, 500+i
		exec(`INSERT INTO mgmt_tenants(id,org_type,code,name,status) VALUES(?,'customer',?,?,'active')`, tenant, fmt.Sprintf("ref-%d", i), fmt.Sprintf("Referred %d", i))
		exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,role,status) VALUES(?,?,?,?,?,'customer','active')`, user, tenant, fmt.Sprintf("refuser%d", i), fmt.Sprintf("Referred %d", i), fmt.Sprintf("refphone%d", i))
		owner, code := 101, 1
		if i == 14 {
			owner, code = 102, 2
		}
		exec(`INSERT INTO crm_registration_referrals(id,invite_code_id,inviter_user_id,referred_user_id,referred_tenant_id,source_type,parent_org_id) VALUES(?,?,?,?,?,'sales_invite',1)`, i, code, owner, user, tenant)
	}
	exec(`INSERT INTO fin_customer_confirmations(tenant_id,receipt_id,reviewer_user_id,confirmed_amount_cents) VALUES(601,7001,900,30000)`)
	actor := model.Actor{UserID: 103, Role: "staff"}
	access := model.StaffAccessContext{Permissions: []string{"finance.dashboard.view"}, PrimaryGroupCode: "warehouse_after_sales"}
	items, total, err := s.ListFinanceInvitations(ctx, actor, access, "", "all", 1, 12)
	if err != nil || total != 14 || len(items) != 12 {
		t.Fatal("finance list/page", total, len(items), err)
	}
	items, total, err = s.ListFinanceInvitations(ctx, actor, access, "", "all", 2, 12)
	if err != nil || total != 14 || len(items) != 2 {
		t.Fatal("second page", err)
	}
	items, total, err = s.ListFinanceInvitations(ctx, actor, access, "", "confirmed", 1, 12)
	if err != nil || total != 1 || items[0].ConfirmedAmountCents != 30000 || items[0].ConfirmedAt == nil {
		t.Fatal("confirmation", items, err)
	}
	_, total, err = s.ListFinanceInvitations(ctx, actor, access, "REF-B", "all", 1, 12)
	if err != nil || total != 1 {
		t.Fatal("code search", total, err)
	}
	_, total, err = s.ListFinanceInvitations(ctx, actor, access, "Seller A", "unconfirmed", 1, 12)
	if err != nil || total != 12 {
		t.Fatal("inviter search", total, err)
	}
	_, total, err = s.ListFinanceInvitations(ctx, actor, access, "' OR 1=1 --", "all", 1, 12)
	if err != nil || total != 0 {
		t.Fatal("untrusted search changed query", total, err)
	}
	_, own, err := s.ListInvitationRecords(ctx, model.Actor{UserID: 102, Role: "sales_staff"}, 1, 12)
	if err != nil || own != 1 {
		t.Fatal("personal scope regression", own, err)
	}
	// Existing source records only, with a negative reversal and actual settlement link.
	exec(`INSERT INTO biz_orders(id,order_no,tenant_id,order_type,status) VALUES(8001,'ORDER-REF-1',601,'time_card','paid'),(8002,'OTHER-CUSTOMER',602,'time_card','paid')`)
	exec(`INSERT INTO inc_earnings(id,external_id,beneficiary_type,beneficiary_id,earning_type,source_order_id,amount_cents,status,program_version_id,rule_id) VALUES(9001,'E-1','sales_staff',1,'referral_reward',8001,1200,'paid',3,4),(9002,'E-2','sales_staff',1,'referral_reward',8001,-1200,'reversed',3,4),(9003,'E-OTHER','sales_staff',2,'sales_commission',8002,9900,'available',3,4)`)
	exec(`UPDATE inc_earnings SET reversal_of_earning_id=9001,source_refund_id=555 WHERE id=9002`)
	exec(`INSERT INTO inc_settlement_batches(id,batch_no,beneficiary_type,beneficiary_id,period_start_at,period_end_at,status) VALUES(10001,'B-1','sales_staff',1,UTC_TIMESTAMP(3),UTC_TIMESTAMP(3),'paid')`)
	exec(`INSERT INTO inc_settlement_items(settlement_batch_id,earning_id,amount_cents) VALUES(10001,9001,1200)`)
	evidence, count, err := s.FinanceInvitationEarnings(ctx, actor, access, 1, 1, 12)
	if err != nil || count != 2 || len(evidence) != 2 {
		t.Fatal("earnings page", count, err)
	}
	if evidence[0].AmountCents != -1200 || evidence[0].ReversalOfEarningID == nil || evidence[1].SettlementBatchNo != "B-1" || evidence[1].ProgramVersionID == nil {
		t.Fatal("evidence/reversal lost", evidence)
	}
	if _, _, err = s.FinanceInvitationEarnings(ctx, actor, access, 9999, 1, 12); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unknown invitation", err)
	}
	t.Log("PASS finance read-only invitation source sharing, permissions/dual-role, pagination/search, confirmation, earnings/rule/refund/settlement provenance; personal scope retained")
}
