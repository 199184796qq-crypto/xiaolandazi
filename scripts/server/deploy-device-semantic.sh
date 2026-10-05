#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:?archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
EXPECTED_ARCHIVE_SHA="${4:?archive SHA required}"

[[ "$(id -u)" == 0 ]] || { echo 'root required' >&2; exit 2; }
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$RELEASE_ID.tar.gz" ]] || exit 2
[[ "$EXPECTED_CURRENT" == /opt/xiaolan/releases/* && "$EXPECTED_CURRENT" != *..* ]] || exit 2
[[ -f "$ARCHIVE" && "$EXPECTED_ARCHIVE_SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2

ROOT=/opt/xiaolan
TARGET="$ROOT/releases/$RELEASE_ID"
BACKUP="$ROOT/deployments/$RELEASE_ID-backup"
mkdir -p "$ROOT/deployments"
exec 9> "$ROOT/deployments/device-semantic.lock"
flock -n 9 || { echo 'another device semantic deployment is active' >&2; exit 3; }
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || { echo 'current changed; abort' >&2; exit 3; }
[[ ! -e "$TARGET" && ! -e "$BACKUP" ]] || { echo 'release already exists' >&2; exit 3; }
printf '%s  %s\n' "$EXPECTED_ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -

CORE_PID="$(systemctl show -p MainPID --value xiaolan-core.service)"
CORE_SHA="$(sha256sum "$EXPECTED_CURRENT/bin/core-service" | cut -d' ' -f1)"
WEB_SHA="$(cd "$EXPECTED_CURRENT" && find web web-desktop web-customer -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)"

mkdir -m 700 "$BACKUP"
cp -a /etc/xiaolan/management.env "$BACKUP/management.env"
cp -a /etc/xiaolan/xiaozhi.env "$BACKUP/xiaozhi.env"
set -a
source /etc/xiaolan/management.env
set +a
echo 'Backing up database before service replacement'
MYSQL_PWD="$DB_PASSWORD" timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
[[ -s "$BACKUP/database-before.sql.gz" ]]

cp -a "$EXPECTED_CURRENT" "$TARGET"
tar -xzf "$ARCHIVE" -C "$TARGET"
[[ -f "$TARGET/bin/management-service" && -f "$TARGET/bin/xiaozhi-gateway" ]]
chmod 755 "$TARGET/bin/management-service" "$TARGET/bin/xiaozhi-gateway"
[[ "$(sha256sum "$TARGET/bin/core-service" | cut -d' ' -f1)" == "$CORE_SHA" ]]
[[ "$(cd "$TARGET" && find web web-desktop web-customer -type f -print0 | sort -z | xargs -0 sha256sum | sha256sum | cut -d' ' -f1)" == "$WEB_SHA" ]]
printf '%s\n' "$RELEASE_ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"device-semantic","dirty":true,"preserved":["core-service","collector-worker","all-web"],"deployed_at_utc":"%s"}\n' "$RELEASE_ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
chown -R ecs-user:ecs-user "$TARGET"

ACTIVATED=0
rollback() {
  trap - ERR
  set +e
  if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "$ROOT/.device-semantic-rollback-$RELEASE_ID"
    mv -Tf "$ROOT/.device-semantic-rollback-$RELEASE_ID" "$ROOT/current"
    systemctl restart xiaolan-management.service xiaolan-xiaozhi.service
  fi
  echo "Deployment failed; previous release restored. Backup: $BACKUP" >&2
}
trap rollback ERR

[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || { echo 'concurrent deployment detected' >&2; exit 5; }
ln -s "$TARGET" "$ROOT/.device-semantic-current-$RELEASE_ID"
ACTIVATED=1
mv -Tf "$ROOT/.device-semantic-current-$RELEASE_ID" "$ROOT/current"
systemctl restart xiaolan-management.service
health_wait() {
  local port="$1"
  for _ in $(seq 1 120); do
    if curl -fsS --max-time 2 "http://127.0.0.1:$port/healthz" >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}
health_wait 8080
systemctl restart xiaolan-xiaozhi.service
health_wait 8083
health_wait 8081
[[ "$(systemctl show -p MainPID --value xiaolan-core.service)" == "$CORE_PID" ]]
[[ "$(sha256sum "$TARGET/bin/core-service" | cut -d' ' -f1)" == "$CORE_SHA" ]]

set -a
source /etc/xiaolan/xiaozhi.env
set +a
[[ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8080/internal/v1/xiaozhi/command-understand)" == 401 ]]
SEMANTIC_RESPONSE="$(curl -fsS --max-time 8 -H 'Content-Type: application/json' -H "X-Xiaozhi-Internal-Token: $XIAOZHI_INTERNAL_TOKEN" -d '{"hardware_mac":"02:00:00:00:ff:fe","request_id":"ctl-semantic-smoke-0001","text":"声音有点吵，收着点"}' http://127.0.0.1:8080/internal/v1/xiaozhi/command-understand)"
grep -q '"status":"understood"' <<< "$SEMANTIC_RESPONSE"
grep -q '"action":"volume"' <<< "$SEMANTIC_RESPONSE"
grep -q '"operation":"adjust"' <<< "$SEMANTIC_RESPONSE"
grep -q '"value":-10' <<< "$SEMANTIC_RESPONSE"
grep -q '"billing_mode":"disabled"' <<< "$SEMANTIC_RESPONSE"

trap - ERR
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nbackup=%s\ncore_pid_preserved=%s\nweb_sha_preserved=%s\n' "$RELEASE_ID" "$EXPECTED_CURRENT" "$TARGET" "$BACKUP" "$CORE_PID" "$WEB_SHA" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "DEPLOYED=$RELEASE_ID"
echo "BACKUP=$BACKUP"
echo "CORE_PID_PRESERVED=$CORE_PID"
echo "SEMANTIC_SMOKE=$SEMANTIC_RESPONSE"
