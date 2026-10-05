#!/usr/bin/env bash
# Explicit owner request: enable capability only; NEVER select dynamic/start room.
set -euo pipefail
[[ "${1:-}" == "grant-only-room-15-tenant-14" ]] || exit 2
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
MYSQL=(mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --default-character-set=utf8mb4)
[[ "$("${MYSQL[@]}" --skip-column-names -e "SELECT COUNT(*) FROM core_rooms WHERE id=15 AND tenant_id=14 AND name='菜籽油'")" == 1 ]] || { echo 'Room identity changed; refusing grant'; exit 3; }
"${MYSQL[@]}" <<'SQL'
CREATE TABLE IF NOT EXISTS live_content_policies (
 tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 0, room_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
 policy_json TEXT NOT NULL, revision BIGINT NOT NULL DEFAULT 1,
 updated_by_user_id BIGINT UNSIGNED NOT NULL, updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY (tenant_id,room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TABLE IF NOT EXISTS live_room_content_access (
 tenant_id BIGINT UNSIGNED NOT NULL, room_id BIGINT UNSIGNED NOT NULL,
 selected_mode VARCHAR(32) NOT NULL DEFAULT 'ai_pregenerated',
 dynamic_authorized TINYINT(1) NOT NULL DEFAULT 0, authorization_source VARCHAR(32) NOT NULL DEFAULT 'default',
 updated_by_user_id BIGINT UNSIGNED NOT NULL, updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
 PRIMARY KEY (tenant_id,room_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
CREATE TEMPORARY TABLE content_grant_guard (ok TINYINT NOT NULL CHECK (ok=1));
START TRANSACTION;
SELECT id,tenant_id,name FROM core_rooms WHERE id=15 AND tenant_id=14 AND name='菜籽油' FOR UPDATE;
INSERT INTO content_grant_guard SELECT IF(COUNT(*)=1,1,0) FROM core_rooms WHERE id=15 AND tenant_id=14 AND name='菜籽油';
-- Confirm the already-observed formal audio remains an original recording.
INSERT INTO content_grant_guard
SELECT IF(COUNT(*)=1,1,0) FROM live_agent_plan_versions v
JOIN media_assets a ON a.id=19 AND a.tenant_id=v.tenant_id
WHERE v.id=27 AND v.tenant_id=14 AND v.room_id=15 AND v.lifecycle_status='published'
AND JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.purpose'))='live_agent_custom_mainline_audio';
SET @before_access=(SELECT JSON_OBJECT('selected_mode',selected_mode,'dynamic_authorized',dynamic_authorized) FROM live_room_content_access WHERE tenant_id=14 AND room_id=15);
SET @before_defaults=(SELECT policy_json FROM live_content_policies WHERE tenant_id=0 AND room_id=0);
INSERT INTO live_room_content_access (tenant_id,room_id,selected_mode,dynamic_authorized,authorization_source,updated_by_user_id)
VALUES (14,15,'user_audio',1,'operator',0)
ON DUPLICATE KEY UPDATE dynamic_authorized=1,authorization_source='operator',updated_by_user_id=0,updated_at=CURRENT_TIMESTAMP(3);
INSERT INTO live_content_policies (tenant_id,room_id,policy_json,revision,updated_by_user_id)
VALUES (0,0,JSON_OBJECT('content_mode','ai_pregenerated','auto_refresh_enabled',CAST('true' AS JSON),'mainline_ttl_seconds',7200,'faq_ttl_seconds',7200,'refresh_ahead_seconds',600,'replacement_percent',25,'faq_variant_count',3,'min_repeat_seconds',1200,'minimum_buffer_seconds',1800),1,0)
ON DUPLICATE KEY UPDATE policy_json=JSON_SET(policy_json,'$.mainline_ttl_seconds',7200,'$.faq_ttl_seconds',7200,'$.replacement_percent',25),revision=revision+1,updated_by_user_id=0,updated_at=CURRENT_TIMESTAMP(3);
INSERT INTO live_support_authorization_events (action,tenant_id,room_id,staff_user_id,actor_user_id,capability,detail_json)
VALUES ('content_entitlement.owner_request',14,15,0,0,'ai_dynamic',JSON_OBJECT('source','explicit_owner_request','reason','只开通高级模式，保留当前模式，不启动直播','before',@before_access,'dynamic_authorized',CAST('true' AS JSON),'preserve_mode',CAST('true' AS JSON))),
('content_policy.owner_defaults',0,0,0,0,'system',JSON_OBJECT('source','explicit_owner_request','before',@before_defaults,'mainline_ttl_seconds',7200,'faq_ttl_seconds',7200,'replacement_percent',25));
COMMIT;
SELECT r.id,r.tenant_id,r.name,r.status,r.monitor_enabled,a.selected_mode,a.dynamic_authorized FROM core_rooms r JOIN live_room_content_access a ON a.tenant_id=r.tenant_id AND a.room_id=r.id WHERE r.id=15 AND r.tenant_id=14;
SELECT tenant_id,room_id,JSON_EXTRACT(policy_json,'$.mainline_ttl_seconds') AS mainline_ttl_seconds,JSON_EXTRACT(policy_json,'$.faq_ttl_seconds') AS faq_ttl_seconds,JSON_EXTRACT(policy_json,'$.replacement_percent') AS replacement_percent FROM live_content_policies WHERE tenant_id=0 AND room_id=0;
SQL
