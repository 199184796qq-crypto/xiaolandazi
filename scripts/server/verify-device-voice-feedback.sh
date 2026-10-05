#!/usr/bin/env bash
set -euo pipefail
RELEASE_ID="${1:?release required}"
CORE_PID="${2:?original Core PID required}"
CORE_SHA="${3:?original Core SHA required}"
[[ "$(id -u)" == 0 && "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$CORE_PID" =~ ^[0-9]+$ && "$CORE_SHA" =~ ^[a-f0-9]{64}$ ]] || exit 2
[[ "$(readlink -f /opt/xiaolan/current)" == "/opt/xiaolan/releases/$RELEASE_ID" ]]
[[ "$(systemctl show -p MainPID --value xiaolan-core.service)" == "$CORE_PID" ]]
[[ "$(sha256sum /opt/xiaolan/current/bin/core-service | cut -d' ' -f1)" == "$CORE_SHA" ]]
[[ -s "/opt/xiaolan/deployments/$RELEASE_ID-backup/database-before.sql.gz" ]]
[[ "$(stat -c %a /etc/xiaolan/management.env)" == 600 ]]
set -a
source /etc/xiaolan/management.env
set +a
[[ "${DEVICE_ADDRESSING_MODEL:-}" == qwen3.8-omni-flash && -n "${DASHSCOPE_API_KEY:-}" && "${DEVICE_ADDRESSING_FFMPEG:-}" == /usr/bin/ffmpeg ]]
echo 'core_binary_and_process_preserved=yes'
echo 'addressing_configuration_enabled=yes (secrets hidden)'
query() { MYSQL_PWD="$DB_PASSWORD" mysql --connect-timeout=10 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names -e "$1"; }
[[ "$(query "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('device_addressing_preferences','device_voice_addressing')")" == 2 ]]
[[ "$(query "SELECT COUNT(*) FROM device_business_events WHERE charged_beans<>0 OR billing_mode<>'disabled'")" == 0 ]]
MYSQL_PWD="$DB_PASSWORD" mysql --connect-timeout=10 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch -e "SELECT COUNT(*) AS addressing_tables FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('device_addressing_preferences','device_voice_addressing'); SELECT COUNT(*) AS unexpected_device_charges FROM device_business_events WHERE charged_beans<>0 OR billing_mode<>'disabled'; SELECT COUNT(*) AS inventory_devices FROM inv_devices; SELECT COUNT(*) AS active_room_bindings FROM live_device_room_bindings WHERE status='active'; SELECT COUNT(*) AS running_sessions FROM live_runtime_sessions WHERE status='running';"
for port in 8080 8081 8083; do curl -fsS --max-time 5 "http://127.0.0.1:$port/healthz" >/dev/null; done
for method in GET PUT; do
  [[ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X "$method" -H 'Content-Type: application/json' -d '{"mode":"female"}' http://127.0.0.1:8080/api/v1/live/devices/7/addressing)" == 401 ]]
done
echo 'protected_addressing_routes=yes'
sha256sum /opt/xiaolan/current/bin/management-service /opt/xiaolan/current/bin/xiaozhi-gateway
echo 'device_voice_feedback_post_deploy=PASS'
