CREATE TABLE IF NOT EXISTS fin_customer_receipts (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 receipt_no VARCHAR(64) NOT NULL,
 tenant_id BIGINT UNSIGNED NOT NULL,
 channel VARCHAR(32) NOT NULL,
 purpose VARCHAR(32) NOT NULL,
 amount_cents BIGINT UNSIGNED NOT NULL,
 order_id BIGINT UNSIGNED NULL,
 verified_payment_id BIGINT UNSIGNED NULL,
 existing_recharge_id BIGINT UNSIGNED NULL,
 payer_name VARCHAR(128) NOT NULL DEFAULT '',
 receiving_account VARCHAR(128) NOT NULL DEFAULT '',
 external_trade_no VARCHAR(128) NOT NULL,
 evidence VARCHAR(2000) NOT NULL,
 occurred_at DATETIME(3) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending',
 requester_user_id BIGINT UNSIGNED NOT NULL,
 last_submitter_user_id BIGINT UNSIGNED NOT NULL,
 request_hash CHAR(64) NOT NULL,
 reviewer_user_id BIGINT UNSIGNED NULL,
 review_note VARCHAR(1000) NOT NULL DEFAULT '',
 reviewed_at DATETIME(3) NULL,
 posted_at DATETIME(3) NULL,
 recharge_id BIGINT UNSIGNED NULL,
 payment_id BIGINT UNSIGNED NULL,
 version_no INT UNSIGNED NOT NULL DEFAULT 1,
 idempotency_key VARCHAR(128) NOT NULL,
 evidence_key CHAR(64) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_customer_receipt_no(receipt_no),
 UNIQUE KEY uk_customer_receipt_request(requester_user_id,idempotency_key),
 UNIQUE KEY uk_customer_receipt_evidence(evidence_key),
 UNIQUE KEY uk_customer_receipt_verified_payment(verified_payment_id),
 UNIQUE KEY uk_customer_receipt_existing_recharge(existing_recharge_id),
 UNIQUE KEY uk_customer_receipt_recharge(recharge_id),
 UNIQUE KEY uk_customer_receipt_payment(payment_id),
 KEY idx_customer_receipt_queue(status,created_at,id),
 KEY idx_customer_receipt_tenant(tenant_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +statement
CREATE TABLE IF NOT EXISTS fin_customer_confirmations (
 tenant_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
 receipt_id BIGINT UNSIGNED NOT NULL,
 reviewer_user_id BIGINT UNSIGNED NOT NULL,
 confirmed_amount_cents BIGINT UNSIGNED NOT NULL,
 confirmed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_customer_confirmation_receipt(receipt_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +statement
CREATE TABLE IF NOT EXISTS fin_customer_receipt_events (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 receipt_id BIGINT UNSIGNED NOT NULL,
 actor_user_id BIGINT UNSIGNED NOT NULL,
 action VARCHAR(32) NOT NULL,
 note VARCHAR(2000) NOT NULL DEFAULT '',
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 KEY idx_receipt_events(receipt_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +statement
CREATE TABLE IF NOT EXISTS crm_support_tickets (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 ticket_no VARCHAR(64) NOT NULL,
 tenant_id BIGINT UNSIGNED NOT NULL,
 requester_user_id BIGINT UNSIGNED NOT NULL,
 requester_role VARCHAR(32) NOT NULL,
 category VARCHAR(32) NOT NULL,
 title VARCHAR(160) NOT NULL,
 description VARCHAR(2000) NOT NULL,
 contact_name VARCHAR(128) NOT NULL,
 contact_phone VARCHAR(64) NOT NULL,
 preferred_at DATETIME(3) NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending',
 assigned_user_id BIGINT UNSIGNED NULL,
 resolution VARCHAR(2000) NOT NULL DEFAULT '',
 confirmed_by_user_id BIGINT UNSIGNED NULL,
 confirmed_at DATETIME(3) NULL,
 version_no INT UNSIGNED NOT NULL DEFAULT 1,
 idempotency_key VARCHAR(128) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 UNIQUE KEY uk_support_ticket_no(ticket_no),
 UNIQUE KEY uk_support_ticket_request(requester_user_id,idempotency_key),
 KEY idx_support_ticket_tenant(tenant_id,id),
 KEY idx_support_ticket_queue(status,assigned_user_id,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
-- +statement
CREATE TABLE IF NOT EXISTS crm_support_ticket_events (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 ticket_id BIGINT UNSIGNED NOT NULL,
 actor_user_id BIGINT UNSIGNED NOT NULL,
 action VARCHAR(32) NOT NULL,
 status VARCHAR(32) NOT NULL,
 note VARCHAR(2000) NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 KEY idx_support_ticket_events(ticket_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
