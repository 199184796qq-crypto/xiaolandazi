package db

import (
	"context"
	"fmt"
)

func (s *Store) MigrateWechatRefunds(ctx context.Context) error {
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS fin_wechat_cash_refunds (
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		 refund_no VARCHAR(64) NOT NULL, tenant_id BIGINT UNSIGNED NOT NULL,
		 wallet_account_id BIGINT UNSIGNED NOT NULL, user_id BIGINT UNSIGNED NOT NULL,
		 amount_cents BIGINT UNSIGNED NOT NULL, idempotency_key VARCHAR(128) NOT NULL,
		 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		 UNIQUE KEY uk_cash_refund_no(refund_no), UNIQUE KEY uk_cash_refund_key(idempotency_key),
		 KEY idx_cash_refund_tenant(tenant_id,id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
		`CREATE TABLE IF NOT EXISTS fin_wechat_cash_refund_items (
		 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		 request_id BIGINT UNSIGNED NOT NULL, recharge_id BIGINT UNSIGNED NOT NULL,
		 recharge_no VARCHAR(64) NOT NULL, payment_no VARCHAR(64) NOT NULL,
		 transaction_id VARCHAR(128) NOT NULL, app_id VARCHAR(64) NOT NULL,
		 mch_id VARCHAR(64) NOT NULL, payer_hash CHAR(64) NOT NULL,
		 total_cents BIGINT UNSIGNED NOT NULL, amount_cents BIGINT UNSIGNED NOT NULL,
		 refund_no VARCHAR(64) NOT NULL, provider_refund_id VARCHAR(128) NULL,
		 status VARCHAR(32) NOT NULL DEFAULT 'queued', received_account VARCHAR(256) NOT NULL DEFAULT '',
		 message VARCHAR(256) NOT NULL DEFAULT '', success_time DATETIME(3) NULL,
		 attempts INT NOT NULL DEFAULT 0, next_check_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		 submitted_at DATETIME(3) NULL,
		 lease_until DATETIME(3) NULL, lease_token VARCHAR(64) NOT NULL DEFAULT '',
		 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
		 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
		 UNIQUE KEY uk_cash_refund_item_no(refund_no), UNIQUE KEY uk_cash_refund_provider(provider_refund_id),
		 UNIQUE KEY uk_cash_refund_source(request_id,recharge_id),
		 KEY idx_cash_refund_due(status,next_check_at), KEY idx_cash_refund_recharge(recharge_id,id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci`,
	} {
		if _, err := s.db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("migrate wechat refunds: %w", err)
		}
	}
	return nil
}
