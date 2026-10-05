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
    execution_realm VARCHAR(96) NOT NULL DEFAULT 'prod',
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
    KEY idx_live_runtime_device_status (device_id, status, started_at),
    KEY idx_live_runtime_execution (execution_realm, status, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_quota_leases (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    runtime_session_id BIGINT UNSIGNED NOT NULL,
    core_boot_id VARCHAR(128) NOT NULL,
    core_working_start_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    core_working_end_seconds BIGINT UNSIGNED NULL,
    allocated_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    consumed_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    status VARCHAR(32) NOT NULL DEFAULT 'reserved',
    granted_at DATETIME(3) NOT NULL,
    expires_at DATETIME(3) NOT NULL,
    settled_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_quota_leases_external (external_id),
    KEY idx_live_quota_leases_tenant_status (tenant_id, status, expires_at),
    KEY idx_live_quota_leases_room_status (room_id, status, expires_at),
    KEY idx_live_quota_leases_session (runtime_session_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_quota_lease_allocations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    lease_id BIGINT UNSIGNED NOT NULL,
    bucket_id BIGINT UNSIGNED NOT NULL,
    reserved_seconds BIGINT UNSIGNED NOT NULL,
    consumed_seconds BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_quota_lease_bucket (lease_id, bucket_id),
    KEY idx_live_quota_lease_alloc_bucket (bucket_id, lease_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS ai_single_use_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_id VARCHAR(96) NOT NULL,
    actor_user_id BIGINT UNSIGNED NOT NULL,
    tenant_id BIGINT UNSIGNED NULL,
    room_id BIGINT UNSIGNED NULL,
    source VARCHAR(64) NOT NULL,
    payer_type VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'started',
    quoted_beans BIGINT UNSIGNED NOT NULL DEFAULT 1,
    charged_beans BIGINT UNSIGNED NOT NULL DEFAULT 0,
    provider VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(128) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    input_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    output_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    total_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    metadata_json JSON NULL,
    started_at DATETIME(3) NOT NULL,
    completed_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_ai_single_use_external (external_id),
    KEY idx_ai_single_use_actor (actor_user_id, started_at),
    KEY idx_ai_single_use_tenant (tenant_id, started_at),
    KEY idx_ai_single_use_source (source, payer_type, status, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS agent_understanding_policies (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    scope_type VARCHAR(32) NOT NULL,
    scope_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    mode VARCHAR(16) NOT NULL DEFAULT 'model',
    provider VARCHAR(64) NOT NULL DEFAULT 'qwen',
    model VARCHAR(128) NOT NULL DEFAULT 'qwen3.8-flash',
    max_context_messages INT NOT NULL DEFAULT 10,
    max_tokens INT NOT NULL DEFAULT 900,
    timeout_ms INT NOT NULL DEFAULT 12000,
    monthly_budget_tokens BIGINT UNSIGNED NOT NULL DEFAULT 0,
    budget_fallback VARCHAR(32) NOT NULL DEFAULT 'program',
    min_confidence DECIMAL(6,5) NOT NULL DEFAULT 0.72000,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    updated_by_user_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_understanding_scope (scope_type, scope_id),
    KEY idx_agent_understanding_mode (mode, enabled),
    KEY idx_agent_understanding_updated (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_room_event_archive (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    event_id BIGINT NOT NULL,
    event_type VARCHAR(64) NOT NULL,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    nickname VARCHAR(255) NOT NULL DEFAULT '',
    content TEXT NULL,
    payload_json MEDIUMTEXT NULL,
    occurred_at DATETIME(3) NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_room_event_archive_event (tenant_id, room_id, event_id),
    KEY idx_live_room_event_archive_room_time (tenant_id, room_id, occurred_at, id),
    KEY idx_live_room_event_archive_type_time (room_id, event_type, occurred_at)
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
CREATE TABLE IF NOT EXISTS live_strategy_center_configs (
    tenant_id BIGINT UNSIGNED NOT NULL,
    config_json JSON NOT NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id),
    KEY idx_live_strategy_center_updated_by (updated_by_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_room_interaction_preferences (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    overall_interaction TINYINT UNSIGNED NOT NULL DEFAULT 50,
    question_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    welcome_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    engagement_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    chat_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    conversion_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    auto_heat TINYINT(1) NOT NULL DEFAULT 1,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_room_interaction_preferences_room (room_id),
    KEY idx_live_room_interaction_preferences_updated_by (updated_by_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
UPDATE live_room_interaction_preferences
SET
    overall_interaction = CASE
        WHEN LOWER(TRIM(CAST(overall_interaction AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(overall_interaction AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(overall_interaction AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(overall_interaction AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(overall_interaction AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END,
    question_preference = CASE
        WHEN LOWER(TRIM(CAST(question_preference AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(question_preference AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(question_preference AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(question_preference AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(question_preference AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END,
    welcome_preference = CASE
        WHEN LOWER(TRIM(CAST(welcome_preference AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(welcome_preference AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(welcome_preference AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(welcome_preference AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(welcome_preference AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END,
    engagement_preference = CASE
        WHEN LOWER(TRIM(CAST(engagement_preference AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(engagement_preference AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(engagement_preference AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(engagement_preference AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(engagement_preference AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END,
    chat_preference = CASE
        WHEN LOWER(TRIM(CAST(chat_preference AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(chat_preference AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(chat_preference AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(chat_preference AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(chat_preference AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END,
    conversion_preference = CASE
        WHEN LOWER(TRIM(CAST(conversion_preference AS CHAR))) IN ('quiet','less','steady') THEN '25'
        WHEN LOWER(TRIM(CAST(conversion_preference AS CHAR))) = 'natural' THEN '50'
        WHEN LOWER(TRIM(CAST(conversion_preference AS CHAR))) IN ('active','more') THEN '75'
        WHEN TRIM(CAST(conversion_preference AS CHAR)) REGEXP '^[0-9]+$' THEN CAST(LEAST(100, CAST(conversion_preference AS UNSIGNED)) AS CHAR)
        ELSE '50'
    END
-- +statement
ALTER TABLE live_room_interaction_preferences
    MODIFY COLUMN overall_interaction TINYINT UNSIGNED NOT NULL DEFAULT 50,
    MODIFY COLUMN question_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    MODIFY COLUMN welcome_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    MODIFY COLUMN engagement_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    MODIFY COLUMN chat_preference TINYINT UNSIGNED NOT NULL DEFAULT 50,
    MODIFY COLUMN conversion_preference TINYINT UNSIGNED NOT NULL DEFAULT 50
-- +statement
CREATE TABLE IF NOT EXISTS live_room_addressing_preferences (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    naming_preference VARCHAR(24) NOT NULL DEFAULT 'natural',
    preferred_terms_json TEXT NOT NULL,
    blocked_terms_json TEXT NOT NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_room_addressing_preferences_room (room_id),
    KEY idx_live_room_addressing_preferences_updated_by (updated_by_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_room_human_behavior_profiles (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    trait_text TEXT NOT NULL,
    state_text TEXT NOT NULL,
    state_expires_at DATETIME(3) NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_room_human_behavior_profiles_room (room_id),
    KEY idx_live_room_human_behavior_profiles_updated_by (updated_by_user_id)
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
CREATE TABLE IF NOT EXISTS voice_model_bindings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    profile_id BIGINT UNSIGNED NOT NULL,
    sample_asset_id BIGINT UNSIGNED NOT NULL,
    provider VARCHAR(64) NOT NULL,
    model VARCHAR(128) NOT NULL,
    voice_id VARCHAR(255) NOT NULL,
    rate DOUBLE NOT NULL DEFAULT 1,
    status VARCHAR(32) NOT NULL DEFAULT 'ready',
    config_json JSON NOT NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_voice_model_bindings_profile (tenant_id, profile_id, status, id),
    KEY idx_voice_model_bindings_model (tenant_id, model, status, id),
    KEY idx_voice_model_bindings_voice (provider, voice_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
INSERT INTO voice_model_bindings (
    tenant_id, profile_id, sample_asset_id, provider, model, voice_id,
    rate, status, config_json, created_by_user_id
)
SELECT
    vp.tenant_id,
    vp.id,
    vp.sample_asset_id,
    CASE
        WHEN LOWER(TRIM(vp.provider)) IN ('aliyun_qwen_clone','aliyun_qwen','qwen','dashscope','qwen_audio','qwen_audio_3_0')
            THEN 'qwen_audio'
        ELSE vp.provider
    END,
    COALESCE(
        NULLIF(JSON_UNQUOTE(JSON_EXTRACT(vp.config_json, '$.target_model')), ''),
        'qwen-audio-3.0-tts-plus'
    ),
    vp.voice_id,
    CASE
        WHEN CAST(COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(vp.config_json, '$.rate')), ''), '1') AS DECIMAL(6,3)) BETWEEN 0.5 AND 2
            THEN CAST(COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(vp.config_json, '$.rate')), ''), '1') AS DECIMAL(6,3))
        ELSE 1
    END,
    'ready',
    JSON_OBJECT('source', 'legacy_voice_profile_backfill'),
    COALESCE(vp.updated_by_user_id, vp.created_by_user_id)
FROM voice_profiles vp
WHERE vp.sample_asset_id IS NOT NULL
  AND TRIM(vp.voice_id) <> ''
  AND LOWER(TRIM(vp.clone_status)) = 'ready'
  AND NOT EXISTS (
      SELECT 1
      FROM voice_model_bindings b
      WHERE b.tenant_id = vp.tenant_id
        AND b.profile_id = vp.id
        AND b.voice_id = vp.voice_id
  )
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
CREATE TABLE IF NOT EXISTS live_support_requests (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    staff_user_id BIGINT UNSIGNED NOT NULL,
    requested_by_user_id BIGINT UNSIGNED NOT NULL,
    capabilities_json JSON NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'pending',
    decided_by_user_id BIGINT UNSIGNED NULL,
    decision_note VARCHAR(512) NOT NULL DEFAULT '',
    requested_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    decided_at DATETIME(3) NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_support_request_staff (staff_user_id, status, requested_at),
    KEY idx_live_support_request_room (tenant_id, room_id, requested_at),
    KEY idx_live_support_request_status (status, requested_at)
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
-- +statement
CREATE TABLE IF NOT EXISTS live_policy_learning_candidates (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    evidence_type VARCHAR(32) NOT NULL DEFAULT 'manual_feedback',
    source_ref VARCHAR(512) NOT NULL DEFAULT '',
    source_layer VARCHAR(8) NOT NULL,
    industry_code VARCHAR(64) NOT NULL DEFAULT '',
    tenant_id BIGINT UNSIGNED NULL,
    room_id BIGINT UNSIGNED NULL,
    question TEXT NOT NULL,
    observed_reply MEDIUMTEXT NOT NULL,
    final_reply MEDIUMTEXT NOT NULL,
    feedback TEXT NOT NULL,
    history_json JSON NOT NULL,
    recommended_layer VARCHAR(8) NOT NULL,
    recommendation_reason TEXT NOT NULL,
    absorb_recommended TINYINT(1) NOT NULL DEFAULT 1,
    confidence INT NOT NULL DEFAULT 0,
    rule_title VARCHAR(200) NOT NULL,
    rule_text MEDIUMTEXT NOT NULL,
    execution_mode VARCHAR(24) NOT NULL DEFAULT 'intent',
    status VARCHAR(24) NOT NULL DEFAULT 'pending',
    model_provider VARCHAR(64) NOT NULL DEFAULT '',
    model_name VARCHAR(96) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    learning_meta_json MEDIUMTEXT NULL,
    adopted_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NOT NULL,
    reviewed_by_user_id BIGINT UNSIGNED NULL,
    review_note VARCHAR(1024) NOT NULL DEFAULT '',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    reviewed_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_live_policy_learning_status (status, id),
    KEY idx_live_policy_learning_layer (recommended_layer, status, id),
    KEY idx_live_policy_learning_room (tenant_id, room_id, status, id),
    KEY idx_live_policy_learning_creator (created_by_user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_learning_sessions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'direct_correction',
    source_ref VARCHAR(512) NOT NULL DEFAULT '',
    question TEXT NOT NULL,
    original_reply MEDIUMTEXT NOT NULL,
    target VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'editing',
    memory_type VARCHAR(24) NOT NULL DEFAULT '',
    adopted_memory_item_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    adopted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    KEY idx_agent_learning_session_room (tenant_id, room_id, status, updated_at),
    KEY idx_agent_learning_session_creator (created_by_user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_learning_evidence (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id BIGINT UNSIGNED NOT NULL,
    turn_no BIGINT UNSIGNED NOT NULL,
    feedback MEDIUMTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_learning_evidence_turn (session_id, turn_no),
    KEY idx_agent_learning_evidence_session (session_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_learning_results (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    session_id BIGINT UNSIGNED NOT NULL,
    evidence_id BIGINT UNSIGNED NOT NULL,
    turn_no BIGINT UNSIGNED NOT NULL,
    memory_type VARCHAR(24) NOT NULL,
    target VARCHAR(255) NOT NULL,
    memory_key VARCHAR(255) NOT NULL,
    matched_memory_item_id BIGINT UNSIGNED NULL,
    result_text MEDIUMTEXT NOT NULL,
    structured_json JSON NOT NULL,
    model_provider VARCHAR(64) NOT NULL DEFAULT '',
    model_name VARCHAR(96) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_learning_result_turn (session_id, turn_no),
    KEY idx_agent_learning_result_session (session_id, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_memory_items (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    memory_type VARCHAR(24) NOT NULL,
    memory_key VARCHAR(255) NOT NULL,
    target VARCHAR(255) NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    current_version_id BIGINT UNSIGNED NULL,
    created_by_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_memory_room_key (tenant_id, room_id, memory_type, memory_key),
    KEY idx_agent_memory_room_type (tenant_id, room_id, memory_type, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_memory_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    memory_item_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    content_text MEDIUMTEXT NOT NULL,
    structured_json JSON NOT NULL,
    source_session_id BIGINT UNSIGNED NOT NULL,
    source_result_id BIGINT UNSIGNED NOT NULL,
    created_by_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_memory_version_no (memory_item_id, version_no),
    KEY idx_agent_memory_version_status (memory_item_id, status, version_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS agent_memory_evidence_stats (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    memory_type VARCHAR(24) NOT NULL,
    memory_key VARCHAR(255) NOT NULL,
    value_signature CHAR(64) NOT NULL,
    value_text MEDIUMTEXT NOT NULL,
    occurrence_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    consecutive_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    explicit_correction_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    adopted_count BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_session_id BIGINT UNSIGNED NULL,
    last_result_id BIGINT UNSIGNED NULL,
    first_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    last_seen_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    last_adopted_at DATETIME(3) NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uk_agent_memory_evidence_value (
        tenant_id, room_id, memory_type, memory_key, value_signature
    ),
    KEY idx_agent_memory_evidence_lookup (
        tenant_id, room_id, memory_type, memory_key, adopted_count, occurrence_count, last_seen_at
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS live_generated_speech_history (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    runtime_session_id BIGINT UNSIGNED NOT NULL,
    runtime_external_id VARCHAR(128) NOT NULL DEFAULT '',
    decision_id VARCHAR(160) NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'interrupt_answer',
    question_text TEXT NOT NULL,
    generated_text MEDIUMTEXT NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_generated_speech_decision (
        tenant_id, room_id, runtime_session_id, decision_id
    ),
    KEY idx_live_generated_speech_session (
        tenant_id, room_id, runtime_session_id, created_at, id
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS semantic_documents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    plan_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
    content_type VARCHAR(48) NOT NULL,
    source_id VARCHAR(160) NOT NULL,
    source_version BIGINT NOT NULL DEFAULT 0,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    content_text MEDIUMTEXT NOT NULL,
    text_hash CHAR(64) NOT NULL,
    embedding_model VARCHAR(128) NOT NULL,
    embedding_dimensions INT UNSIGNED NOT NULL,
    embedding_blob MEDIUMBLOB NOT NULL,
    expires_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_semantic_document_source (
        tenant_id, room_id, plan_id, content_type, source_id, source_version, embedding_model
    ),
    KEY idx_semantic_document_room (
        tenant_id, room_id, content_type, status, updated_at
    ),
    KEY idx_semantic_document_plan (
        tenant_id, plan_id, content_type, status, updated_at
    ),
    KEY idx_semantic_document_expiry (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci

-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plans (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(160) NOT NULL,
    description TEXT NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_agent_plans_tenant (tenant_id, status, updated_at),
    KEY idx_live_agent_plans_name (tenant_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_room_bindings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    active_room_id BIGINT UNSIGNED GENERATED ALWAYS AS (
        CASE WHEN status='active' THEN room_id ELSE NULL END
    ) STORED,
    bound_by_user_id BIGINT UNSIGNED NULL,
    bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    unbound_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_active_room (tenant_id, plan_id, active_room_id),
    KEY idx_live_agent_plan_binding_room (tenant_id, room_id, status),
    KEY idx_live_agent_plan_binding_plan (plan_id, status, room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_room_plan_selections (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    selected_by_user_id BIGINT UNSIGNED NULL,
    selected_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_agent_room_plan_selection_plan (tenant_id, plan_id, room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    lifecycle_status VARCHAR(24) NOT NULL DEFAULT 'draft',
    duration_minutes INT UNSIGNED NOT NULL DEFAULT 0,
    round_minutes INT UNSIGNED NOT NULL DEFAULT 0,
    voice_identity_json JSON NOT NULL,
    variants_json JSON NOT NULL,
    generation_context_json JSON NOT NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_version_no (tenant_id, plan_id, room_id, version_no),
    KEY idx_live_agent_plan_versions_room (tenant_id, room_id, lifecycle_status, version_no),
    KEY idx_live_agent_plan_versions_plan (tenant_id, plan_id, room_id, lifecycle_status, version_no)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_room_plan_publications (
    tenant_id BIGINT UNSIGNED NOT NULL,
    room_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    version_id BIGINT UNSIGNED NOT NULL,
    published_by_user_id BIGINT UNSIGNED NULL,
    published_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, room_id),
    KEY idx_live_agent_room_plan_publications_version (version_id),
    KEY idx_live_agent_room_plan_publications_plan (tenant_id, plan_id, room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS mgmt_user_ui_preferences (
    user_id BIGINT UNSIGNED NOT NULL,
    selected_live_room_id BIGINT UNSIGNED NULL,
    sidebar_collapsed TINYINT(1) NOT NULL DEFAULT 1,
    live_plan_panel_collapsed TINYINT(1) NOT NULL DEFAULT 1,
    agent_drawer_collapsed TINYINT(1) NOT NULL DEFAULT 1,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (user_id),
    KEY idx_user_ui_preferences_room (selected_live_room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_terms (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    canonical_text VARCHAR(255) NOT NULL,
    term_type VARCHAR(32) NOT NULL DEFAULT 'proper_noun',
    note VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_term (plan_id, canonical_text),
    KEY idx_live_agent_plan_terms_plan (tenant_id, plan_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_term_variants (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    term_id BIGINT UNSIGNED NOT NULL,
    variant_text VARCHAR(255) NOT NULL,
    source VARCHAR(32) NOT NULL DEFAULT 'manual_correction',
    confirmation_count INT UNSIGNED NOT NULL DEFAULT 1,
    last_confirmed_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_term_variant (term_id, variant_text),
    KEY idx_live_agent_plan_term_variants_term (term_id, confirmation_count, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_scripts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(180) NOT NULL DEFAULT '',
    source_type VARCHAR(24) NOT NULL DEFAULT 'paste',
    source_asset_id BIGINT UNSIGNED NULL,
    original_name VARCHAR(255) NOT NULL DEFAULT '',
    raw_text MEDIUMTEXT NOT NULL,
    readable_text MEDIUMTEXT NOT NULL,
    analysis_status VARCHAR(24) NOT NULL DEFAULT 'not_analyzed',
    analysis_json MEDIUMTEXT NOT NULL,
    model_provider VARCHAR(64) NOT NULL DEFAULT '',
    model_name VARCHAR(128) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    analyzed_at DATETIME(3) NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_agent_plan_scripts_plan (tenant_id, plan_id, status, updated_at),
    KEY idx_live_agent_plan_scripts_asset (source_asset_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_style_overlays (
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    items_json JSON NOT NULL,
    revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
    updated_by_user_id BIGINT UNSIGNED NOT NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (tenant_id, plan_id),
    KEY idx_live_agent_plan_style_overlays_plan (plan_id),
    KEY idx_live_agent_plan_style_overlays_updated_by (updated_by_user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_facts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    category VARCHAR(48) NOT NULL DEFAULT 'other',
    fact_key VARCHAR(255) NOT NULL,
    fact_value TEXT NOT NULL,
    forbidden_wording TEXT NULL,
    safe_rewrite TEXT NULL,
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT 'adoptable',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT 'analysis_adoption',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    version_no BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_fact_current (plan_id, category, fact_key),
    KEY idx_live_agent_plan_facts_plan (tenant_id, plan_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_fact_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    fact_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    action VARCHAR(32) NOT NULL,
    category VARCHAR(48) NOT NULL,
    fact_key VARCHAR(255) NOT NULL,
    fact_value TEXT NOT NULL,
    forbidden_wording TEXT NULL,
    safe_rewrite TEXT NULL,
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT '',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT '',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_fact_revision (fact_id, version_no),
    KEY idx_live_agent_plan_fact_revisions_plan (tenant_id, plan_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_product_links (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    link_key VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL DEFAULT '',
    spec VARCHAR(255) NOT NULL DEFAULT '',
    daily_price VARCHAR(255) NOT NULL DEFAULT '',
    quantity VARCHAR(255) NOT NULL DEFAULT '',
    audience VARCHAR(512) NOT NULL DEFAULT '',
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT 'adoptable',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT 'analysis_adoption',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    version_no BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_product_link_current (plan_id, link_key),
    KEY idx_live_agent_plan_product_links_plan (tenant_id, plan_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_product_link_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    product_link_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    action VARCHAR(32) NOT NULL,
    link_key VARCHAR(64) NOT NULL,
    product_name VARCHAR(255) NOT NULL DEFAULT '',
    spec VARCHAR(255) NOT NULL DEFAULT '',
    daily_price VARCHAR(255) NOT NULL DEFAULT '',
    quantity VARCHAR(255) NOT NULL DEFAULT '',
    audience VARCHAR(512) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT '',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT '',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_product_link_revision (product_link_id, version_no),
    KEY idx_live_agent_plan_product_link_revisions_plan (tenant_id, plan_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_benefits (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    benefit_key VARCHAR(255) NOT NULL,
    link_key VARCHAR(64) NOT NULL DEFAULT '',
    product_name VARCHAR(255) NOT NULL DEFAULT '',
    activity_price VARCHAR(255) NOT NULL DEFAULT '',
    gift VARCHAR(512) NOT NULL DEFAULT '',
    activity_text TEXT NOT NULL,
    starts_at DATETIME(3) NULL,
    ends_at DATETIME(3) NULL,
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT 'adoptable',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT 'analysis_adoption',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'draft',
    version_no BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_benefit_current (plan_id, benefit_key),
    KEY idx_live_agent_plan_benefits_plan (tenant_id, plan_id, status, updated_at),
    KEY idx_live_agent_plan_benefits_window (tenant_id, plan_id, starts_at, ends_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_benefit_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    benefit_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    action VARCHAR(32) NOT NULL,
    benefit_key VARCHAR(255) NOT NULL,
    link_key VARCHAR(64) NOT NULL DEFAULT '',
    product_name VARCHAR(255) NOT NULL DEFAULT '',
    activity_price VARCHAR(255) NOT NULL DEFAULT '',
    gift VARCHAR(512) NOT NULL DEFAULT '',
    activity_text TEXT NOT NULL,
    starts_at DATETIME(3) NULL,
    ends_at DATETIME(3) NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'draft',
    source_quote TEXT NOT NULL,
    source_review_bucket VARCHAR(24) NOT NULL DEFAULT '',
    source_review_reason VARCHAR(512) NOT NULL DEFAULT '',
    source_type VARCHAR(48) NOT NULL DEFAULT '',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_benefit_revision (benefit_id, version_no),
    KEY idx_live_agent_plan_benefit_revisions_plan (tenant_id, plan_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_script_references (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    reference_key VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    content_text MEDIUMTEXT NOT NULL,
    goal VARCHAR(512) NOT NULL DEFAULT '',
    transition_text VARCHAR(512) NOT NULL DEFAULT '',
    execution_mode VARCHAR(24) NOT NULL DEFAULT 'intent',
    source_quote TEXT NOT NULL,
    source_type VARCHAR(48) NOT NULL DEFAULT 'system_agent',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    version_no BIGINT UNSIGNED NOT NULL DEFAULT 1,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_script_reference_current (plan_id, reference_key),
    KEY idx_live_agent_plan_script_references_plan (tenant_id, plan_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_agent_plan_script_reference_revisions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    reference_id BIGINT UNSIGNED NOT NULL,
    version_no BIGINT UNSIGNED NOT NULL,
    action VARCHAR(32) NOT NULL,
    reference_key VARCHAR(255) NOT NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    content_text MEDIUMTEXT NOT NULL,
    goal VARCHAR(512) NOT NULL DEFAULT '',
    transition_text VARCHAR(512) NOT NULL DEFAULT '',
    execution_mode VARCHAR(24) NOT NULL DEFAULT 'intent',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    source_quote TEXT NOT NULL,
    source_type VARCHAR(48) NOT NULL DEFAULT '',
    source_ref VARCHAR(255) NOT NULL DEFAULT '',
    actor_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_agent_plan_script_reference_revision (reference_id, version_no),
    KEY idx_live_agent_plan_script_reference_revisions_plan (tenant_id, plan_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_anchor_styles (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(180) NOT NULL,
    description VARCHAR(2000) NOT NULL DEFAULT '',
    status VARCHAR(24) NOT NULL DEFAULT 'active',
    profile_json MEDIUMTEXT NOT NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_anchor_styles_tenant (tenant_id, status, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_anchor_style_samples (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    style_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(255) NOT NULL,
    source_type VARCHAR(32) NOT NULL DEFAULT 'paste',
    original_name VARCHAR(255) NOT NULL DEFAULT '',
    raw_text MEDIUMTEXT NOT NULL,
    readable_text MEDIUMTEXT NOT NULL,
    analysis_status VARCHAR(32) NOT NULL DEFAULT 'not_analyzed',
    analysis_json MEDIUMTEXT NOT NULL,
    provider VARCHAR(64) NOT NULL DEFAULT '',
    model VARCHAR(128) NOT NULL DEFAULT '',
    latency_ms BIGINT NOT NULL DEFAULT 0,
    progress INT NOT NULL DEFAULT 0,
    stage VARCHAR(255) NOT NULL DEFAULT '',
    error_message VARCHAR(1000) NOT NULL DEFAULT '',
    analyzed_at DATETIME(3) NULL,
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_anchor_style_samples_style (tenant_id, style_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_anchor_style_trainings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    style_id BIGINT UNSIGNED NOT NULL,
    request_text VARCHAR(2000) NOT NULL,
    target_chars INT NOT NULL DEFAULT 500,
    heat INT NOT NULL DEFAULT 50,
    expansion_freedom INT NOT NULL DEFAULT 50,
    selected_facts_json MEDIUMTEXT NOT NULL,
    generated_text MEDIUMTEXT NOT NULL,
    score_json MEDIUMTEXT NOT NULL,
    status VARCHAR(24) NOT NULL DEFAULT 'draft',
    created_by_user_id BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_live_anchor_style_trainings_style (tenant_id, style_id, created_at, id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_anchor_style_plugins (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    style_id BIGINT UNSIGNED NOT NULL,
    plugin_id VARCHAR(160) NOT NULL,
    plugin_version VARCHAR(64) NOT NULL,
    enabled TINYINT(1) NOT NULL DEFAULT 1,
    parameters_json MEDIUMTEXT NOT NULL,
    updated_by_user_id BIGINT UNSIGNED NULL,
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_anchor_style_plugin (style_id, plugin_id, plugin_version),
    KEY idx_live_anchor_style_plugins_style (tenant_id, style_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
-- +statement
CREATE TABLE IF NOT EXISTS live_anchor_style_plan_bindings (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id BIGINT UNSIGNED NOT NULL,
    style_id BIGINT UNSIGNED NOT NULL,
    plan_id BIGINT UNSIGNED NOT NULL,
    bound_by_user_id BIGINT UNSIGNED NULL,
    bound_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    UNIQUE KEY uk_live_anchor_style_plan (tenant_id, plan_id),
    KEY idx_live_anchor_style_plan_style (tenant_id, style_id, plan_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
