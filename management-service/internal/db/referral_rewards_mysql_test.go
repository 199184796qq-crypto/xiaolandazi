package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func referralTestCreateCommercialTables(t *testing.T, s *Store, names ...string) {
	t.Helper()
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	for _, statement := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		fields := strings.Fields(statement)
		if len(fields) <= 5 || fields[0] != "CREATE" || !wanted[fields[5]] {
			continue
		}
		if _, err := s.db.Exec(strings.TrimSuffix(strings.TrimSpace(statement), ";")); err != nil {
			t.Fatalf("create referral test table %s: %v", fields[5], err)
		}
		delete(wanted, fields[5])
	}
	if len(wanted) > 0 {
		t.Fatalf("referral test schema fixtures missing: %v", wanted)
	}
}

func referralTestCreateWalletSchema(t *testing.T, s *Store) {
	t.Helper()
	for _, statement := range strings.Split(strings.ReplaceAll(beneficiaryWalletSchema, "\r\n", "\n"), "\n-- +statement\n") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if _, err := s.db.Exec(strings.TrimSuffix(statement, ";")); err != nil {
			t.Fatalf("create beneficiary wallet test schema: %v", err)
		}
	}
}

func TestReferralRewardLifecycleMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	referralTestCreateCommercialTables(
		t,
		s,
		"catalog_membership_plans",
		"catalog_membership_plan_versions",
		"inc_programs",
		"inc_program_versions",
		"inc_rules",
		"inc_earnings",
	)
	referralTestCreateWalletSchema(t, s)

	exec := func(q string, args ...any) int64 {
		t.Helper()
		result, err := s.db.ExecContext(ctx, q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := result.LastInsertId()
		return id
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := s.db.QueryRowContext(ctx, q, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}

	exec("INSERT INTO catalog_membership_plans(id,code,name,status) VALUES(1,'REF-PLAN','Referral Plan','active')")
	exec(`
		INSERT INTO catalog_membership_plan_versions(
			id,plan_id,version_no,lifecycle_status,price_cents,participates_referral,
			effective_from,published_at
		) VALUES(11,1,1,'published',10000,1,UTC_TIMESTAMP(3)-INTERVAL 1 DAY,UTC_TIMESTAMP(3)-INTERVAL 1 DAY)
	`)
	exec("INSERT INTO inc_programs(id,code,name,program_type,status) VALUES(21,'REF-10','10% referral','referral','active')")
	exec(`
		INSERT INTO inc_program_versions(
			id,program_id,version_no,lifecycle_status,pending_days,effective_from,published_at
		) VALUES(22,21,1,'published',1,UTC_TIMESTAMP(3)-INTERVAL 1 DAY,UTC_TIMESTAMP(3)-INTERVAL 1 DAY)
	`)
	exec(`
		INSERT INTO inc_rules(
			id,program_version_id,priority,event_type,conditions_json,action_type,action_config_json,enabled
		) VALUES(23,22,10,'membership_paid',
			JSON_OBJECT('referral_level',1,'refund_reversal',true),
			'percent_paid_amount',JSON_OBJECT('rate_bps',1000),1)
	`)

	paidAt := time.Now().UTC().Add(-time.Hour)
	exec(`
		INSERT INTO biz_orders(
			id,order_no,tenant_id,order_type,status,
			list_amount_cents,payable_amount_cents,paid_amount_cents,
			referral_relation_id_snapshot,referrer_tenant_id_snapshot,paid_at
		) VALUES(8001,'REF-ORDER-1',701,'membership','paid',10000,10000,10000,77,700,?)
	`, paidAt)
	exec(`
		INSERT INTO biz_order_items(
			id,order_id,product_type,product_id,product_version_id,
			product_name_snapshot,quantity,unit_list_price_cents,unit_paid_price_cents
		) VALUES(8101,8001,'membership',1,11,'Referral Plan',1,10000,10000)
	`)

	accrue := func() {
		t.Helper()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := accrueReferralRewardForPaidOrderTx(ctx, tx, 8001); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	accrue()
	accrue()

	if got := scalar("SELECT COUNT(*) FROM inc_earnings WHERE source_order_id=8001 AND earning_type='referral_reward'"); got != 1 {
		t.Fatalf("referral accrual is not idempotent: %d", got)
	}
	if got := scalar("SELECT amount_cents FROM inc_earnings WHERE source_order_id=8001 AND earning_type='referral_reward'"); got != 1000 {
		t.Fatalf("expected 10%% referral = 1000 cents, got %d", got)
	}
	if got := scalar("SELECT frozen_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 1000 {
		t.Fatalf("pending referral not frozen: %d", got)
	}
	if got := scalar("SELECT available_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 0 {
		t.Fatalf("pending referral leaked into available balance: %d", got)
	}

	exec("UPDATE inc_earnings SET available_at=UTC_TIMESTAMP(3)-INTERVAL 1 SECOND WHERE source_order_id=8001")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseMatureReferralRewardsTx(ctx, tx, 700); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := scalar("SELECT available_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 1000 {
		t.Fatalf("matured referral not released: %d", got)
	}
	if got := scalar("SELECT frozen_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 0 {
		t.Fatalf("matured referral still frozen: %d", got)
	}

	withdrawal, err := s.CreateCustomerReferralWithdrawal(ctx, 700, 900, 600)
	if err != nil || withdrawal.Status != "reviewing" {
		t.Fatalf("create referral withdrawal: %+v %v", withdrawal, err)
	}
	if got := scalar("SELECT available_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 400 {
		t.Fatalf("withdrawal hold did not reduce available balance: %d", got)
	}
	if got := scalar("SELECT frozen_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 600 {
		t.Fatalf("withdrawal hold not frozen: %d", got)
	}

	if _, err := s.ApproveReferralWithdrawal(ctx, withdrawal.ID, 900); !errors.Is(err, ErrFinanceDistinctReviewer) {
		t.Fatalf("strict review must reject same operator: %v", err)
	}
	exec(`
		INSERT INTO mgmt_system_settings(setting_key,value_text)
		VALUES(?,'false')
		ON DUPLICATE KEY UPDATE value_text='false'
	`, model.FinanceDistinctReviewerSetting)
	withdrawal, err = s.ApproveReferralWithdrawal(ctx, withdrawal.ID, 900)
	if err != nil || withdrawal.Status != "approved" {
		t.Fatalf("permission-only referral approval failed: %+v %v", withdrawal, err)
	}
	withdrawal, err = s.PayReferralWithdrawal(ctx, withdrawal.ID, 900)
	if err != nil || withdrawal.Status != "paid" {
		t.Fatalf("pay referral withdrawal failed: %+v %v", withdrawal, err)
	}
	if got := scalar("SELECT frozen_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != 0 {
		t.Fatalf("paid withdrawal still frozen: %d", got)
	}

	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := reverseOrderIncentivesForRefundTx(ctx, tx, 8001, 555, "REFUND-1", 10000, 10000); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if got := scalar("SELECT COUNT(*) FROM inc_earnings WHERE reversal_of_earning_id IS NOT NULL AND source_order_id=8001"); got != 1 {
		t.Fatalf("refund reversal earning missing: %d", got)
	}
	if got := scalar("SELECT available_balance_cents FROM fin_beneficiary_wallets WHERE beneficiary_type='customer_referrer' AND beneficiary_id=700"); got != -600 {
		t.Fatalf("late full refund should create -600 referral debt after 600 was paid out, got %d", got)
	}
	if _, err := s.CreateCustomerReferralWithdrawal(ctx, 700, 900, 1); !errors.Is(err, ErrReferralWithdrawalInsufficient) {
		t.Fatalf("negative referral balance must block new withdrawal: %v", err)
	}

	t.Log("PASS referral payment accrual, idempotency, freeze/release, review policy, withdrawal payout, full-refund clawback, negative debt block")
}
