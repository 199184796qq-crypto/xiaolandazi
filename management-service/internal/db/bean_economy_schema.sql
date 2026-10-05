CREATE TABLE IF NOT EXISTS bean_wallet_accounts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    owner_type VARCHAR(24) NOT NULL,
    owner_id BIGINT UNSIGNED NOT NULL,
    available_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    frozen_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_wallet_owner (owner_type, owner_id),
    KEY idx_bean_wallet_status (owner_type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS bean_wallet_ledger (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(128) NOT NULL,
    wallet_id BIGINT UNSIGNED NOT NULL,
    available_delta BIGINT NOT NULL DEFAULT 0,
    frozen_delta BIGINT NOT NULL DEFAULT 0,
    available_before BIGINT UNSIGNED NOT NULL,
    available_after BIGINT UNSIGNED NOT NULL,
    frozen_before BIGINT UNSIGNED NOT NULL,
    frozen_after BIGINT UNSIGNED NOT NULL,
    business_type VARCHAR(64) NOT NULL,
    reference_type VARCHAR(64) NOT NULL DEFAULT '',
    reference_id BIGINT UNSIGNED NULL,
    operator_user_id BIGINT UNSIGNED NULL,
    reason VARCHAR(512) NOT NULL DEFAULT '',
    idempotency_key VARCHAR(160) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_ledger_external (external_id),
    UNIQUE KEY uk_bean_ledger_idempotency (idempotency_key),
    KEY idx_bean_ledger_wallet (wallet_id, created_at),
    KEY idx_bean_ledger_reference (reference_type, reference_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS bean_commerce_settings (
    id TINYINT UNSIGNED NOT NULL DEFAULT 1,
    purchase_beans_per_yuan BIGINT UNSIGNED NOT NULL DEFAULT 100,
    minimum_purchase_cents BIGINT UNSIGNED NOT NULL DEFAULT 100,
    staff_cash_fen_per_100_beans BIGINT UNSIGNED NOT NULL DEFAULT 50,
    minimum_staff_conversion_beans BIGINT UNSIGNED NOT NULL DEFAULT 1000,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT IGNORE INTO bean_commerce_settings (id) VALUES (1)
-- +statement
CREATE TABLE IF NOT EXISTS bean_pricing_rules (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    action_code VARCHAR(96) NOT NULL,
    action_name VARCHAR(160) NOT NULL,
    category VARCHAR(48) NOT NULL DEFAULT 'ai',
    description VARCHAR(512) NOT NULL DEFAULT '',
    charge_mode VARCHAR(24) NOT NULL DEFAULT 'fixed',
    beans_per_unit BIGINT UNSIGNED NOT NULL DEFAULT 1,
    unit_size BIGINT UNSIGNED NOT NULL DEFAULT 1,
    minimum_charge_beans BIGINT UNSIGNED NOT NULL DEFAULT 1,
    maximum_charge_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    staff_reward_bps INT UNSIGNED NOT NULL DEFAULT 0,
    enabled TINYINT(1) NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_pricing_action (action_code),
    KEY idx_bean_pricing_category (category, enabled, action_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT IGNORE INTO bean_pricing_rules
    (action_code, action_name, category, description, charge_mode, beans_per_unit, unit_size, minimum_charge_beans, staff_reward_bps, enabled)
VALUES
    ('audio.transcription', '录音转文字', 'ai', '按音频分钟数计费，实际接入前保持关闭', 'per_unit', 2, 60, 2, 0, 0),
    ('agent.single_chat', '非直播单次模型调用', 'ai', '按调用次数计费，直播中的 AI 仍使用时长卡', 'fixed', 1, 1, 1, 0, 0),
    ('content.analysis', '内容分析', 'ai', '按分析任务计费', 'fixed', 3, 1, 3, 0, 0),
    ('support.ops.standard', '标准运维协助', 'service', '客户确认完成后结算给处理人员', 'fixed', 100, 1, 100, 7500, 0)
-- +statement
CREATE TABLE IF NOT EXISTS bean_purchase_orders (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    purchase_no VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    cash_amount_cents BIGINT UNSIGNED NOT NULL,
    beans_per_yuan_snapshot BIGINT UNSIGNED NOT NULL,
    credited_beans BIGINT UNSIGNED NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'paid',
    cash_ledger_id BIGINT UNSIGNED NULL,
    bean_ledger_id BIGINT UNSIGNED NULL,
    operator_user_id BIGINT UNSIGNED NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_purchase_no (purchase_no),
    UNIQUE KEY uk_bean_purchase_idempotency (tenant_id, idempotency_key),
    KEY idx_bean_purchase_tenant (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS bean_charges (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(128) NOT NULL,
    idempotency_key VARCHAR(160) NOT NULL,
    action_code VARCHAR(96) NOT NULL,
    payer_owner_type VARCHAR(24) NOT NULL,
    payer_owner_id BIGINT UNSIGNED NOT NULL,
    payer_wallet_id BIGINT UNSIGNED NOT NULL,
    beneficiary_user_id BIGINT UNSIGNED NULL,
    rule_id BIGINT UNSIGNED NOT NULL,
    rule_version BIGINT UNSIGNED NOT NULL,
    charge_mode VARCHAR(24) NOT NULL,
    beans_per_unit BIGINT UNSIGNED NOT NULL,
    unit_size BIGINT UNSIGNED NOT NULL,
    minimum_charge_beans BIGINT UNSIGNED NOT NULL,
    maximum_charge_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    staff_reward_bps INT UNSIGNED NOT NULL DEFAULT 0,
    requested_units BIGINT UNSIGNED NOT NULL DEFAULT 1,
    actual_units BIGINT UNSIGNED NOT NULL DEFAULT 0,
    quoted_beans BIGINT UNSIGNED NOT NULL,
    reserved_beans BIGINT UNSIGNED NOT NULL,
    charged_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    staff_reward_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    platform_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'reserved',
    reference_type VARCHAR(64) NOT NULL DEFAULT '',
    reference_id BIGINT UNSIGNED NULL,
    metadata_json JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    settled_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_charge_external (external_id),
    UNIQUE KEY uk_bean_charge_idempotency (idempotency_key),
    KEY idx_bean_charge_payer (payer_owner_type, payer_owner_id, created_at),
    KEY idx_bean_charge_action (action_code, status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS bean_conversion_requests (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    conversion_no VARCHAR(64) NOT NULL,
    user_id BIGINT UNSIGNED NOT NULL,
    wallet_id BIGINT UNSIGNED NOT NULL,
    bean_amount BIGINT UNSIGNED NOT NULL,
    cash_amount_cents BIGINT UNSIGNED NOT NULL,
    cash_fen_per_100_beans_snapshot BIGINT UNSIGNED NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'reviewing',
    requested_by_user_id BIGINT UNSIGNED NOT NULL,
    approved_by_user_id BIGINT UNSIGNED NULL,
    paid_by_user_id BIGINT UNSIGNED NULL,
    reject_reason VARCHAR(512) NOT NULL DEFAULT '',
    requested_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    approved_at DATETIME(3) NULL,
    paid_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_bean_conversion_no (conversion_no),
    KEY idx_bean_conversion_user (user_id, status, requested_at),
    KEY idx_bean_conversion_status (status, requested_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
