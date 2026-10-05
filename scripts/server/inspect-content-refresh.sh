#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --default-character-set=utf8mb4 <<'SQL'
SELECT id,tenant_id,name,status,monitor_enabled FROM core_rooms WHERE name LIKE '%菜籽油%';
SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('live_content_policies','live_room_content_access','live_content_refresh_jobs','mgmt_room_deletions');
SELECT s.id,s.tenant_id,s.room_id,s.status,s.started_at FROM live_runtime_sessions s JOIN core_rooms r ON r.id=s.room_id AND r.tenant_id=s.tenant_id WHERE r.name LIKE '%菜籽油%' ORDER BY s.id DESC LIMIT 5;
SELECT v.id,v.tenant_id,v.room_id,v.plan_id,v.version_no,v.lifecycle_status,v.published_at,JSON_LENGTH(v.variants_json) AS variants FROM live_agent_plan_versions v JOIN core_rooms r ON r.id=v.room_id AND r.tenant_id=v.tenant_id WHERE r.name LIKE '%菜籽油%' ORDER BY v.id DESC LIMIT 5;
SELECT a.tenant_id,a.room_id,a.staff_user_id,a.capability,a.status FROM live_support_authorizations a JOIN core_rooms r ON r.id=a.room_id AND r.tenant_id=a.tenant_id WHERE r.name LIKE '%菜籽油%';
SELECT v.id,j.variant_key,j.is_formal,j.audio_asset_id,JSON_UNQUOTE(JSON_EXTRACT(a.metadata_json,'$.purpose')) AS purpose FROM live_agent_plan_versions v JOIN JSON_TABLE(v.variants_json,'$[*]' COLUMNS (variant_key VARCHAR(64) PATH '$.variant_key',is_formal INT PATH '$.is_formal',audio_asset_id BIGINT PATH '$.audio_asset_id')) j LEFT JOIN media_assets a ON a.id=j.audio_asset_id AND a.tenant_id=v.tenant_id WHERE v.id=27 AND v.tenant_id=14 AND v.room_id=15;
SELECT status,COUNT(*) AS sessions FROM live_runtime_sessions WHERE status IN ('running','paused') GROUP BY status;
SQL
