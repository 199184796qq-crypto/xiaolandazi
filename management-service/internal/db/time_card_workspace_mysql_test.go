package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestTimeCardWorkspaceMySQL(t *testing.T) {
	s := salesIsolatedMySQL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	exec := func(query string, args ...any) int64 {
		t.Helper()
		result, err := s.db.ExecContext(ctx, query, args...)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		id, _ := result.LastInsertId()
		return id
	}
	scalar := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		if err := s.db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return value
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, schema := range []string{commercialSchema, beneficiaryWalletSchema, commerceRulesSchema} {
		for _, stmt := range strings.Split(strings.ReplaceAll(schema, "\r\n", "\n"), "\n-- +statement\n") {
			if strings.TrimSpace(stmt) != "" {
				exec(stmt)
			}
		}
	}
	must(s.migrateCustomerBusiness(ctx))
	for _, column := range wechatPaymentColumnMigrations() {
		exec(column.sql)
	}
	exec(`ALTER TABLE staff_approval_tasks ADD policy_id BIGINT NULL, ADD target_type VARCHAR(32),
        ADD amount DECIMAL(16,2) NOT NULL DEFAULT 0`)
	exec(`CREATE TABLE org_resource_ledger (
        id BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY, organization_id BIGINT, resource_type VARCHAR(64),
        change_quantity BIGINT,balance_before BIGINT,balance_after BIGINT,business_type VARCHAR(64),
        operator_user_id BIGINT NULL,reason VARCHAR(512),created_at DATETIME(3))`)
	must(s.MigrateWorkInbox(ctx))
	exec(`INSERT INTO mgmt_tenants (id,code,name,org_type) VALUES (10,'customer','水果店','customer')`)
	exec(`INSERT INTO mgmt_users (id,tenant_id,username,display_name,role) VALUES
        (110,10,'card-owner','水果店','customer'),(210,NULL,'ops','运维','staff'),(211,NULL,'fin','财务','staff')`)
	exec(`INSERT INTO fin_wallet_accounts (tenant_id,account_type,balance_cents) VALUES (10,'cash',50000)`)

	terms := model.CommercialTimeCardInput{Name: "10 小时时长卡", PriceCents: 10000, DurationSeconds: 36000, ValidityDays: 7, ActivationDeadlineDays: 20}
	must(s.SaveWorkspaceTimeCard(ctx, 210, 0, terms))
	id := scalar("SELECT id FROM catalog_time_card_products WHERE name=?", terms.Name)
	must(s.SaveWorkspaceTimeCard(ctx, 210, 0, model.CommercialTimeCardInput{Name: "测试新卡", PriceCents: 2500, DurationSeconds: 7200, ValidityDays: 7, ActivationDeadlineDays: 20}))
	other := scalar("SELECT id FROM catalog_time_card_products WHERE name='测试新卡'")
	must(s.ReorderWorkspaceTimeCards(ctx, 210, []int64{other, id}, []int64{id, other}))

	cards, err := s.ListWorkspaceTimeCards(ctx, time.Time{}, time.Time{})
	must(err)
	if len(cards) != 2 || cards[0].ID != other || cards[1].ID != id {
		t.Fatalf("unexpected catalog %+v", cards)
	}
	code := cards[1].Code
	if !regexp.MustCompile(`^aitc\d{18}$`).MatchString(code) || !strings.HasSuffix(code, "000001") {
		t.Fatal(code)
	}
	if _, err := s.SetCommercialTimeCardStatus(ctx, 210, id, "active"); err != nil {
		t.Fatal(err)
	}
	order, err := s.CreateCustomerShopOrder(ctx, 10, 110, model.CreateCustomerShopOrderInput{ProductType: "time_card", ProductID: id, Quantity: 2, IdempotencyKey: "workspace-payment"})
	must(err)
	order, err = s.WalletPayCustomerShopOrder(ctx, 10, 110, order.ID, "")
	must(err)
	if order.PaidAmountCents != 20000 {
		t.Fatal(order.PaidAmountCents)
	}
	first := scalar("SELECT id FROM biz_time_card_assets WHERE source_order_id=? AND asset_sequence=1", order.ID)
	second := scalar("SELECT id FROM biz_time_card_assets WHERE source_order_id=? AND asset_sequence=2", order.ID)
	filter := TimeCardSalesQuery{Username: "card-owner", Page: 1, PageSize: 50}
	sales, err := s.ListTimeCardSales(ctx, id, filter)
	must(err)
	if sales.Count != 2 || sales.Items[0].SaleNo != code+"00000002" || sales.Items[1].SaleNo != code+"00000001" || sales.PaidCents != 20000 {
		t.Fatalf("unexpected sales %+v", sales)
	}

	terms.Code = "cannot-overwrite-code"
	terms.PriceCents = 12000
	terms.ValidityDays = 9
	must(s.SaveWorkspaceTimeCard(ctx, 210, id, terms))
	cards, err = s.ListWorkspaceTimeCards(ctx, time.Time{}, time.Time{})
	must(err)
	if cards[1].Code != code || cards[1].PriceCents != 12000 || cards[1].Status != "active" {
		t.Fatal(cards[1])
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	must(err)
	must(lockTenantAIResourceTx(ctx, tx, 10))
	must(activateTimeCardAssetByIDTx(ctx, tx, 10, first, now))
	must(tx.Commit())
	exec(`UPDATE quota_buckets SET remaining_seconds=9000 WHERE source_type='time_card_asset' AND source_id=?`, first)
	exec(`UPDATE biz_time_card_assets SET activation_deadline_at=? WHERE id=?`, now.Add(-time.Minute), second)
	if err := s.RequestTimeCardExpiryRefund(ctx, id, first, 210, 2500, "客户补偿", "主管"); !errors.Is(err, ErrTimeCardRefundUnavailable) {
		t.Fatal(err)
	}
	must(s.SweepTimeCardExpirations(ctx, now.AddDate(0, 0, 7).Add(time.Second)))
	must(s.SweepTimeCardExpirations(ctx, now.AddDate(0, 0, 7).Add(2*time.Second)))
	if got := scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10 AND account_type='cash'"); got != 40000 {
		t.Fatal(got)
	}
	if got := scalar("SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_type=?", timeCardActivationRefund); got != 1 {
		t.Fatal(got)
	}
	filter.Status = "refunded"
	sales, err = s.ListTimeCardSales(ctx, id, filter)
	must(err)
	if sales.Count != 1 || sales.PaidCents != 10000 || sales.RefundedCents != 10000 || sales.Items[0].AssetID != second {
		t.Fatal(sales)
	}
	start := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	exec("UPDATE biz_time_card_assets SET created_at=? WHERE id=?", start, second)
	exec("UPDATE biz_time_card_assets SET created_at=? WHERE id=?", end, first)
	bounded, err := s.ListWorkspaceTimeCards(ctx, start, end)
	must(err)
	if bounded[0].SaleCount != 0 || bounded[1].SaleCount != 1 || bounded[1].NetCents != 0 {
		t.Fatalf("bounded catalog totals should contain only the refunded sale: %+v", bounded)
	}
	all, err := s.ListWorkspaceTimeCards(ctx, time.Time{}, time.Time{})
	must(err)
	if all[1].SaleCount != 2 || all[1].NetCents != 10000 {
		t.Fatalf("all-time catalog totals: %+v", all)
	}
	exec("UPDATE biz_time_card_assets SET created_at=? WHERE id IN (?,?)", now, first, second)
	filter.Status = ""
	sales, err = s.ListTimeCardSales(ctx, id, filter)
	must(err)
	if got := sales.Items[1].RefundableCents; got != 2500 {
		t.Fatal(got)
	}
	if err := s.RequestTimeCardExpiryRefund(ctx, id, first, 210, 2501, "客户补偿", "主管"); !errors.Is(err, ErrTimeCardRefundAmount) {
		t.Fatal(err)
	}

	must(s.RequestTimeCardExpiryRefund(ctx, id, first, 210, 2500, "客户补偿", "主管"))
	task := scalar("SELECT MAX(id) FROM staff_approval_tasks")
	must(s.RejectStaffFinanceTask(ctx, task, 211))
	must(s.RequestTimeCardExpiryRefund(ctx, id, first, 210, 2500, "补充退款说明", "主管"))
	task = scalar("SELECT MAX(id) FROM staff_approval_tasks")
	if got := scalar("SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_type=?", timeCardResidualRefund); got != 0 {
		t.Fatal(got)
	}
	if err := s.RequestTimeCardExpiryRefund(ctx, id, first, 210, 2500, "重复申请", "主管"); !errors.Is(err, ErrTimeCardRefundUnavailable) {
		t.Fatal(err)
	}
	if err := s.ApproveStaffFinanceTask(ctx, task, 210); !errors.Is(err, ErrFinanceDistinctReviewer) {
		t.Fatal(err)
	}
	must(s.ApproveStaffFinanceTask(ctx, task, 211))
	if err := s.ApproveStaffFinanceTask(ctx, task, 211); err == nil {
		t.Fatal("double approval accepted")
	}
	if got := scalar("SELECT balance_cents FROM fin_wallet_accounts WHERE tenant_id=10 AND account_type='reward'"); got != 2500 {
		t.Fatal(got)
	}
	if got := scalar("SELECT COUNT(*) FROM fin_wallet_ledger WHERE business_type=?", timeCardResidualRefund); got != 1 {
		t.Fatal(got)
	}
	if got := scalar("SELECT revision FROM work_inbox_revisions WHERE topic='finance'"); got != 4 {
		t.Fatal(got)
	}
	sales, err = s.ListTimeCardSales(ctx, id, filter)
	must(err)
	if sales.RefundedCents != 12500 || sales.Items[1].ManualRefundStatus != "success" || len(sales.Items[1].Refunds) != 2 {
		t.Fatalf("unexpected refund history %+v", sales)
	}
	withdrawal, err := s.CreateCustomerWalletWithdrawal(ctx, 10, 110, "reward", 2500)
	must(err)
	if withdrawal.AmountCents != 2500 {
		t.Fatal(withdrawal)
	}
	_, err = s.SetCommercialTimeCardStatus(ctx, 210, id, "archived")
	must(err)
	if err := s.SaveWorkspaceTimeCard(ctx, 210, id, terms); !errors.Is(err, ErrTimeCardArchived) {
		t.Fatal(err)
	}
	sales, err = s.ListTimeCardSales(ctx, id, TimeCardSalesQuery{Reference: code + "00000001", Page: 1, PageSize: 100})
	must(err)
	if sales.Count != 1 {
		t.Fatal(fmt.Sprint(sales))
	}

	t.Run("internal time card", func(t *testing.T) {
		card, err := s.SetCommercialTimeCardStatus(ctx, 210, other, "internal")
		if err != nil || card.Status != "internal" {
			t.Fatal(card, err)
		}
		campaignOffers, err := s.ListInternalTimeCardOffers(ctx)
		if err != nil || len(campaignOffers) != 1 || campaignOffers[0].ID != other ||
			campaignOffers[0].ActiveVersion.PriceCents != 2500 {
			t.Fatal(campaignOffers, err)
		}
		checkout := model.CreateCustomerShopOrderInput{ProductType: "time_card", ProductID: other, IdempotencyKey: "internal-card"}
		if _, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("direct checkout must be rejected", err)
		}
		checkout.MarketingCampaignID = 999
		checkout.MarketingPlacement = "shop"
		if _, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout); err == nil {
			t.Fatal("invalid campaign must be rejected")
		}
		campaign, err := s.CreateMarketingCampaign(ctx, 210, model.MarketingCampaignInput{
			Code: "internal-free", Name: "仅活动领取", Status: "active", DisplayLocations: []string{"shop"},
			Items: []model.MarketingCampaignItem{{TargetType: "time_card", TargetID: other, PricingMode: "free", Quantity: 1, PackageMonths: 1}},
		})
		if err != nil {
			t.Fatal(err)
		}
		checkout.MarketingCampaignID = campaign.ID
		order, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout)
		if err != nil || order.PayableAmountCents != 0 {
			t.Fatal(order, err)
		}
		if _, err := s.ClaimFreeMarketingOrder(ctx, 10, 110, order.ID); err != nil {
			t.Fatal(err)
		}

		if _, err := s.SetCommercialTimeCardStatus(ctx, 210, other, "inactive"); err != nil {
			t.Fatal(err)
		}
		checkout.IdempotencyKey = "inactive-card"
		if _, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout); !errors.Is(err, sql.ErrNoRows) {
			t.Fatal("an unlisted product must be unavailable in campaigns", err)
		}
	})

	t.Run("membership payments are one-off", func(t *testing.T) {
		if err := s.SaveWorkspaceMembership(ctx, 210, 0, WorkspaceMembershipInput{
			Name: "月度会员", PriceCents: 1000, IncludedSeconds: 3600, AllowAutoRenew: true,
		}); err != nil {
			t.Fatal(err)
		}
		memberPlan := scalar("SELECT id FROM catalog_membership_plans WHERE name='月度会员'")
		if n := scalar("SELECT allow_auto_renew FROM catalog_membership_plan_versions WHERE plan_id=?", memberPlan); n != 0 {
			t.Fatal("workspace accepted auto-renewal")
		}
		if _, err := s.SetCommercialMembershipStatus(ctx, 210, memberPlan, "active"); err != nil {
			t.Fatal(err)
		}
		exec("UPDATE catalog_membership_plan_versions SET allow_auto_renew=1 WHERE plan_id=?", memberPlan)
		order, err := s.CreateCustomerShopOrder(ctx, 10, 110, model.CreateCustomerShopOrderInput{
			ProductType: "membership", ProductID: memberPlan, MembershipCycle: "recurring_month", IdempotencyKey: "one-off-membership",
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.SandboxPayCustomerShopOrder(ctx, 10, 110, order.ID, model.SandboxPayOrderInput{AmountCents: 1000}); err != nil {
			t.Fatal(err)
		}
		if n := scalar("SELECT COUNT(*) FROM biz_memberships WHERE source_order_id=? AND auto_renew=0", order.ID); n != 1 {
			t.Fatal("historical flag enabled auto-renewal on a one-off purchase")
		}
	})

	t.Run("campaign stock covers all targets and releases a cancelled hold", func(t *testing.T) {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		_, err := s.SetCommercialTimeCardStatus(ctx, 210, id, "active")
		must(err)
		_, err = s.SetCommercialTimeCardStatus(ctx, 210, other, "internal")
		must(err)

		limit := uint32(1)
		input := model.MarketingCampaignInput{
			Name: "库存测试", Status: "active", StockLimit: &limit, DisplayLocations: []string{"shop"},
			Items: []model.MarketingCampaignItem{
				{TargetType: "time_card", TargetID: id, Quantity: 1, PricingMode: "free"},
				{TargetType: "time_card", TargetID: other, Quantity: 2, PricingMode: "discount", DiscountBPS: 9000},
			},
		}
		campaign, err := s.CreateMarketingCampaign(ctx, 210, input)
		must(err)

		shopInput := func(target int64, key string) model.CreateCustomerShopOrderInput {
			return model.CreateCustomerShopOrderInput{ProductType: "time_card", ProductID: target, MarketingCampaignID: campaign.ID, Quantity: 1, IdempotencyKey: key}
		}
		checkStock := func(used, reserved, remaining uint64) {
			t.Helper()
			rows, err := s.ListMarketingWorkspace(ctx, time.Time{}, time.Time{})
			must(err)
			for _, row := range rows {
				if row.ID == campaign.ID {
					if row.Stock == nil || row.Stock.Used != used || row.Stock.Reserved != reserved || row.Stock.Remaining != remaining {
						t.Fatalf("unexpected campaign stock: %+v", row.Stock)
					}
					return
				}
			}
			t.Fatal("campaign missing")
		}
		checkStock(0, 0, 1)
		offers, err := s.ListMarketingCampaigns(ctx, "", 0, true, "shop")
		must(err)
		must(s.AnnotateCampaignEligibility(ctx, 10, offers))
		seen := false
		for _, offer := range offers {
			if offer.ID == campaign.ID {
				seen = true
				if offer.RemainingStock == nil || *offer.RemainingStock != 1 {
					t.Fatal("limited customer offer missing stock", offer.RemainingStock)
				}
			} else if offer.RemainingStock != nil {
				t.Fatal("unlimited offer should have no numeric stock")
			}
		}
		if !seen {
			t.Fatal("limited offer missing before sellout")
		}

		stale, err := s.db.BeginTx(ctx, nil)
		must(err)
		defer stale.Rollback()
		_, err = readMarketingStock(ctx, stale, campaign.ID, 0, limit, false)
		must(err)

		first, err := s.CreateCustomerShopOrder(ctx, 10, 110, shopInput(id, "stock-first"))
		must(err)
		stock, err := readMarketingStock(ctx, stale, campaign.ID, 0, limit, true)
		if err != nil || stock.Remaining != 0 || stock.Reserved != 1 {
			t.Fatal("reservation invisible to stale transaction", stock, err)
		}
		must(stale.Rollback())
		checkStock(0, 1, 0)
		must(s.UpdateWorkspaceMarketingStock(ctx, campaign.ID, 210, 2, 1))
		checkStock(0, 1, 1)
		if err := s.UpdateWorkspaceMarketingStock(ctx, campaign.ID, 210, 3, 1); !errors.Is(err, ErrMarketingCampaignStockChanged) {
			t.Fatal("stale stock update accepted", err)
		}
		must(s.UpdateWorkspaceMarketingStock(ctx, campaign.ID, 210, 1, 2))
		checkStock(0, 1, 0)

		if _, err := s.CreateCustomerShopOrder(ctx, 10, 110, shopInput(other, "stock-blocked")); err == nil {
			t.Fatal("another target exceeded activity stock")
		}
		_, err = s.CancelCustomerShopOrder(ctx, 10, first.ID)
		must(err)
		checkStock(0, 0, 1)

		free, err := s.CreateCustomerShopOrder(ctx, 10, 110, shopInput(id, "stock-free"))
		must(err)
		_, err = s.ClaimFreeMarketingOrder(ctx, 10, 110, free.ID)
		must(err)
		_, err = s.ClaimFreeMarketingOrder(ctx, 10, 110, free.ID)
		must(err)
		checkStock(1, 0, 0)
		active, err := s.ListMarketingCampaigns(ctx, "", 0, true, "shop")
		must(err)
		for _, displayed := range active {
			if displayed.ID == campaign.ID {
				t.Fatal("sold-out campaign still in shop")
			}
		}
		if err := s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "active"); !errors.Is(err, ErrMarketingCampaignStockInsufficient) {
			t.Fatal("sold-out relisting accepted", err)
		}

		input.StockLimit = new(uint32)
		*input.StockLimit = 2
		campaign, err = s.UpdateWorkspaceMarketingCampaign(ctx, campaign.ID, 210, input)
		must(err)
		checkStock(1, 0, 1)
		paid, err := s.CreateCustomerShopOrder(ctx, 10, 110, shopInput(other, "stock-paid"))
		must(err)
		if paid.PayableAmountCents == 0 {
			t.Fatal("expected a non-free checkout")
		}
		_, err = s.SandboxPayCustomerShopOrder(ctx, 10, 110, paid.ID, model.SandboxPayOrderInput{AmountCents: paid.PayableAmountCents})
		must(err)
		checkStock(2, 0, 0)
		input.StockLimit = &limit
		if _, err := s.UpdateWorkspaceMarketingCampaign(ctx, campaign.ID, 210, input); !errors.Is(err, ErrMarketingCampaignStockBelowUsage) {
			t.Fatal("stock less than consumed was accepted", err)
		}
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "archived"))
	})

	t.Run("marketing lifecycle and paid participation", func(t *testing.T) {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}
		exec := func(query string, args ...any) {
			t.Helper()
			_, err := s.db.ExecContext(ctx, query, args...)
			must(err)
		}

		limit := uint32(20)
		input := model.MarketingCampaignInput{
			Name: "工作台促销", Status: "inactive", DisplayLocations: []string{"shop"}, StockLimit: &limit,
			Items: []model.MarketingCampaignItem{{TargetType: "time_card", TargetID: id, PricingMode: "discount", DiscountBPS: 9000, Quantity: 1}},
		}
		campaign, err := s.CreateMarketingCampaign(ctx, 210, input)
		must(err)
		if campaign.Code == "" || campaign.Status != "inactive" {
			t.Fatal(campaign)
		}
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "active"))
		input.Name = "工作台促销更新"
		campaign, err = s.UpdateWorkspaceMarketingCampaign(ctx, campaign.ID, 210, input)
		must(err)
		if campaign.Status != "active" || campaign.Name != input.Name {
			t.Fatal(campaign)
		}
		if err := s.DeleteWorkspaceMarketingCampaign(ctx, campaign.ID); !errors.Is(err, ErrMarketingCampaignMustUnlist) {
			t.Fatal("active campaign deletion should be blocked", err)
		}
		if _, err := s.SetCommercialTimeCardStatus(ctx, 210, id, "active"); err != nil {
			t.Fatal(err)
		}
		checkout := model.CreateCustomerShopOrderInput{
			ProductType: "time_card", ProductID: id, MarketingCampaignID: campaign.ID, Quantity: 1, IdempotencyKey: "campaign-stats-paid",
		}
		order, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout)
		must(err)
		order, err = s.SandboxPayCustomerShopOrder(ctx, 10, 110, order.ID, model.SandboxPayOrderInput{AmountCents: order.PayableAmountCents})
		must(err)
		if order.PaidAmountCents <= 200 {
			t.Fatal(order.PaidAmountCents)
		}
		start := time.Date(2026, 10, 31, 16, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 1, 0)
		exec("UPDATE biz_orders SET paid_at=?, refunded_amount_cents=200, status='partially_refunded' WHERE id=?", start, order.ID)
		exec("INSERT INTO mkt_campaign_usage(campaign_id,tenant_id,status,created_at,quantity) VALUES (?,10,'consumed',?,1),(?,10,'consumed',?,1)",
			campaign.ID, end.Add(-time.Millisecond), campaign.ID, end)
		checkout.IdempotencyKey = "campaign-stats-pending"
		pending, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout)
		must(err)
		find := func(start, end time.Time) MarketingWorkspaceCampaign {
			rows, err := s.ListMarketingWorkspace(ctx, start, end)
			must(err)
			for _, row := range rows {
				if row.ID == campaign.ID {
					return row
				}
			}
			t.Fatalf("campaign %d missing from workspace", campaign.ID)
			return MarketingWorkspaceCampaign{}
		}
		row := find(start, end)
		if row.ParticipationCount != 2 || row.ParticipationAmountCents != order.PaidAmountCents-200 || !row.HasOrders || !row.HasPendingOrders {
			t.Fatalf("bounded participation %+v", row)
		}
		if all := find(time.Time{}, time.Time{}); all.ParticipationCount != 3 || all.ParticipationAmountCents != row.ParticipationAmountCents {
			t.Fatalf("unbounded participation %+v", all)
		}
		unusedInput := input
		unusedInput.Name = "可删除计划"
		unusedInput.SortOrder = 80
		unused, err := s.CreateMarketingCampaign(ctx, 210, unusedInput)
		must(err)
		rows, err := s.ListMarketingWorkspace(ctx, time.Time{}, time.Time{})
		must(err)
		var previous []int64
		next := []int64{unused.ID}
		for _, row := range rows {
			if row.Status != "archived" {
				previous = append(previous, row.ID)
				if row.ID != unused.ID {
					next = append(next, row.ID)
				}
			}
		}
		duplicate := append([]int64(nil), next...)
		duplicate[1] = unused.ID
		if err := s.ReorderWorkspaceMarketingCampaigns(ctx, 210, duplicate, previous); !errors.Is(err, ErrMarketingCampaignOrderChanged) {
			t.Fatal("duplicate IDs must be rejected", err)
		}
		must(s.ReorderWorkspaceMarketingCampaigns(ctx, 210, next, previous))
		if err := s.ReorderWorkspaceMarketingCampaigns(ctx, 210, next, previous); !errors.Is(err, ErrMarketingCampaignOrderChanged) {
			t.Fatal("stale sorting must be rejected", err)
		}
		rows, err = s.ListMarketingWorkspace(ctx, time.Time{}, time.Time{})
		must(err)
		var actual []int64
		for _, row := range rows {
			if row.Status != "archived" {
				actual = append(actual, row.ID)
			}
		}
		if !slices.Equal(actual, next) {
			t.Fatalf("sort order not persisted: %v, want %v", actual, next)
		}
		ranked := find(start, end)
		if ranked.ParticipationCount != row.ParticipationCount || ranked.ParticipationAmountCents != row.ParticipationAmountCents || !ranked.HasPendingOrders {
			t.Fatal("sorting changed participation stats or pending orders", ranked)
		}

		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "inactive"))
		attempt, err := s.PrepareWechatShopPayment(ctx, 10, 110, pending.ID, model.WechatPaymentIdentity{
			AppID: "archive-app", MchID: "archive-mch", OpenID: "archive-customer",
		})
		must(err)
		if err := s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "archived"); !errors.Is(err, ErrMarketingCampaignArchivePaymentUnconfirmed) {
			t.Fatal("archive should wait for uncertain payments", err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		must(err)
		reserveErr := reserveMarketingCampaignOrderTx(ctx, tx, 10, pending.ID, campaign, campaign.Items[0], 10000, 9000)
		must(tx.Rollback())
		if !errors.Is(reserveErr, ErrMarketingCampaignUnavailable) {
			t.Fatal("stale storefront should not reserve", reserveErr)
		}
		must(s.UpdateWechatTradeState(ctx, attempt.PaymentNo, "CLOSED"))
		_, err = s.CancelCustomerShopOrder(ctx, 10, pending.ID)
		must(err)
		if err := s.DeleteWorkspaceMarketingCampaign(ctx, campaign.ID); !errors.Is(err, ErrMarketingCampaignHistoryLocked) {
			t.Fatal("historical orders must be kept", err)
		}
		input.Name = "历史活动修改"
		input.PreserveSortOrder = true
		campaign, err = s.UpdateWorkspaceMarketingCampaign(ctx, campaign.ID, 210, input)
		must(err)
		if campaign.Status != "inactive" || campaign.SortOrder != ranked.SortOrder {
			t.Fatal("editing an old draft overwrote the new sort order", campaign)
		}
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "active"))
		checkout.IdempotencyKey = "campaign-archive-abandoned"
		unpaid, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout)
		must(err)
		checkout.IdempotencyKey = "campaign-archive-closed"
		closed, err := s.CreateCustomerShopOrder(ctx, 10, 110, checkout)
		must(err)
		attempt, err = s.PrepareWechatShopPayment(ctx, 10, 110, closed.ID, model.WechatPaymentIdentity{
			AppID: "archive-app", MchID: "archive-mch", OpenID: "archive-customer",
		})
		must(err)
		must(s.UpdateWechatTradeState(ctx, attempt.PaymentNo, "CLOSED"))
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "inactive"))

		exec(`INSERT INTO fin_customer_receipts
			(receipt_no, tenant_id, channel, purpose, amount_cents, order_id, external_trade_no,
			 evidence, occurred_at, requester_user_id, last_submitter_user_id, request_hash, idempotency_key, evidence_key)
			VALUES ('ARCHIVE-RECEIPT',10,'bank','order',?,?,'archive-trade',
			        'payment evidence',UTC_TIMESTAMP(3),210,210,'archive-request','archive-receipt','archive-evidence')`,
			closed.PayableAmountCents, closed.ID)
		if err := s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "archived"); !errors.Is(err, ErrMarketingCampaignArchivePaymentUnconfirmed) {
			t.Fatal("archive should wait for manual payment verification", err)
		}
		if count := scalar("SELECT COUNT(*) FROM biz_orders WHERE id IN (?,?) AND status='pending'", unpaid.ID, closed.ID); count != 2 {
			t.Fatal("blocked archive did not roll back cancellations", count)
		}
		exec("UPDATE fin_customer_receipts SET status='rejected' WHERE receipt_no='ARCHIVE-RECEIPT'")
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "archived"))
		must(s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "archived"))
		if count := scalar("SELECT COUNT(*) FROM biz_orders WHERE id IN (?,?) AND status='cancelled' AND cancelled_at IS NOT NULL", unpaid.ID, closed.ID); count != 2 {
			t.Fatal("unpaid orders were not cancelled", count)
		}
		if reserved := scalar("SELECT SUM(reserved_quantity) FROM mkt_campaign_inventory WHERE campaign_id=?", campaign.ID); reserved != 0 {
			t.Fatal("cancelled orders still hold campaign stock", reserved)
		}
		if audits := scalar(`SELECT COUNT(*) FROM biz_audit_events WHERE action='marketing.campaign.archive'
			AND entity_id=? AND actor_user_id=210 AND JSON_LENGTH(after_json, '$.cancelled_order_ids')=2`, fmt.Sprint(campaign.ID)); audits != 1 {
			t.Fatal("archive cancellations should be audited once", audits)
		}
		if found := find(start, end); found.Status != "archived" || found.ParticipationCount != 2 || found.HasPendingOrders {
			t.Fatal(found)
		}
		if err := s.SetMarketingCampaignStatus(ctx, campaign.ID, 210, "active"); !errors.Is(err, ErrMarketingCampaignArchived) {
			t.Fatal(err)
		}
		legacy, err := s.ListMarketingCampaigns(ctx, "", 0, false)
		must(err)
		for _, item := range legacy {
			if item.ID == campaign.ID {
				t.Fatal("archived campaign in legacy catalog")
			}
		}

		if err := s.ReorderWorkspaceMarketingCampaigns(ctx, 210, previous, next); !errors.Is(err, ErrMarketingCampaignOrderChanged) {
			t.Fatal("archived campaign must invalidate a stale sort", err)
		}
		if found := find(start, end); found.SortOrder != ranked.SortOrder {
			t.Fatal("archived order changed", found)
		}
		must(s.DeleteWorkspaceMarketingCampaign(ctx, unused.ID))
	})

}
