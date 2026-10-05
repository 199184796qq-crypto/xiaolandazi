#!/usr/bin/env bash
set -euo pipefail
[[ "$(id -u)" == 0 ]] || exit 2
MODE="${1:-dry-run}"
[[ "$MODE" == dry-run || "$MODE" == apply ]] || exit 2
BIN=/tmp/inventory-e2e-cleanup
[[ -x "$BIN" ]] || exit 2
set -a
source /etc/xiaolan/management.env
set +a
if [[ "$MODE" == dry-run ]]; then "$BIN"; exit; fi
BACKUP=/opt/xiaolan/deployments/20261002-inventory-e2e-cleanup
[[ ! -e "$BACKUP" ]] || { echo 'Backup already exists; do not repeat cleanup' >&2; exit 3; }
exec 9>/opt/xiaolan/deployments/xiaozhi-phase1.lock
flock -n 9 || exit 3
# Verify the exact scope before any service interruption or write.
"$BIN"
mkdir -m 700 "$BACKUP"
echo 'Backing up database before deleting the inspected E2E fixtures'
MYSQL_PWD="$DB_PASSWORD" timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
gzip -t "$BACKUP/database-before.sql.gz"
[[ -s "$BACKUP/database-before.sql.gz" ]] || exit 4
# Prevent inventory mutations during the short cleanup transaction. Core stays running.
trap 'systemctl start xiaolan-management.service' EXIT
systemctl stop xiaolan-management.service
"$BIN" --apply | tee "$BACKUP/cleanup-report.json"
systemctl start xiaolan-management.service
trap - EXIT
echo "E2E cleanup committed. Recoverable full backup: $BACKUP/database-before.sql.gz"
