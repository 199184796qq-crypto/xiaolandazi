CREATE TABLE IF NOT EXISTS device_hardware_profiles (
    device_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    hardware_mac VARCHAR(17) NOT NULL,
    claim_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    claimed_tenant_id BIGINT UNSIGNED NULL,
    device_name VARCHAR(64) NOT NULL DEFAULT '',
    claimed_at DATETIME(3) NULL,
    name_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    UNIQUE KEY uk_device_hardware_mac (hardware_mac),
    UNIQUE KEY uk_device_customer_name (claimed_tenant_id, device_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS device_claim_codes (
    device_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    code_hash CHAR(64) NOT NULL,
    encrypted_code VARBINARY(128) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    UNIQUE KEY uk_device_claim_code (code_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS device_name_sequences (
    tenant_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    last_sequence BIGINT UNSIGNED NOT NULL DEFAULT 0
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS device_provisioning_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    device_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NULL,
    actor_user_id BIGINT UNSIGNED NULL,
    event_code VARCHAR(32) NOT NULL,
    detail VARCHAR(255) NOT NULL DEFAULT '',
    occurred_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    KEY idx_device_provisioning_events (device_id, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
