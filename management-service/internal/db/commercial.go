package db

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
)

//go:embed commercial_schema.sql
var commercialSchema string

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
