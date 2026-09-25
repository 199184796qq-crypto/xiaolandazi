package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed commercial_schema.sql
var commercialSchema string

//go:embed beneficiary_wallet_schema.sql
var beneficiaryWalletSchema string

// MigrateCommercial creates the stable commercial foundation used by
// memberships, wallet, quota, referral, internal sales commission and
// external-agent settlement.
//
// Invariants:
//   - money uses integer cents
//   - durations use integer seconds
//   - discounts/rates use basis points (10000 = 100%)
//   - historical money/quota/earning entries are append-only
//   - mutable commercial configuration is versioned
//   - source, internal sales, external agent and customer referral are separate
func (s *Store) MigrateCommercial(ctx context.Context) error {
	schema := strings.ReplaceAll(commercialSchema, "\r\n", "\n")
	for _, raw := range strings.Split(schema, "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply commercial schema: %w", err)
		}
	}

	beneficiarySchema := strings.ReplaceAll(beneficiaryWalletSchema, "\r\n", "\n")
	for _, raw := range strings.Split(beneficiarySchema, "\n-- +statement\n") {
		statement := strings.TrimSpace(raw)
		if statement == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply beneficiary wallet schema: %w", err)
		}
	}

	if err := s.migrateCustomerBusiness(ctx); err != nil {
		return err
	}

	customerCooperationColumns := []struct {
		name string
		sql  string
	}{
		{"cooperation_status", "ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_status VARCHAR(32) NOT NULL DEFAULT 'cooperating' AFTER source_note"},
		{"cooperation_note", "ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_note VARCHAR(512) NOT NULL DEFAULT '' AFTER cooperation_status"},
		{"cooperation_marked_at", "ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_marked_at DATETIME(3) NULL AFTER cooperation_note"},
		{"cooperation_marked_by_user_id", "ALTER TABLE crm_customer_profiles ADD COLUMN cooperation_marked_by_user_id BIGINT UNSIGNED NULL AFTER cooperation_marked_at"},
	}
	for _, column := range customerCooperationColumns {
		exists, err := s.columnExists(ctx, "crm_customer_profiles", column.name)
		if err != nil {
			return err
		}
		if !exists {
			if _, err := s.db.ExecContext(ctx, column.sql); err != nil {
				return fmt.Errorf("add crm_customer_profiles.%s: %w", column.name, err)
			}
		}
	}

	if err := s.migrateLegacyMembershipMarketingCampaigns(ctx); err != nil {
		return fmt.Errorf("migrate legacy membership marketing: %w", err)
	}
	if err := s.migrateFeatureMarketingCampaigns(ctx); err != nil {
		return fmt.Errorf("migrate feature marketing campaigns: %w", err)
	}

	// Campaign display placement is intentionally independent from campaign
	// pricing. Existing membership-only campaigns belong in the membership
	// center; mixed/time-card/device campaigns keep the historical shop
	// behavior. Operators can move any campaign to backoffice-only afterwards.
	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO mkt_campaign_placements (campaign_id, placement_code, sort_order)
		SELECT
			c.id,
			CASE
				WHEN SUM(CASE WHEN i.target_type='membership' THEN 0 ELSE 1 END)=0
					 AND SUM(CASE WHEN i.target_type='membership' THEN 1 ELSE 0 END)>0
				THEN 'membership'
				ELSE 'shop'
			END,
			10
		FROM mkt_campaigns c
		INNER JOIN mkt_campaign_items i ON i.campaign_id=c.id
		WHERE NOT EXISTS (
			SELECT 1
			FROM mkt_campaign_placements p
			WHERE p.campaign_id=c.id
		)
		GROUP BY c.id
	`); err != nil {
		return fmt.Errorf("backfill marketing campaign placements: %w", err)
	}

	hasTimeCardActivationMode, err := s.columnExists(ctx, "catalog_time_card_versions", "activation_mode")
	if err != nil {
		return err
	}
	if !hasTimeCardActivationMode {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_time_card_versions
			ADD COLUMN activation_mode VARCHAR(32) NOT NULL DEFAULT 'first_use' AFTER validity_days
		`); err != nil {
			return fmt.Errorf("add catalog_time_card_versions.activation_mode: %w", err)
		}
	}

	hasTimeCardActivationDeadlineDays, err := s.columnExists(ctx, "catalog_time_card_versions", "activation_deadline_days")
	if err != nil {
		return err
	}
	if !hasTimeCardActivationDeadlineDays {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_time_card_versions
			ADD COLUMN activation_deadline_days INT UNSIGNED NOT NULL DEFAULT 0 AFTER activation_mode
		`); err != nil {
			return fmt.Errorf("add catalog_time_card_versions.activation_deadline_days: %w", err)
		}
	}

	hasOrderActivationModeSnapshot, err := s.columnExists(ctx, "biz_order_items", "activation_mode_snapshot")
	if err != nil {
		return err
	}
	if !hasOrderActivationModeSnapshot {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE biz_order_items
			ADD COLUMN activation_mode_snapshot VARCHAR(32) NULL AFTER validity_days_snapshot
		`); err != nil {
			return fmt.Errorf("add biz_order_items.activation_mode_snapshot: %w", err)
		}
	}

	hasOrderActivationDeadlineDaysSnapshot, err := s.columnExists(ctx, "biz_order_items", "activation_deadline_days_snapshot")
	if err != nil {
		return err
	}
	if !hasOrderActivationDeadlineDaysSnapshot {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE biz_order_items
			ADD COLUMN activation_deadline_days_snapshot INT UNSIGNED NULL AFTER activation_mode_snapshot
		`); err != nil {
			return fmt.Errorf("add biz_order_items.activation_deadline_days_snapshot: %w", err)
		}
	}

	hasTimeCardAssetRemainingSeconds, err := s.columnExists(ctx, "biz_time_card_assets", "remaining_seconds")
	if err != nil {
		return err
	}
	if !hasTimeCardAssetRemainingSeconds {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE biz_time_card_assets
			ADD COLUMN remaining_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER original_seconds
		`); err != nil {
			return fmt.Errorf("add biz_time_card_assets.remaining_seconds: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `
			UPDATE biz_time_card_assets
			SET remaining_seconds=original_seconds
			WHERE remaining_seconds=0 AND status='unactivated'
		`); err != nil {
			return fmt.Errorf("backfill biz_time_card_assets.remaining_seconds: %w", err)
		}
	}

	hasAvatarURL, err := s.columnExists(ctx, "mgmt_users", "avatar_url")
	if err != nil {
		return err
	}
	if !hasAvatarURL {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE mgmt_users
			ADD COLUMN avatar_url VARCHAR(1024) NOT NULL DEFAULT '' AFTER display_name
		`); err != nil {
			return fmt.Errorf("add mgmt_users.avatar_url: %w", err)
		}
	}

	hasSettlementCreator, err := s.columnExists(ctx, "inc_settlement_batches", "created_by_user_id")
	if err != nil {
		return err
	}
	if !hasSettlementCreator {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE inc_settlement_batches
			ADD COLUMN created_by_user_id BIGINT UNSIGNED NULL AFTER status
		`); err != nil {
			return fmt.Errorf("add inc_settlement_batches.created_by_user_id: %w", err)
		}
	}

	hasExternalContractNo, err := s.columnExists(ctx, "crm_agent_contracts", "external_contract_no")
	if err != nil {
		return err
	}
	if !hasExternalContractNo {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE crm_agent_contracts
			ADD COLUMN external_contract_no VARCHAR(96) NOT NULL DEFAULT '' AFTER contract_no
		`); err != nil {
			return fmt.Errorf("add crm_agent_contracts.external_contract_no: %w", err)
		}
	}

	hasDeviceImageURL, err := s.columnExists(ctx, "catalog_device_products", "image_url")
	if err != nil {
		return err
	}
	if !hasDeviceImageURL {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_device_products
			ADD COLUMN image_url VARCHAR(2048) NOT NULL DEFAULT '' AFTER description
		`); err != nil {
			return fmt.Errorf("add catalog_device_products.image_url: %w", err)
		}
	}
	hasDeviceUnitCode, err := s.columnExists(ctx, "catalog_device_products", "unit_code")
	if err != nil {
		return err
	}
	if !hasDeviceUnitCode {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_device_products
			ADD COLUMN unit_code VARCHAR(96) NOT NULL DEFAULT 'unit' AFTER image_url
		`); err != nil {
			return fmt.Errorf("add catalog_device_products.unit_code: %w", err)
		}
	}

	hasDeviceSalesStock, err := s.columnExists(ctx, "catalog_device_products", "sales_stock")
	if err != nil {
		return err
	}
	if !hasDeviceSalesStock {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_device_products
			ADD COLUMN sales_stock INT UNSIGNED NOT NULL DEFAULT 0 AFTER unit_code
		`); err != nil {
			return fmt.Errorf("add catalog_device_products.sales_stock: %w", err)
		}
		inventoryExists, err := s.tableExists(ctx, "inv_devices")
		if err != nil {
			return err
		}
		if inventoryExists {
			if _, err := s.db.ExecContext(ctx, `
				UPDATE catalog_device_products p
				SET sales_stock=(
					SELECT COUNT(*)
					FROM inv_devices d
					WHERE d.sku_code=p.sku_code AND d.lifecycle_status='IN_STOCK'
				)
			`); err != nil {
				return fmt.Errorf("backfill catalog_device_products.sales_stock: %w", err)
			}
		}
	}

	hasDeviceHoldExpiresAt, err := s.columnExists(ctx, "biz_order_devices", "hold_expires_at")
	if err != nil {
		return err
	}
	if !hasDeviceHoldExpiresAt {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE biz_order_devices
			ADD COLUMN hold_expires_at DATETIME(3) NULL AFTER reserved_at
		`); err != nil {
			return fmt.Errorf("add biz_order_devices.hold_expires_at: %w", err)
		}
	}

	hasDeviceCostPrice, err := s.columnExists(ctx, "catalog_device_versions", "cost_price_cents")
	if err != nil {
		return err
	}
	if !hasDeviceCostPrice {
		if _, err := s.db.ExecContext(ctx, `
			ALTER TABLE catalog_device_versions
			ADD COLUMN cost_price_cents BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER currency
		`); err != nil {
			return fmt.Errorf("add catalog_device_versions.cost_price_cents: %w", err)
		}
	}

	// Earlier device editor versions labelled the first price as "原价", but
	// operators actually entered the device cost there (for example cost 80,
	// sale 299). Preserve that business meaning by moving those rows into the
	// dedicated cost column and using the sale price as the customer list price.
	if _, err := s.db.ExecContext(ctx, `
		UPDATE catalog_device_versions
		SET cost_price_cents=list_price_cents,
		    list_price_cents=sale_price_cents
		WHERE cost_price_cents=0
		  AND list_price_cents>0
		  AND sale_price_cents>list_price_cents
	`); err != nil {
		return fmt.Errorf("normalize device cost/list prices: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE catalog_device_versions
		SET list_price_cents=sale_price_cents
		WHERE list_price_cents=0 AND sale_price_cents>0
	`); err != nil {
		return fmt.Errorf("normalize device list price: %w", err)
	}

	membershipDiscountColumns := []struct {
		name       string
		definition string
	}{
		{"recurring_month_discount_bps", "INT UNSIGNED NOT NULL DEFAULT 10000 AFTER price_cents"},
		{"recurring_quarter_discount_bps", "INT UNSIGNED NOT NULL DEFAULT 10000 AFTER recurring_month_discount_bps"},
		{"annual_discount_bps", "INT UNSIGNED NOT NULL DEFAULT 10000 AFTER recurring_quarter_discount_bps"},
		{"default_device_discount_bps", "INT UNSIGNED NOT NULL DEFAULT 10000 AFTER default_time_card_discount_bps"},
	}
	for _, column := range membershipDiscountColumns {
		exists, err := s.columnExists(ctx, "catalog_membership_plan_versions", column.name)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("ALTER TABLE catalog_membership_plan_versions ADD COLUMN %s %s", column.name, column.definition),
		); err != nil {
			return fmt.Errorf("add catalog_membership_plan_versions.%s: %w", column.name, err)
		}
	}

	// Early membership editor versions stored values such as 9 / 8 as 900 / 800
	// BPS, which means 0.9 / 0.8 discount instead of the intended 9 / 8 discount.
	// Values below 1000 in these legacy rows are normalized once by multiplication;
	// already-correct 1折 (1000 BPS) and above are left untouched, keeping startup
	// migration idempotent.
	for _, column := range []string{
		"recurring_month_discount_bps",
		"recurring_quarter_discount_bps",
		"annual_discount_bps",
		"default_time_card_discount_bps",
		"default_device_discount_bps",
	} {
		if _, err := s.db.ExecContext(ctx,
			fmt.Sprintf("UPDATE catalog_membership_plan_versions SET %s=%s*10 WHERE %s BETWEEN 100 AND 999", column, column, column),
		); err != nil {
			return fmt.Errorf("normalize membership discount %s: %w", column, err)
		}
	}
	hasWalletFrozen, err := s.columnExists(ctx, "fin_wallet_accounts", "frozen_balance_cents")
	if err != nil {
		return err
	}
	if !hasWalletFrozen {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE fin_wallet_accounts ADD COLUMN frozen_balance_cents BIGINT NOT NULL DEFAULT 0 AFTER balance_cents`); err != nil {
			return fmt.Errorf("add fin_wallet_accounts.frozen_balance_cents: %w", err)
		}
	}

	hasMembershipReferral, err := s.columnExists(ctx, "catalog_membership_plan_versions", "participates_referral")
	if err != nil {
		return err
	}
	if !hasMembershipReferral {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE catalog_membership_plan_versions ADD COLUMN participates_referral TINYINT(1) NOT NULL DEFAULT 1 AFTER allow_auto_renew`); err != nil {
			return fmt.Errorf("add catalog_membership_plan_versions.participates_referral: %w", err)
		}
	}

	// backfill customer commercial foundation for tenants created before the
	// commercial domain existed. No sales/agent/referral relationship is inferred.
	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO crm_customer_profiles (tenant_id, source_type)
		SELECT DISTINCT tenant_id, 'direct'
		FROM mgmt_users
		WHERE role = 'customer' AND tenant_id IS NOT NULL
	`); err != nil {
		return fmt.Errorf("backfill customer profiles: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO fin_wallet_accounts (
			tenant_id, account_type, currency, balance_cents, status
		)
		SELECT DISTINCT tenant_id, 'cash', 'CNY', 0, 'active'
		FROM mgmt_users
		WHERE role = 'customer' AND tenant_id IS NOT NULL
	`); err != nil {
		return fmt.Errorf("backfill cash wallets: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `
		INSERT IGNORE INTO fin_wallet_accounts (
			tenant_id, account_type, currency, balance_cents, status
		)
		SELECT DISTINCT tenant_id, 'reward', 'CNY', 0, 'active'
		FROM mgmt_users
		WHERE role = 'customer' AND tenant_id IS NOT NULL
	`); err != nil {
		return fmt.Errorf("backfill reward wallets: %w", err)
	}

	return nil
}
