#!/usr/bin/env bash
set -euo pipefail
ARCHIVE="${1:?archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
EXPECTED_ARCHIVE_SHA="${4:?archive SHA required}"
ADDRESSING_MODEL="${5:-}"
[[ "$(id -u)" == 0 ]] || { echo 'root required' >&2; exit 2; }
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$RELEASE_ID.tar.gz" ]] || exit 2
[[ "$EXPECTED_CURRENT" == /opt/xiaolan/releases/* && "$EXPECTED_CURRENT" != *..* ]] || exit 2
[[ -f "$ARCHIVE" && "$EXPECTED_ARCHIVE_SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2
[[ -z "$ADDRESSING_MODEL" || "$ADDRESSING_MODEL" =~ ^qwen[A-Za-z0-9._-]*omni[A-Za-z0-9._-]*$ ]] || exit 2
ROOT=/opt/xiaolan
TARGET="$ROOT/releases/$RELEASE_ID"
BACKUP="$ROOT/deployments/$RELEASE_ID-backup"
mkdir -p "$ROOT/deployments"
exec 9> "$ROOT/deployments/xiaozhi-phase1.lock"
flock -n 9 || { echo 'another phase1 deployment active' >&2; exit 3; }
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || { echo 'current changed; abort' >&2; exit 3; }
[[ ! -e "$TARGET" && ! -e "$BACKUP" ]] || { echo 'release already exists' >&2; exit 3; }
printf '%s  %s\n' "$EXPECTED_ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
CORE_SHA="$(sha256sum "$EXPECTED_CURRENT/bin/core-service" | cut -d' ' -f1)"
CORE_PID="$(systemctl show -p MainPID --value xiaolan-core.service)"
mkdir -m 700 "$BACKUP"
cp -a /etc/xiaolan/management.env "$BACKUP/management.env"
cp -a /etc/xiaolan/xiaozhi.env "$BACKUP/xiaozhi.env"
cp -a /etc/systemd/system/xiaolan-xiaozhi.service "$BACKUP/xiaolan-xiaozhi.service"
if [[ -f "$ROOT/state/xiaozhi-bindings.json" ]]; then cp -a "$ROOT/state/xiaozhi-bindings.json" "$BACKUP/xiaozhi-bindings.json"; fi
set -a
source /etc/xiaolan/management.env
set +a
echo 'Backing up database before migrations'
MYSQL_PWD="$DB_PASSWORD" timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
[[ -s "$BACKUP/database-before.sql.gz" ]] || exit 4
set -a
source /etc/xiaolan/xiaozhi.env
set +a
[[ "${XIAOZHI_INTERNAL_TOKEN:-}" =~ ^[a-fA-F0-9]{64}$ ]] || { echo 'gateway token must be existing 64-hex secret; abort without rotating it' >&2; exit 4; }
mkdir "$TARGET"
# Preserve the production core, collector and sales mobile artifacts verbatim.
(cd "$EXPECTED_CURRENT" && tar --exclude='./web' --exclude='./web-desktop' --exclude='./web-customer' -cf - .) | tar -xf - -C "$TARGET"
tar -xzf "$ARCHIVE" -C "$TARGET"
cp -a "$TARGET/web-desktop" "$TARGET/web"
[[ -x "$TARGET/bin/core-service" && -f "$TARGET/bin/management-service" && -f "$TARGET/bin/xiaozhi-gateway" ]]
[[ "$(sha256sum "$TARGET/bin/core-service" | cut -d' ' -f1)" == "$CORE_SHA" ]]
grep -q 'name="xiaolan-app" content="desktop"' "$TARGET/web-desktop/index.html"
grep -q 'name="xiaolan-app" content="customer-mobile"' "$TARGET/web-customer/index.html"
chmod 755 "$TARGET/bin/management-service" "$TARGET/bin/xiaozhi-gateway"
printf '%s\n' "$RELEASE_ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"xiaozhi-phase1","dirty":true,"preserved":["core-service","collector-worker","web-sales"],"deployed_at_utc":"%s"}\n' "$RELEASE_ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
chown -R ecs-user:ecs-user "$TARGET"
CONFIG_CHANGED=0
ACTIVATED=0
rollback() {
  trap - ERR
  set +e
  if [[ "$CONFIG_CHANGED" == 1 ]]; then
    cp -a "$BACKUP/management.env" /etc/xiaolan/management.env
    cp -a "$BACKUP/xiaozhi.env" /etc/xiaolan/xiaozhi.env
  fi
  if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "$ROOT/.phase1-rollback-$RELEASE_ID"
    mv -Tf "$ROOT/.phase1-rollback-$RELEASE_ID" "$ROOT/current"
  fi
  if [[ "$CONFIG_CHANGED" == 1 || "$ACTIVATED" == 1 ]]; then
    systemctl restart xiaolan-management.service xiaolan-xiaozhi.service
  fi
  echo "Deployment failed; previous binary/env restored. Additive tables retained. Backup: $BACKUP" >&2
}
trap rollback ERR
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || { echo 'concurrent deployment detected' >&2; exit 5; }
CONFIG_CHANGED=1
MGMT_TEMP="$(mktemp /etc/xiaolan/.management-phase1.XXXXXX)"
awk '!/^XIAOZHI_INTERNAL_TOKEN=/' /etc/xiaolan/management.env > "$MGMT_TEMP"
if [[ -n "$ADDRESSING_MODEL" ]]; then
  ADDRESSING_TEMP="$(mktemp /etc/xiaolan/.addressing-phase1.XXXXXX)"
  awk '!/^DEVICE_ADDRESSING_(MODEL|BASE_URL|FFMPEG)=/' "$MGMT_TEMP" > "$ADDRESSING_TEMP"
  mv -f "$ADDRESSING_TEMP" "$MGMT_TEMP"
  printf '\nDEVICE_ADDRESSING_MODEL="%s"\nDEVICE_ADDRESSING_BASE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"\nDEVICE_ADDRESSING_FFMPEG="/usr/bin/ffmpeg"\n' "$ADDRESSING_MODEL" >> "$MGMT_TEMP"
fi
printf '\nXIAOZHI_INTERNAL_TOKEN="%s"\n' "$XIAOZHI_INTERNAL_TOKEN" >> "$MGMT_TEMP"
chmod 600 "$MGMT_TEMP"
mv -f "$MGMT_TEMP" /etc/xiaolan/management.env
GW_TEMP="$(mktemp /etc/xiaolan/.xiaozhi-phase1.XXXXXX)"
awk '!/^XIAOZHI_MANAGEMENT_BASE_URL=/' /etc/xiaolan/xiaozhi.env > "$GW_TEMP"
printf '\nXIAOZHI_MANAGEMENT_BASE_URL="http://127.0.0.1:8080"\n' >> "$GW_TEMP"
chmod 600 "$GW_TEMP"
mv -f "$GW_TEMP" /etc/xiaolan/xiaozhi.env
ln -s "$TARGET" "$ROOT/.phase1-current-$RELEASE_ID"
ACTIVATED=1
mv -Tf "$ROOT/.phase1-current-$RELEASE_ID" "$ROOT/current"
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
PROVISION="$(curl -fsS --max-time 8 -H 'Content-Type: application/json' -H "X-Xiaozhi-Internal-Token: $XIAOZHI_INTERNAL_TOKEN" -d '{"hardware_mac":"02:00:00:00:ff:fe"}' http://127.0.0.1:8080/internal/v1/xiaozhi/provision)"
grep -q 'unregistered' <<< "$PROVISION"
[[ "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/live/devices/default-name)" == 401 ]]
[[ "$(curl -s -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' http://127.0.0.1:8080/api/v1/live/devices/claim)" == 401 ]]
for path in voice capture command-dispatch command-events business-results; do
  [[ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' "http://127.0.0.1:8080/internal/v1/xiaozhi/$path")" == 401 ]]
done
[[ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/api/v1/live/devices/7/business-events)" == 401 ]]
[[ "$(curl -s --max-time 5 -o /dev/null -w '%{http_code}' -X POST http://127.0.0.1:8083/xiaozhi/v1/capture)" == 401 ]]
trap - ERR
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nbackup=%s\ncore_pid_preserved=%s\n' "$RELEASE_ID" "$EXPECTED_CURRENT" "$TARGET" "$BACKUP" "$CORE_PID" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "DEPLOYED=$RELEASE_ID"
echo "BACKUP=$BACKUP"
echo "CORE_PID_PRESERVED=$CORE_PID"
