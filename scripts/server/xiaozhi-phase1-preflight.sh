#!/usr/bin/env bash
set -euo pipefail
TEST_BINARY="${1:?test binary required}"
[[ "$TEST_BINARY" == /tmp/xiaozhi-phase1-device-db-test ]] || exit 2
[[ -f /etc/xiaolan/management.env && -x "$TEST_BINARY" ]] || exit 2
set -a
source /etc/xiaolan/management.env
set +a
MYSQL_PWD="$DB_PASSWORD" mysql --connect-timeout=10 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch -e "SELECT COUNT(*) AS inventory_devices FROM inv_devices; SELECT COUNT(*) AS active_room_bindings FROM live_device_room_bindings WHERE status='active'; SELECT COUNT(*) AS running_sessions FROM live_runtime_sessions WHERE status='running';"
TEST_ENV="$(mktemp /tmp/xiaozhi-phase1-db-env.XXXXXX)"
chmod 600 "$TEST_ENV"
trap 'rm -f -- "$TEST_ENV"' EXIT
printf 'DB_HOST=%s\nDB_PORT=%s\nDB_USER=%s\nDB_PASSWORD=%s\nDB_NAME=%s\n' "$DB_HOST" "${DB_PORT:-3306}" "$DB_USER" "$DB_PASSWORD" "$DB_NAME" > "$TEST_ENV"
"$TEST_BINARY" -test.run '^(TestDeviceProvisioningMySQL|TestInventoryStockMySQL|TestDeviceBusinessMySQL|TestDeviceAddressingMySQL)$' -test.v -sales-test-env-file "$TEST_ENV" -sales-test-prefixed-tables
