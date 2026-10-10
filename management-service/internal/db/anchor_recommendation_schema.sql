CREATE TABLE IF NOT EXISTS system_anchor_recommendations (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
 source_tenant_id BIGINT UNSIGNED NOT NULL,
 source_style_id BIGINT UNSIGNED NOT NULL,
 name VARCHAR(180) NOT NULL,
 introduction VARCHAR(2000) NOT NULL DEFAULT '',
 summary VARCHAR(200) NOT NULL,
 snapshot_json MEDIUMTEXT NOT NULL,
 source_sha256 CHAR(64) NOT NULL,
 generation_json MEDIUMTEXT NOT NULL,
 version_no BIGINT UNSIGNED NOT NULL DEFAULT 1,
 status VARCHAR(24) NOT NULL DEFAULT 'active',
 published_by_user_id BIGINT UNSIGNED NOT NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (id),
 UNIQUE KEY uk_system_anchor_source (source_tenant_id,source_style_id),
 KEY idx_system_anchor_status (status,updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci
