CREATE TABLE IF NOT EXISTS mkt_campaign_controls (
    campaign_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    controls_json JSON NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS mkt_campaign_item_options (
    campaign_item_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    fixed_price_cents BIGINT UNSIGNED NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS mkt_claim_locks (
    benefit_key VARCHAR(128) NOT NULL,
    subject_hash CHAR(64) NOT NULL,
    PRIMARY KEY (benefit_key, subject_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS mkt_claim_reservations (
    order_id BIGINT UNSIGNED NOT NULL,
    benefit_key VARCHAR(128) NOT NULL,
    subject_hash CHAR(64) NOT NULL,
    quantity INT UNSIGNED NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (order_id, subject_hash),
    KEY idx_mkt_claim_limit (benefit_key, subject_hash, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_commerce_rule_heads (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    channel VARCHAR(16) NOT NULL,
    scope_type VARCHAR(16) NOT NULL,
    product_type VARCHAR(32) NOT NULL DEFAULT '',
    target_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    published_version_id BIGINT UNSIGNED NULL,
    UNIQUE KEY uk_commerce_rule_scope(channel,scope_type,product_type,target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_commerce_rule_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    head_id BIGINT UNSIGNED NOT NULL,
    config_json JSON NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    created_by BIGINT UNSIGNED NOT NULL,
    published_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3) NULL,
    KEY idx_commerce_rule_version(head_id,id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS fin_order_reward_snapshots (
    order_id BIGINT UNSIGNED NOT NULL,
    order_item_id BIGINT UNSIGNED NOT NULL,
    channel VARCHAR(16) NOT NULL,
    rule_version_id BIGINT UNSIGNED NULL,
    config_json JSON NOT NULL,
    PRIMARY KEY(order_item_id,channel),
    KEY idx_order_reward_snapshots(order_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
