package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestFinanceReviewSettingValidation(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		if err := ValidateFinanceReviewSetting(model.FinanceDistinctReviewerSetting, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "0", "False", "false ", "null", "anything"} {
		if ValidateFinanceReviewSetting(model.FinanceDistinctReviewerSetting, value) == nil {
			t.Fatalf("accepted invalid switch %q", value)
		}
	}
}

func TestFinanceReviewPolicyMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := s.db.ExecContext(ctx, q, args...)
		if err != nil {
			t.Fatal(err)
		}
		n, _ := r.LastInsertId()
		return n
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	policy, err := s.FinanceReviewPolicy(ctx)
	if err != nil || !policy.RequireDistinctReviewer {
		t.Fatal("missing setting must retain strict mode", err)
	}
	lead, err := s.CreateSalesLeadByUser(ctx, 101, model.SalesLeadInput{BusinessName: "Policy customer", Phone: "13900009991"})
	if err != nil {
		t.Fatal(err)
	}
	_, user, _, err := s.ConvertSalesLeadByUser(ctx, 101, lead.ID, model.ConvertSalesLeadInput{Username: "policy_customer", DisplayName: "Policy customer", Phone: "13800009991", Province: "P", City: "C", District: "D"}, "test-only-not-a-credential")
	if err != nil {
		t.Fatal(err)
	}
	tenant := *user.TenantID
	finance := model.CustomerBusinessScope{UserID: 900, Role: "platform_admin", Finance: true}
	makeReceipt := func(key string) model.CustomerReceipt {
		t.Helper()
		r, err := s.SubmitCustomerReceipt(ctx, finance, model.CustomerReceiptInput{TenantID: tenant, Channel: "bank_transfer", Purpose: "recharge", AmountCents: 30000, PayerName: "Test", ReceivingAccount: "Test company", ExternalTradeNo: key, Evidence: "Isolated test evidence", OccurredAt: time.Now().Add(-time.Hour), IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := makeReceipt("POLICY-1")
	if _, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "post", "self denied"); !errors.Is(err, ErrFinanceDistinctReviewer) {
		t.Fatal("default self approval", err)
	}
	exec("INSERT INTO mgmt_system_settings(setting_key,value_text) VALUES(?,'true')", model.FinanceDistinctReviewerSetting)
	set := func(value string) {
		t.Helper()
		if err := s.UpdateSystemSettings(ctx, []model.SystemSettingUpdate{{Key: model.FinanceDistinctReviewerSetting, Value: value}}, 900); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.UpdateSystemSettings(ctx, []model.SystemSettingUpdate{{Key: model.FinanceDistinctReviewerSetting, Value: "invalid"}}, 900); err == nil {
		t.Fatal("invalid configuration saved")
	}
	set("false")
	policy, err = s.FinanceReviewPolicy(ctx)
	if err != nil || policy.RequireDistinctReviewer {
		t.Fatal("switch not read", err)
	}
	noPermission := model.CustomerBusinessScope{UserID: 900, Role: "sales_staff"}
	if _, err = s.ReviewCustomerReceipt(ctx, noPermission, r.ID, r.Version, "post", "must reject"); err == nil {
		t.Fatal("toggle granted finance privileges")
	}
	r, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "post", "same authorized finance operator")
	if err != nil || r.Status != "posted" {
		t.Fatal("permission-only self posting failed", err)
	}
	if scalar("SELECT COUNT(*) FROM fin_customer_receipts WHERE id=? AND requester_user_id=reviewer_user_id", r.ID) != 1 {
		t.Fatal("operator identities lost")
	}
	if scalar("SELECT COUNT(*) FROM fin_customer_receipt_events WHERE receipt_id=? AND action='review_policy' AND note LIKE '%同一人%'", r.ID) != 1 {
		t.Fatal("same-user audit missing")
	}
	if _, err = s.ReviewCustomerReceipt(ctx, finance, r.ID, r.Version, "post", "replay"); err != nil {
		t.Fatal(err)
	}
	if scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=? AND account_type='cash'", tenant) != 30000 {
		t.Fatal("replayed money")
	}
	r2 := makeReceipt("POLICY-2")
	set("true")
	if _, err = s.ReviewCustomerReceipt(ctx, finance, r2.ID, r2.Version, "reject", "stale page must reject"); !errors.Is(err, ErrFinanceDistinctReviewer) {
		t.Fatal("reenable failed", err)
	}
	set("false")
	r2, err = s.ReviewCustomerReceipt(ctx, finance, r2.ID, r2.Version, "needs_info", "more evidence")
	if err != nil {
		t.Fatal(err)
	}
	r2, err = s.ResubmitCustomerReceipt(ctx, finance, r2.ID, r2.Version, "supplemented evidence")
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE mgmt_system_settings SET value_text='corrupt' WHERE setting_key=?", model.FinanceDistinctReviewerSetting)
	if _, err = s.ReviewCustomerReceipt(ctx, finance, r2.ID, r2.Version, "post", "bad policy"); !errors.Is(err, ErrFinanceReviewPolicyUnavailable) {
		t.Fatal("invalid stored config did not fail closed", err)
	}
	set("false")
	r2, err = s.ReviewCustomerReceipt(ctx, finance, r2.ID, r2.Version, "reject", "rejected by same authorized operator")
	if err != nil || r2.Status != "rejected" {
		t.Fatal("permission-only reject failed", err)
	}
	// Legacy approval queue uses exactly the same stored toggle and records policy.
	id := exec("INSERT INTO staff_approval_tasks(operation_code,requester_user_id,target_id,status,payload_json) VALUES('finance.reward',900,?,'pending',?)", tenant, `{"amount_cents":100,"reason":"policy test"}`)
	set("true")
	if err = s.ApproveStaffFinanceTask(ctx, id, 900); !errors.Is(err, ErrFinanceDistinctReviewer) {
		t.Fatal("legacy strict failed", err)
	}
	set("false")
	if err = s.ApproveStaffFinanceTask(ctx, id, 900); err != nil {
		t.Fatal("legacy permission-only failed", err)
	}
	if scalar("SELECT COUNT(*) FROM staff_approval_tasks WHERE id=? AND status='approved' AND approver_user_id=requester_user_id AND JSON_UNQUOTE(JSON_EXTRACT(payload_json,'$.review_policy_mode'))='permission_only'", id) != 1 {
		t.Fatal("legacy review trace lost")
	}
	id = exec("INSERT INTO staff_approval_tasks(operation_code,requester_user_id,target_id,status,payload_json) VALUES('finance.reward',900,?,'pending','{}')", tenant)
	if err = s.RejectStaffFinanceTask(ctx, id, 900); err != nil {
		t.Fatal("legacy reject failed", err)
	}
	set("true")
	if scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=? AND account_type='cash'", tenant) != 30000 {
		t.Fatal("policy decisions mutated money unexpectedly")
	}
	t.Log("PASS default strict, permission-only self post/reject/supplement, re-enable, malformed config fail closed, no permission grant, no duplicate credit, receipts and legacy policy audit")
}
