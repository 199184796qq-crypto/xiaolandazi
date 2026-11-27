package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestDeviceWalletPaymentMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exec := func(query string, args ...any) sql.Result {
		t.Helper()
		result, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	assertCount := func(want int64, query string, args ...any) {
		t.Helper()
		var got int64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d, want %d, error %v", query, got, want, err)
		}
	}
	for _, schema := range []string{commercialSchema, inventorySchema, beneficiaryWalletSchema} {
		for _, query := range strings.Split(strings.ReplaceAll(schema, "\r\n", "\n"), "\n-- +statement\n") {
			if strings.TrimSpace(query) != "" {
				exec(query)
			}
		}
	}
	exec(`INSERT INTO mgmt_tenants(id,code,name) VALUES(10,'device-test','设备测试')`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username) VALUES(110,10,'device-buyer')`)
	exec(`INSERT INTO fin_wallet_accounts(tenant_id,account_type,currency,balance_cents) VALUES(10,'cash','CNY',500)`)
	exec(`INSERT INTO catalog_device_products(id,code,sku_code,name,sales_stock,status) VALUES(77,'box','BOX','直播终端',1,'active')`)
	exec(`INSERT INTO catalog_device_versions(product_id,version_no,lifecycle_status,list_price_cents,sale_price_cents,participates_referral,participates_sales_commission,participates_agent_settlement) VALUES(77,1,'published',1200,1000,0,0,0)`)
	exec(`INSERT INTO inv_warehouses(id,code,name,status) VALUES(1,'HQ','测试仓','active')`)
	exec(`INSERT INTO inv_devices(sn,sku_code,custody_warehouse_id,lifecycle_status,quality_status) VALUES('BOX77','BOX',1,'IN_STOCK','qualified')`)
	input := model.CreateCustomerShopOrderInput{
		ProductType: "device", ProductID: 77, Quantity: 1, IdempotencyKey: "device-checkout",
		RecipientName: "测试", RecipientPhone: "13800138000", Province: "四川省", City: "南充市", District: "顺庆区", Address: "模拟地址",
	}
	order, err := s.CreateCustomerShopOrder(ctx, 10, 110, input)
	if err != nil || order.Status != "pending" || order.PayableAmountCents != 1000 || order.Shipping == nil {
		t.Fatal(order, err)
	}
	assertCount(1, `SELECT COUNT(*) FROM biz_order_devices WHERE order_id=? AND status='payment_hold'`, order.ID)
	assertCount(0, `SELECT sales_stock FROM catalog_device_products WHERE id=77`)
	pay := func() (model.CustomerShopOrder, error) {
		return s.WalletPayCustomerShopOrder(ctx, 10, 110, order.ID, "")
	}
	if _, err := pay(); !errors.Is(err, ErrInsufficientWalletBalance) {
		t.Fatalf("expected insufficient wallet balance, got %v", err)
	}
	assertCount(500, `SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10`)
	exec(`UPDATE fin_wallet_accounts SET balance_cents=2000 WHERE tenant_id=10`)
	exec(`UPDATE biz_order_devices SET hold_expires_at=CURRENT_TIMESTAMP(3)-INTERVAL 1 MINUTE WHERE order_id=?`, order.ID)
	if _, err := pay(); !errors.Is(err, ErrShopOrderExpired) {
		t.Fatalf("expected expired stock hold, got %v", err)
	}
	assertCount(0, `SELECT COUNT(*) FROM fin_payment_transactions WHERE order_id=?`, order.ID)
	assertCount(1, `SELECT COUNT(*) FROM biz_orders WHERE id=? AND status='cancelled'`, order.ID)
	assertCount(1, `SELECT sales_stock FROM catalog_device_products WHERE id=77`)
	input.IdempotencyKey = "device-checkout-after-expiry"
	order, err = s.CreateCustomerShopOrder(ctx, 10, 110, input)
	if err != nil {
		t.Fatal(err)
	}
	var payments sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		payments.Add(1)
		go func() {
			defer payments.Done()
			paid, err := pay()
			if err == nil && (paid.PaymentStatus != "paid" || len(paid.Shipments) != 1 || paid.Shipments[0].Status != "ready_to_ship") {
				err = fmt.Errorf("unexpected payment and shipment state: %+v", paid)
			}
			errs <- err
		}()
	}
	payments.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	assertCount(1000, `SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10`)
	assertCount(1, `SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_id=? AND business_type='device_purchase'`, order.ID)
	assertCount(1, `SELECT COUNT(*) FROM inv_shipments WHERE business_id=? AND status='ready_to_ship' AND tracking_no=''`, order.ID)
	assertCount(0, `SELECT sales_stock FROM catalog_device_products WHERE id=77`)

	refund, err := exec(`INSERT INTO fin_refund_orders(refund_no,tenant_id,source_type,source_id,refund_amount_cents,refund_method,status,idempotency_key) VALUES('BOX-REF',10,'order',?,1000,'wallet_balance','pending_approval','box-refund')`, order.ID).LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	applyRefund := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := applyOrderRefundTx(ctx, tx, 10, refund, "BOX-REF", order.ID, 1000, 110, "测试退货", "wallet_balance"); err != nil {
			return err
		}
		return tx.Commit()
	}
	if err := applyRefund(); err != nil {
		t.Fatal(err)
	}
	if err := applyRefund(); !errors.Is(err, ErrShopOrderFullyRefunded) {
		t.Fatalf("expected a completed refund to block a second credit, got %v", err)
	}
	assertCount(2000, `SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10`)
	assertCount(1, `SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_id=? AND business_type='device_refund'`, refund)

	terms := WorkspaceMembershipInput{
		Name: "验证会员", PriceCents: 89900, IncludedSeconds: 360000,
		TimeCardReduction: 1000, DeviceReduction: 2000,
	}
	if err := s.SaveWorkspaceMembership(ctx, 110, 0, terms); err != nil {
		t.Fatal(err)
	}
	plans, err := s.ListCommercialMembershipPlans(ctx)
	if err != nil || len(plans) != 1 || plans[0].Status != "inactive" || !strings.HasPrefix(plans[0].Code, "aimem") ||
		!strings.HasSuffix(plans[0].Code, "000001") || plans[0].ActiveVersion.DefaultDeviceDiscountBPS != 8000 {
		t.Fatalf("membership creation: %+v %v", plans, err)
	}
	plan := plans[0]
	listed, err := s.SetCommercialMembershipStatus(ctx, 110, plan.ID, "active")
	if err != nil || listed.Status != "active" {
		t.Fatal(listed, err)
	}
	offers, err := s.ListCustomerMembershipOffers(ctx)
	if err != nil || len(offers) != 1 || offers[0].DeviceDiscountBPS != 8000 {
		t.Fatal(offers, err)
	}
	exec(`INSERT INTO biz_memberships (tenant_id,plan_id,plan_version_id,cycle_start_at,cycle_end_at)
		VALUES (10,?,?,CURRENT_TIMESTAMP(3)-INTERVAL 1 DAY,CURRENT_TIMESTAMP(3)+INTERVAL 1 DAY)`, plan.ID, plan.ActiveVersion.ID)
	exec(`INSERT INTO catalog_device_products(id,code,sku_code,name,sales_stock,status) VALUES(78,'box2','BOX2','会员直播终端',2,'active')`)
	exec(`INSERT INTO catalog_device_versions(product_id,version_no,lifecycle_status,list_price_cents,sale_price_cents) VALUES(78,1,'published',1000,1000)`)
	exec(`INSERT INTO inv_devices(sn,sku_code,custody_warehouse_id,lifecycle_status,quality_status) VALUES
		('BOX78A','BOX2',1,'IN_STOCK','qualified'),('BOX78B','BOX2',1,'IN_STOCK','qualified')`)
	input.ProductID, input.IdempotencyKey = 78, "membership-device-checkout"
	device, err := s.CreateCustomerShopOrder(ctx, 10, 110, input)
	if err != nil || device.PayableAmountCents != 800 {
		t.Fatal(device, err)
	}
	if err := s.SaveWorkspaceTimeCard(ctx, 110, 0, model.CommercialTimeCardInput{
		Name: "试验时长卡", PriceCents: 10000, DurationSeconds: 36000, ValidityDays: 7, ActivationDeadlineDays: 20,
	}); err != nil {
		t.Fatal(err)
	}
	cards, err := s.ListWorkspaceTimeCards(ctx, time.Time{}, time.Time{})
	if err != nil || len(cards) != 1 {
		t.Fatal(cards, err)
	}
	if _, err := s.SetCommercialTimeCardStatus(ctx, 110, cards[0].ID, "active"); err != nil {
		t.Fatal(err)
	}
	cardInput := model.CreateCustomerShopOrderInput{ProductType: "time_card", ProductID: cards[0].ID, Quantity: 1, IdempotencyKey: "membership-card-checkout"}
	cardOrder, err := s.CreateCustomerShopOrder(ctx, 10, 110, cardInput)
	if err != nil || cardOrder.PayableAmountCents != 9000 {
		t.Fatal(cardOrder, err)
	}
	terms.DeviceReduction, terms.TimeCardReduction = 10000, 9500
	if err := s.SaveWorkspaceMembership(ctx, 110, plan.ID, terms); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetCommercialMembershipPlan(ctx, plan.ID)
	if err != nil || current.Status != "active" || current.Code != plan.Code || current.ActiveVersion.ID == plan.ActiveVersion.ID ||
		current.ActiveVersion.DefaultDeviceDiscountBPS != 0 || current.ActiveVersion.DefaultTimeCardDiscountBPS != 500 {
		t.Fatalf("membership edit: %+v %v", current, err)
	}
	oldRate, err := s.CustomerTimeCardDiscountBPS(ctx, 10)
	if err != nil || oldRate != 9000 {
		t.Fatal(oldRate, err)
	}
	exec(`UPDATE biz_memberships SET plan_version_id=? WHERE tenant_id=10`, current.ActiveVersion.ID)
	input.IdempotencyKey = "fully-discounted-device"
	freeDevice, err := s.CreateCustomerShopOrder(ctx, 10, 110, input)
	if err != nil || freeDevice.PayableAmountCents != 0 {
		t.Fatal(freeDevice, err)
	}
	paid, err := s.WalletPayCustomerShopOrder(ctx, 10, 110, freeDevice.ID, "")
	if err != nil || paid.PaymentStatus != "paid" || paid.PaidAmountCents != 0 {
		t.Fatal(paid, err)
	}
	cardInput.IdempotencyKey = "deep-membership-card-discount"
	cardOrder, err = s.CreateCustomerShopOrder(ctx, 10, 110, cardInput)
	if err != nil || cardOrder.PayableAmountCents != 500 {
		t.Fatal(cardOrder, err)
	}
	unlisted, err := s.SetCommercialMembershipStatus(ctx, 110, plan.ID, "inactive")
	if err != nil || unlisted.Status != "inactive" {
		t.Fatal(unlisted, err)
	}
	offers, err = s.ListCustomerMembershipOffers(ctx)
	if err != nil || len(offers) != 0 {
		t.Fatal(offers, err)
	}
	archived, err := s.SetCommercialMembershipStatus(ctx, 110, plan.ID, "archived")
	if err != nil || archived.Status != "archived" {
		t.Fatal(archived, err)
	}
	if err := s.SaveWorkspaceMembership(ctx, 110, plan.ID, terms); !errors.Is(err, ErrMembershipArchived) {
		t.Fatal(err)
	}
	if _, err := s.SetCommercialMembershipStatus(ctx, 110, plan.ID, "active"); !errors.Is(err, ErrMembershipArchived) {
		t.Fatal(err)
	}
}

func TestCommerceClosedLoopMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	exec := func(q string, args ...any) sql.Result {
		t.Helper()
		r, e := s.db.ExecContext(ctx, q, args...)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	scalar := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if e := s.db.QueryRowContext(ctx, q, args...).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	wanted := map[string]bool{"quota_buckets": true, "quota_ledger": true, "catalog_time_card_products": true, "catalog_time_card_versions": true, "catalog_device_versions": true, "catalog_membership_plans": true, "catalog_membership_plan_versions": true, "catalog_membership_card_discounts": true, "biz_memberships": true, "crm_customer_agent_relations": true, "crm_referral_relations": true, "mkt_campaigns": true, "mkt_campaign_skin_assets": true, "mkt_campaign_presentations": true, "mkt_campaign_placements": true, "mkt_campaign_items": true, "mkt_campaign_price_rules": true, "mkt_campaign_scopes": true, "mkt_campaign_inventory": true, "mkt_campaign_order_snapshots": true, "inc_programs": true, "inc_program_versions": true, "inc_rules": true, "inc_earnings": true, "inc_settlement_batches": true, "inc_settlement_items": true, "biz_audit_events": true}
	for _, q := range strings.Split(strings.ReplaceAll(commercialSchema, "\r\n", "\n"), "\n-- +statement\n") {
		f := strings.Fields(q)
		if len(f) > 5 && wanted[f[5]] {
			exec(q)
		}
	}
	for _, q := range strings.Split(strings.ReplaceAll(beneficiaryWalletSchema, "\r\n", "\n"), "\n-- +statement\n") {
		if strings.TrimSpace(q) != "" {
			exec(q)
		}
	}
	exec(`CREATE TABLE org_resource_ledger(id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,organization_id BIGINT UNSIGNED NOT NULL,counterparty_org_id BIGINT UNSIGNED NULL,resource_type VARCHAR(64) NOT NULL,change_quantity BIGINT NOT NULL,balance_before BIGINT NOT NULL,balance_after BIGINT NOT NULL,business_type VARCHAR(64) NOT NULL,operator_user_id BIGINT UNSIGNED NULL,reason VARCHAR(512) NOT NULL DEFAULT '',created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3))`)
	exec(inboxRevisionSchema)
	exec(`INSERT INTO mgmt_tenants(id,org_type,level,code,name,status,created_at) VALUES(10,'customer',1,'new','New','active',CURRENT_TIMESTAMP(3)),(11,'customer',1,'old','Old','active','2020-01-01'),(12,'customer',1,'ref','Ref','active',CURRENT_TIMESTAMP(3))`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,role,status,created_at) VALUES(110,10,'new','New','13800138001','customer','active',CURRENT_TIMESTAMP(3)),(111,11,'old','Old','13800138002','customer','active','2020-01-01')`)
	exec(`INSERT INTO catalog_time_card_products(id,code,name,status) VALUES(1,'test-card','Test card','active'),(2,'other-card','Other card','active')`)
	exec(`INSERT INTO catalog_time_card_versions(id,product_id,version_no,lifecycle_status,price_cents,duration_seconds,validity_days,activation_mode,participates_referral,participates_sales_commission) VALUES(1,1,1,'published',10000,3600,365,'first_use',1,1),(2,2,1,'published',10000,3600,365,'first_use',1,1)`)
	exec(`INSERT INTO fin_wallet_accounts(tenant_id,account_type,currency,balance_cents) VALUES(10,'cash','CNY',10000)`)
	exec(`INSERT INTO crm_customer_sales_assignments(tenant_id,sales_staff_id,status,effective_from) VALUES(10,1,'active','2020-01-01')`)
	exec(`INSERT INTO crm_referral_relations(referred_tenant_id,referrer_tenant_id,status,bound_at) VALUES(10,12,'active','2020-01-01')`)
	makeCampaign := func(code string, product int64, mode string, price *uint64) model.MarketingCampaign {
		t.Helper()
		c, e := s.CreateMarketingCampaign(ctx, 900, model.MarketingCampaignInput{Code: code, Name: code, Status: "active", PricingRule: "floor_yuan", DisplayLocations: []string{"shop"}, Controls: model.MarketingCampaignControls{Audience: "new_within_days", NewAccountDays: 7}, Items: []model.MarketingCampaignItem{{TargetType: "time_card", TargetID: product, PricingMode: mode, FixedPriceCents: price, Quantity: 3, PackageMonths: 1, DiscountBPS: 10000}}})
		if e != nil {
			t.Fatal(e)
		}
		return c
	}
	p := uint64(100)
	c := makeCampaign("new-one-yuan", 1, "fixed", &p)
	if c.Items[0].FixedPriceCents == nil || *c.Items[0].FixedPriceCents != 100 || c.Controls.MaxClaims != 1 {
		t.Fatal("campaign roundtrip")
	}
	create := func(tenant, user int64, c model.MarketingCampaign, key string) (model.CustomerShopOrder, error) {
		return s.CreateCustomerShopOrder(ctx, tenant, user, model.CreateCustomerShopOrderInput{ProductType: "time_card", ProductID: c.Items[0].TargetID, Quantity: 1, MarketingCampaignID: c.ID, MarketingPlacement: "shop", IdempotencyKey: key})
	}
	if _, e := create(11, 111, c, "old"); !errors.Is(e, ErrMarketingEligibility) {
		t.Fatal("old account eligible", e)
	}
	r := model.CommerceRewardRule{Name: "Sales ten percent", Channel: "sales", ScopeType: "product", ProductType: "time_card", TargetID: 1, Mode: "percent", RateBPS: 1000, ScheduleMode: "immediate"}
	id, e := s.SaveCommerceRewardDraft(ctx, 900, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PublishCommerceRewardRule(ctx, 900, id); e != nil {
		t.Fatal(e)
	}
	r.Channel = "referral"
	r.Name = "Ref fixed"
	r.Mode = "fixed"
	r.AmountCents = 7
	id, e = s.SaveCommerceRewardDraft(ctx, 900, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PublishCommerceRewardRule(ctx, 900, id); e != nil {
		t.Fatal(e)
	}
	order, e := create(10, 110, c, "new-buy")
	if e != nil {
		t.Fatal(e)
	}
	if order.PayableAmountCents != 100 {
		t.Fatal("fixed per bundle", order.PayableAmountCents)
	}
	if _, e = create(10, 110, c, "duplicate"); !errors.Is(e, ErrMarketingEligibility) {
		t.Fatal("duplicate claim allowed", e)
	}
	// Published replacement must not change the existing order snapshot.
	r.Channel = "sales"
	r.Name = "Sales now twenty"
	r.Mode = "percent"
	r.RateBPS = 2000
	id, e = s.SaveCommerceRewardDraft(ctx, 900, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PublishCommerceRewardRule(ctx, 900, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.WalletPayCustomerShopOrder(ctx, 10, 110, order.ID, "paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.WalletPayCustomerShopOrder(ctx, 10, 110, order.ID, "paid-again"); e != nil {
		t.Fatal(e)
	}
	if n := scalar(`SELECT COUNT(*) FROM inc_earnings WHERE source_order_id=?`, order.ID); n != 2 {
		t.Fatal("payment retry duplicated commission", n)
	}
	if n := scalar(`SELECT amount_cents FROM inc_earnings WHERE source_order_id=? AND earning_type='commerce_sales'`, order.ID); n != 10 {
		t.Fatal("snapshot or penny allocation changed", n)
	}
	if n := scalar(`SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=?`, order.ID); n != 3 {
		t.Fatal("bundle fulfillment", n)
	}
	wallet, e := s.GetSalesCommissionWalletDashboard(ctx, 1, 100)
	if e != nil || wallet.Wallet.AvailableBalanceCents != 10 {
		t.Fatal("sales wallet", e)
	}
	wd, e := s.CreateSalesCommissionWithdrawal(ctx, 1, 101, 10)
	if e != nil {
		t.Fatal(e)
	}
	exec(`INSERT INTO mgmt_system_settings(setting_key,value_text) VALUES('finance.require_distinct_reviewer','true') ON DUPLICATE KEY UPDATE value_text='true'`)
	if _, e = s.ApproveSalesCommissionWithdrawal(ctx, wd.ID, 900); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PaySalesCommissionWithdrawal(ctx, wd.ID, 900); e != nil {
		t.Fatal(e)
	}
	// Two half refunds must exhaust reward exactly, even the odd 7-cent reward.
	for i := 1; i <= 2; i++ {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.ExecContext(ctx, `UPDATE biz_orders SET refunded_amount_cents=? WHERE id=?`, 50*i, order.ID); e != nil {
			t.Fatal(e)
		}
		if e = reverseOrderIncentivesForRefundTx(ctx, tx, order.ID, int64(800+i), fmt.Sprint(i), 50, 100); e != nil {
			tx.Rollback()
			t.Fatal(e)
		}
		if e = tx.Commit(); e != nil {
			t.Fatal(e)
		}
	}
	if n := scalar(`SELECT SUM(amount_cents) FROM inc_earnings WHERE source_order_id=? AND beneficiary_type='customer_referrer'`, order.ID); n != 0 {
		t.Fatal("partial refund rounding leaked reward", n)
	}
	wallet, e = s.GetSalesCommissionWalletDashboard(ctx, 1, 100)
	if e != nil || wallet.Wallet.AvailableBalanceCents != -10 {
		t.Fatal("paid-out refund must become recoverable debt", e, wallet.Wallet)
	}
	if _, e = s.CreateSalesCommissionWithdrawal(ctx, 1, 101, 1); !errors.Is(e, ErrReferralWithdrawalInsufficient) {
		t.Fatal("withdrew debt", e)
	}
	if _, e = create(10, 110, c, "refund-reclaim"); !errors.Is(e, ErrMarketingEligibility) {
		t.Fatal("refund restored welcome benefit", e)
	}
	// A fresh account: concurrency across different campaigns/products sharing same benefit.
	exec(`INSERT INTO mgmt_tenants(id,org_type,level,code,name,status) VALUES(13,'customer',1,'concurrent','Concurrent','active')`)
	exec(`INSERT INTO mgmt_users(id,tenant_id,username,display_name,phone,role,status) VALUES(113,13,'concurrent','Concurrent','13800138003','customer','active')`)
	free := makeCampaign("new-free", 2, "free", nil)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	orders := make(chan model.CustomerShopOrder, 2)
	for i, cp := range []model.MarketingCampaign{c, free} {
		wg.Add(1)
		go func(i int, cp model.MarketingCampaign) {
			defer wg.Done()
			o, e := create(13, 113, cp, fmt.Sprintf("race-%d", i))
			results <- e
			if e == nil {
				orders <- o
			}
		}(i, cp)
	}
	wg.Wait()
	close(results)
	close(orders)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrMarketingEligibility) {
			t.Fatal("concurrency unexpected failure", e)
		}
	}
	if success != 1 {
		t.Fatal("double welcome claim", success)
	}
	var race model.CustomerShopOrder
	for o := range orders {
		race = o
	}
	if _, e = s.CancelCustomerShopOrder(ctx, 13, race.ID); e != nil {
		t.Fatal(e)
	}
	o, e := create(13, 113, free, "free-after-cancel")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimFreeMarketingOrder(ctx, 13, 113, o.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimFreeMarketingOrder(ctx, 13, 113, o.ID); e != nil {
		t.Fatal(e)
	}
	if n := scalar(`SELECT COUNT(*) FROM biz_time_card_assets WHERE source_order_id=?`, o.ID); n != 3 {
		t.Fatal("free retry duplicated assets", n)
	}
	if n := scalar(`SELECT COUNT(*) FROM inc_earnings WHERE source_order_id=?`, o.ID); n != 0 {
		t.Fatal("free reward minted money", n)
	}
	// Monthly freeze and idempotent scheduled maturity in isolated fixture.
	r.Name = "Monthly fifteen"
	r.Channel = "sales"
	r.Mode = "percent"
	r.RateBPS = 1000
	r.ScheduleMode = "monthly"
	r.MonthLag = 1
	r.ReleaseDay = 15
	id, e = s.SaveCommerceRewardDraft(ctx, 900, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.PublishCommerceRewardRule(ctx, 900, id); e != nil {
		t.Fatal(e)
	}
	mid, _ := exec(`INSERT INTO biz_orders(order_no,tenant_id,order_type,status,currency,paid_amount_cents,paid_at,sales_staff_id_snapshot) VALUES('MONTHLY',10,'time_card','paid','CNY',100,CURRENT_TIMESTAMP(3),1)`).LastInsertId()
	exec(`INSERT INTO biz_order_items(order_id,product_type,product_id,product_version_id,product_name_snapshot,quantity,unit_paid_price_cents) VALUES(?,'time_card',1,1,'Test card',3,33)`, mid)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = snapshotCommerceRewardsTx(ctx, tx, mid); e == nil {
		e = accrueCommerceRewardsTx(ctx, tx, mid)
	}
	if e != nil {
		tx.Rollback()
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	wallet, e = s.GetSalesCommissionWalletDashboard(ctx, 1, 100)
	if e != nil || wallet.Wallet.FrozenBalanceCents != 10 {
		t.Fatal("monthly earning not frozen", e, wallet.Wallet)
	}
	exec(`UPDATE inc_earnings SET available_at=UTC_TIMESTAMP(3)-INTERVAL 1 DAY WHERE source_order_id=?`, mid)
	if e = s.ReleaseCommerceEarnings(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.ReleaseCommerceEarnings(ctx); e != nil {
		t.Fatal(e)
	}
	wallet, e = s.GetSalesCommissionWalletDashboard(ctx, 1, 100)
	if e != nil || wallet.Wallet.FrozenBalanceCents != 0 || wallet.Wallet.AvailableBalanceCents != 0 {
		t.Fatal("maturity or refund debt offset", e, wallet.Wallet)
	}
	details, e := s.ListCommerceEarningDetails(ctx, "sales_staff", 1, "")
	if e != nil || len(details) < 2 {
		t.Fatal("earning detail missing", e)
	}
	if _, e = s.ListCommerceEarningDetails(ctx, "sales_staff", 1, "bad"); e == nil {
		t.Fatal("invalid period accepted")
	}

	exec(`INSERT INTO catalog_membership_plans(id,code,name,status) VALUES(1,'member-test','Test member','active')`)
	exec(`INSERT INTO catalog_membership_plan_versions(id,plan_id,version_no,lifecycle_status,price_cents,included_seconds,allow_auto_renew,participates_referral) VALUES(1,1,1,'active',10000,3600,1,1)`)
	mc, e := s.CreateMarketingCampaign(ctx, 900, model.MarketingCampaignInput{Code: "member-free", Name: "Member free", Status: "active", PricingRule: "floor_yuan", DisplayLocations: []string{"shop"}, Controls: model.MarketingCampaignControls{Audience: "all", MaxClaims: 1}, Items: []model.MarketingCampaignItem{{TargetType: "membership", TargetID: 1, PricingMode: "free", Quantity: 1, PackageMonths: 1, DiscountBPS: 10000}}})
	if e != nil {
		t.Fatal(e)
	}
	mo, e := s.CreateCustomerShopOrder(ctx, 13, 113, model.CreateCustomerShopOrderInput{ProductType: "membership", ProductID: 1, MarketingCampaignID: mc.ID, MarketingPlacement: "shop", IdempotencyKey: "member-free", Quantity: 1})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimFreeMarketingOrder(ctx, 10, 110, mo.ID); e == nil {
		t.Fatal("cross tenant claim accepted")
	}
	if _, e = s.ClaimFreeMarketingOrder(ctx, 13, 113, mo.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ClaimFreeMarketingOrder(ctx, 13, 113, mo.ID); e != nil {
		t.Fatal(e)
	}
	if n := scalar("SELECT COUNT(*) FROM biz_memberships WHERE tenant_id=13 AND source_order_id=?", mo.ID); n != 1 {
		t.Fatal("free membership duplicate", n)
	}
	if n := scalar("SELECT COUNT(*) FROM biz_memberships WHERE tenant_id=13 AND auto_renew=1"); n != 0 {
		t.Fatal("free membership silently auto-renews")
	}
	t.Log("eligibility, cents, immutable snapshots, duplicate payment, commission, withdrawals, refund debt, concurrent claims, free fulfillment passed")
}
