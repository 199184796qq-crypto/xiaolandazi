CREATE TABLE IF NOT EXISTS fin_beneficiary_wallets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    beneficiary_type VARCHAR(32) NOT NULL,
    beneficiary_id BIGINT UNSIGNED NOT NULL,
    currency CHAR(3) NOT NULL DEFAULT 'CNY',
    available_balance_cents BIGINT NOT NULL DEFAULT 0,
    frozen_balance_cents BIGINT NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_beneficiary_wallet (beneficiary_type, beneficiary_id, currency),
    KEY idx_fin_beneficiary_wallet_balance (beneficiary_type, available_balance_cents)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_beneficiary_wallet_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    wallet_id BIGINT UNSIGNED NOT NULL,
    external_id VARCHAR(128) NOT NULL,
    business_type VARCHAR(48) NOT NULL,
    reference_type VARCHAR(48) NOT NULL DEFAULT '',
    reference_id BIGINT UNSIGNED NULL,
    available_delta_cents BIGINT NOT NULL DEFAULT 0,
    frozen_delta_cents BIGINT NOT NULL DEFAULT 0,
    available_before_cents BIGINT NOT NULL,
    available_after_cents BIGINT NOT NULL,
    frozen_before_cents BIGINT NOT NULL,
    frozen_after_cents BIGINT NOT NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_beneficiary_wallet_ledger_external (external_id),
    KEY idx_fin_beneficiary_wallet_ledger_wallet (wallet_id, created_at),
    KEY idx_fin_beneficiary_wallet_ledger_ref (reference_type, reference_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_withdrawal_requests (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    withdrawal_no VARCHAR(64) NOT NULL,
    wallet_id BIGINT UNSIGNED NOT NULL,
    beneficiary_type VARCHAR(32) NOT NULL,
    beneficiary_id BIGINT UNSIGNED NOT NULL,
    amount_cents BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'reviewing',
    requested_by_user_id BIGINT UNSIGNED NOT NULL,
    approved_by_user_id BIGINT UNSIGNED NULL,
    paid_by_user_id BIGINT UNSIGNED NULL,
    reject_reason VARCHAR(1024) NOT NULL DEFAULT '',
    requested_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    approved_at DATETIME(3) NULL,
    paid_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_fin_withdrawal_no (withdrawal_no),
    KEY idx_fin_withdrawal_beneficiary (beneficiary_type, beneficiary_id, status),
    KEY idx_fin_withdrawal_status (status, requested_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
