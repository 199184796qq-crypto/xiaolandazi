#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
readlink -f /opt/xiaolan/current
MYSQL_PWD="$DB_PASSWORD" mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --default-character-set=utf8mb4 <<'SQL'
SELECT id,tenant_id,name,status FROM live_agent_plans WHERE tenant_id=14 ORDER BY id;
SELECT plan_id,room_id FROM live_agent_plan_room_bindings WHERE tenant_id=14 AND room_id=15;
SELECT id,plan_id,title,analysis_status,CHAR_LENGTH(readable_text) AS source_chars,JSON_LENGTH(JSON_EXTRACT(analysis_json,'$.anchor_style.dimensions')) AS style_dimensions,JSON_LENGTH(JSON_EXTRACT(analysis_json,'$.anchor_style.reusable_rules')) AS style_rules,analyzed_at,updated_at FROM live_agent_plan_scripts WHERE tenant_id=14 AND status='active' ORDER BY plan_id,updated_at DESC;
SELECT id,plan_id,analysis_status,status FROM live_agent_plan_scripts WHERE tenant_id=14 ORDER BY id;
SELECT id,plan_id,lifecycle_status,JSON_LENGTH(JSON_EXTRACT(generation_context_json,'$.anchor_style.dimensions')) AS snapshot_style_dimensions FROM live_agent_plan_versions WHERE tenant_id=14 AND room_id=15 ORDER BY id DESC LIMIT 5;
SQL
