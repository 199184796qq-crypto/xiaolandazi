CREATE TABLE IF NOT EXISTS device_addressing_preferences (
    device_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    mode VARCHAR(16) NOT NULL DEFAULT 'auto',
    updated_by_user_id BIGINT UNSIGNED NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY(device_id,tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS device_voice_addressing (
    event_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    device_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    addressing VARCHAR(16) NOT NULL DEFAULT 'neutral',
    source VARCHAR(16) NOT NULL DEFAULT 'neutral',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    KEY idx_device_voice_addressing_tenant(tenant_id,device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
