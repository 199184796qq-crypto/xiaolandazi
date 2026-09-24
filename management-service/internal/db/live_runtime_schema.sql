CREATE TABLE IF NOT EXISTS live_device_room_bindings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    device_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    binding_role VARCHAR(32) NOT NULL DEFAULT 'primary',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    unbound_at DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    ended_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_device_binding_tenant (tenant_id, status, room_id),
    KEY idx_live_device_binding_device (device_id, status, bound_at),
    KEY idx_live_device_binding_room (room_id, status, binding_role)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_device_runtime_state (
    device_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    current_room_id BIGINT UNSIGNED NULL,
    connection_status VARCHAR(32) NOT NULL DEFAULT 'offline',
    work_status VARCHAR(32) NOT NULL DEFAULT 'idle',
    stop_reason VARCHAR(64) NOT NULL DEFAULT '',
    last_heartbeat_at DATETIME(3) NULL,
    metadata_json JSON NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (device_id),
    KEY idx_live_device_runtime_tenant (tenant_id, connection_status, work_status),
    KEY idx_live_device_runtime_room (current_room_id, work_status),
    KEY idx_live_device_runtime_heartbeat (last_heartbeat_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_runtime_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    device_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'running',
    stop_reason VARCHAR(64) NOT NULL DEFAULT '',
    started_by_user_id BIGINT UNSIGNED NULL,
    stopped_by_user_id BIGINT UNSIGNED NULL,
    started_at DATETIME(3) NOT NULL,
    last_billed_at DATETIME(3) NOT NULL,
    ended_at DATETIME(3) NULL,
    total_billed_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_runtime_external (external_id),
    KEY idx_live_runtime_tenant_status (tenant_id, status, started_at),
    KEY idx_live_runtime_room_status (room_id, status, started_at),
    KEY idx_live_runtime_device_status (device_id, status, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_runtime_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NULL,
    device_id BIGINT UNSIGNED NULL,
    session_id BIGINT UNSIGNED NULL,
    actor_type VARCHAR(32) NOT NULL DEFAULT 'system',
    actor_user_id BIGINT UNSIGNED NULL,
    event_code VARCHAR(64) NOT NULL,
    title VARCHAR(160) NOT NULL,
    detail_json JSON NULL,
    occurred_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_runtime_events_tenant (tenant_id, occurred_at),
    KEY idx_live_runtime_events_room (room_id, occurred_at),
    KEY idx_live_runtime_events_device (device_id, occurred_at),
    KEY idx_live_runtime_events_session (session_id, occurred_at),
    KEY idx_live_runtime_events_code (event_code, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_settings (
    tenant_id BIGINT UNSIGNED NOT NULL,
    display_name VARCHAR(128) NOT NULL DEFAULT '小伴直播教练',
    role_name VARCHAR(160) NOT NULL DEFAULT '直播策略与场控 Agent',
    self_introduction TEXT NOT NULL,
    mission TEXT NOT NULL,
    greeting TEXT NOT NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id),
    KEY idx_live_agent_settings_updated_by (updated_by_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(128) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    current_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_profiles_tenant (tenant_id),
    KEY idx_live_agent_profiles_status (status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_config_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    agent_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    layer1_json JSON NOT NULL,
    layer2_json JSON NOT NULL,
    layer3_json JSON NOT NULL,
    persona_json JSON NOT NULL,
    model_config_json JSON NOT NULL,
    speech_config_json JSON NOT NULL,
    style_profile_json JSON NOT NULL,
    safety_config_json JSON NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_config_version (agent_id, version_no),
    KEY idx_live_agent_config_status (agent_id, lifecycle_status, version_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_strategies (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    agent_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(160) NOT NULL,
    execution_mode VARCHAR(32) NOT NULL DEFAULT 'intent',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    current_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_agent_strategy_tenant (tenant_id, status, updated_at),
    KEY idx_live_agent_strategy_agent (agent_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_strategy_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    strategy_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    source_text MEDIUMTEXT NOT NULL,
    intent_json JSON NOT NULL,
    constraints_json JSON NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_strategy_version (strategy_id, version_no),
    KEY idx_live_agent_strategy_version_status (strategy_id, lifecycle_status, version_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_room_bindings (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    agent_id BIGINT UNSIGNED NOT NULL,
    config_version_id BIGINT UNSIGNED NULL,
    strategy_version_id BIGINT UNSIGNED NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    activated_by_user_id BIGINT UNSIGNED NULL,
    activated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_agent_room_agent (agent_id, status),
    KEY idx_live_agent_room_config (config_version_id),
    KEY idx_live_agent_room_strategy (strategy_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS media_assets (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    agent_id BIGINT UNSIGNED NULL,
    asset_type VARCHAR(48) NOT NULL,
    original_name VARCHAR(255) NOT NULL,
    storage_driver VARCHAR(16) NOT NULL,
    storage_bucket VARCHAR(128) NOT NULL DEFAULT '',
    object_key VARCHAR(768) NOT NULL,
    mime_type VARCHAR(160) NOT NULL DEFAULT 'application/octet-stream',
    size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0,
    duration_ms BIGINT UNSIGNED NULL,
    checksum_sha256 CHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    metadata_json JSON NOT NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_media_assets_tenant (tenant_id, asset_type, status, created_at),
    KEY idx_media_assets_agent (agent_id, asset_type, status),
    KEY idx_media_assets_object (storage_driver, storage_bucket, object_key(191))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS voice_profiles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    agent_id BIGINT UNSIGNED NULL,
    name VARCHAR(128) NOT NULL,
    provider VARCHAR(64) NOT NULL,
    voice_id VARCHAR(255) NOT NULL DEFAULT '',
    sample_asset_id BIGINT UNSIGNED NULL,
    clone_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    config_json JSON NOT NULL,
    is_default TINYINT(1) NOT NULL DEFAULT 0,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_voice_profiles_tenant (tenant_id, is_default, clone_status),
    KEY idx_voice_profiles_agent (agent_id, clone_status),
    KEY idx_voice_profiles_voice (provider, voice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT INTO live_agent_profiles (
    tenant_id, name, status, created_by_user_id, updated_by_user_id
)
SELECT tenant_id, display_name, 'active', updated_by_user_id, updated_by_user_id
FROM live_agent_settings
ON DUPLICATE KEY UPDATE
    name=VALUES(name),
    updated_by_user_id=VALUES(updated_by_user_id)
-- +statement
INSERT INTO live_agent_config_versions (
    agent_id, version_no,
    layer1_json, layer2_json, layer3_json,
    persona_json, model_config_json, speech_config_json,
    style_profile_json, safety_config_json,
    lifecycle_status, created_by_user_id, published_by_user_id, published_at
)
SELECT
    p.id,
    1,
    JSON_OBJECT(),
    JSON_OBJECT(),
    JSON_OBJECT(),
    JSON_OBJECT(
        'display_name', s.display_name,
        'role_name', s.role_name,
        'self_introduction', s.self_introduction,
        'mission', s.mission,
        'greeting', s.greeting
    ),
    JSON_OBJECT(),
    JSON_OBJECT(),
    JSON_OBJECT(),
    JSON_OBJECT(),
    'active',
    s.updated_by_user_id,
    s.updated_by_user_id,
    s.updated_at
FROM live_agent_settings s
INNER JOIN live_agent_profiles p ON p.tenant_id=s.tenant_id
LEFT JOIN live_agent_config_versions v ON v.agent_id=p.id
WHERE v.id IS NULL
-- +statement
UPDATE live_agent_profiles p
INNER JOIN (
    SELECT agent_id, MAX(id) AS version_id
    FROM live_agent_config_versions
    WHERE lifecycle_status='active'
    GROUP BY agent_id
) v ON v.agent_id=p.id
SET p.current_version_id=v.version_id
WHERE p.current_version_id IS NULL

-- +statement
CREATE TABLE IF NOT EXISTS live_policy_industries (
    code VARCHAR(64) NOT NULL,
    name VARCHAR(128) NOT NULL,
    parent_code VARCHAR(64) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    sort_order INT NOT NULL DEFAULT 0,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (code),
    KEY idx_live_policy_industries_status (status, sort_order, name),
    KEY idx_live_policy_industries_parent (parent_code, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT INTO live_policy_industries (
    code, name, parent_code, status, sort_order
) VALUES
    ('general', '通用', '', 'active', 0),
    ('food', '食品餐饮', '', 'active', 10),
    ('agriculture', '农产品生鲜', '', 'active', 20),
    ('apparel', '服装鞋包', '', 'active', 30),
    ('beauty', '美妆个护', '', 'active', 40),
    ('local_life', '本地生活', '', 'active', 50)
ON DUPLICATE KEY UPDATE
    name=VALUES(name),
    sort_order=VALUES(sort_order)
-- +statement
CREATE TABLE IF NOT EXISTS live_policy_tenant_industries (
    tenant_id BIGINT UNSIGNED NOT NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT 'general',
    bound_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id),
    KEY idx_live_policy_tenant_industry (industry_code, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_policy_scopes (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    layer VARCHAR(8) NOT NULL,
    scope_key VARCHAR(255) NOT NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT '',
    tenant_id BIGINT UNSIGNED NULL,
    room_id BIGINT UNSIGNED NULL,
    name VARCHAR(160) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    current_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_policy_scope_key (scope_key),
    KEY idx_live_policy_scope_layer (layer, status, updated_at),
    KEY idx_live_policy_scope_industry (industry_code, layer, status),
    KEY idx_live_policy_scope_room (tenant_id, room_id, layer, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_policy_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    policy_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(32) NOT NULL DEFAULT 'draft',
    source_text MEDIUMTEXT NOT NULL,
    rules_json JSON NOT NULL,
    overrides_json JSON NOT NULL,
    conflicts_json JSON NOT NULL,
    note VARCHAR(1024) NOT NULL DEFAULT '',
    source_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    published_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_policy_version (policy_id, version_no),
    KEY idx_live_policy_version_status (policy_id, lifecycle_status, version_no),
    KEY idx_live_policy_version_source (source_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_policy_audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    actor_user_id BIGINT UNSIGNED NULL,
    action VARCHAR(64) NOT NULL,
    layer VARCHAR(8) NOT NULL,
    policy_id BIGINT UNSIGNED NULL,
    version_id BIGINT UNSIGNED NULL,
    tenant_id BIGINT UNSIGNED NULL,
    room_id BIGINT UNSIGNED NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT '',
    detail_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_policy_audit_actor (actor_user_id, created_at),
    KEY idx_live_policy_audit_policy (policy_id, created_at),
    KEY idx_live_policy_audit_room (tenant_id, room_id, created_at),
    KEY idx_live_policy_audit_layer (layer, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_support_authorizations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    staff_user_id BIGINT UNSIGNED NOT NULL,
    capability VARCHAR(48) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    granted_by_user_id BIGINT UNSIGNED NOT NULL,
    granted_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    revoked_by_user_id BIGINT UNSIGNED NULL,
    revoked_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_support_authorization (tenant_id, room_id, staff_user_id, capability),
    KEY idx_live_support_staff (staff_user_id, status, room_id),
    KEY idx_live_support_room (tenant_id, room_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_support_config_versions (
    version_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    staff_user_id BIGINT UNSIGNED NOT NULL,
    capability VARCHAR(48) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (version_id),
    KEY idx_live_support_config_staff (staff_user_id, room_id, created_at),
    KEY idx_live_support_config_room (tenant_id, room_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_support_authorization_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    action VARCHAR(64) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    staff_user_id BIGINT UNSIGNED NOT NULL,
    actor_user_id BIGINT UNSIGNED NOT NULL,
    capability VARCHAR(48) NOT NULL,
    detail_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_support_event_room (tenant_id, room_id, created_at),
    KEY idx_live_support_event_staff (staff_user_id, created_at),
    KEY idx_live_support_event_actor (actor_user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_runtime_policy_snapshots (
    session_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT 'general',
    l1_version_id BIGINT UNSIGNED NULL,
    l2_version_id BIGINT UNSIGNED NULL,
    l3_version_id BIGINT UNSIGNED NULL,
    effective_json JSON NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (session_id),
    KEY idx_live_runtime_policy_room (tenant_id, room_id, created_at),
    KEY idx_live_runtime_policy_versions (l1_version_id, l2_version_id, l3_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS live_runtime_policy_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT 'general',
    l1_version_id BIGINT UNSIGNED NULL,
    l2_version_id BIGINT UNSIGNED NULL,
    l3_version_id BIGINT UNSIGNED NULL,
    revision_key VARCHAR(160) NOT NULL,
    effective_json JSON NOT NULL,
    observed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_runtime_policy_revision (session_id, revision_key),
    KEY idx_live_runtime_policy_revision_room (tenant_id, room_id, observed_at),
    KEY idx_live_runtime_policy_revision_session (session_id, observed_at),
    KEY idx_live_runtime_policy_revision_versions (l1_version_id, l2_version_id, l3_version_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
