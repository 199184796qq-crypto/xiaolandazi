#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --default-character-set=utf8mb4 <<'SQL'
SELECT r.id,r.tenant_id,r.name,r.status,r.monitor_enabled,a.selected_mode,a.dynamic_authorized,a.authorization_source FROM core_rooms r LEFT JOIN live_room_content_access a ON a.room_id=r.id AND a.tenant_id=r.tenant_id WHERE r.id=15;
SELECT tenant_id,room_id,JSON_EXTRACT(policy_json,'$.mainline_ttl_seconds') AS mainline_ttl,JSON_EXTRACT(policy_json,'$.faq_ttl_seconds') AS faq_ttl,JSON_EXTRACT(policy_json,'$.replacement_percent') AS replacement_percent FROM live_content_policies WHERE tenant_id=0 AND room_id=0;
SELECT COUNT(*) AS active_sessions FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming');
SQL
