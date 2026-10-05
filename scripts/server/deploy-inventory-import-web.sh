#!/usr/bin/env bash
set -euo pipefail
[[ "$(id -u)" == 0 ]] || exit 2
ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
SHA="${4:?SHA required}"
SCOPE="${5:-inventory-import-web-only}"
[[ "$ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$ID.tar.gz" && "$SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2
[[ "$EXPECTED_CURRENT" == /opt/xiaolan/releases/* && "$EXPECTED_CURRENT" != *..* ]] || exit 2
[[ "$SCOPE" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
TARGET="/opt/xiaolan/releases/$ID"
exec 9>/opt/xiaolan/deployments/xiaozhi-phase1.lock
flock -n 9 || exit 3
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" && ! -e "$TARGET" ]] || exit 3
printf '%s  %s\n' "$SHA" "$ARCHIVE" | sha256sum -c -
# Archive is desktop static content only. Reject path traversal or unrelated artifacts.
tar -tzf "$ARCHIVE" | awk '{ if ($0 !~ /^web-desktop\// || $0 ~ /(^|\/)\.\.(\/|$)/) exit 1 }'
PIDS="$(systemctl show -p MainPID --value xiaolan-management xiaolan-core xiaolan-xiaozhi)"
BIN_SHA="$(cd "$EXPECTED_CURRENT" && sha256sum bin/management-service bin/core-service bin/xiaozhi-gateway)"
mkdir "$TARGET"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
tar -xzf "$ARCHIVE" -C "$TARGET"
cp -a "$TARGET/web-desktop/." "$TARGET/web/"
[[ "$(cd "$TARGET" && sha256sum bin/management-service bin/core-service bin/xiaozhi-gateway)" == "$BIN_SHA" ]]
grep -q 'name="xiaolan-app" content="desktop"' "$TARGET/web-desktop/index.html"
[[ -f "$TARGET/web-desktop/templates/inventory-devices.csv" ]]
printf '%s\n' "$ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"%s","deployed_at_utc":"%s"}\n' "$ID" "$(basename "$EXPECTED_CURRENT")" "$SCOPE" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
chown -R ecs-user:ecs-user "$TARGET"
ACTIVATED=0
rollback() {
  trap - ERR
  if [[ "$ACTIVATED" == 1 && "$(readlink -f /opt/xiaolan/current)" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "/opt/xiaolan/.rollback-$ID"
    mv -Tf "/opt/xiaolan/.rollback-$ID" /opt/xiaolan/current
  fi
  echo 'Web deployment failed; previous release restored' >&2
}
trap rollback ERR
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]]
ln -s "$TARGET" "/opt/xiaolan/.current-$ID"
ACTIVATED=1
mv -Tf "/opt/xiaolan/.current-$ID" /opt/xiaolan/current
curl -fsS https://www.xiaolandaizi.cn/templates/inventory-devices.csv | cmp - "$TARGET/web-desktop/templates/inventory-devices.csv"
curl -fsS https://www.xiaolandaizi.cn/resources/inventory | cmp - "$TARGET/web-desktop/index.html"
[[ "$(systemctl show -p MainPID --value xiaolan-management xiaolan-core xiaolan-xiaozhi)" == "$PIDS" ]]
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
trap - ERR
printf 'release_id=%s\nprevious=%s\nscope=%s\nservices_restarted=false\n' "$ID" "$EXPECTED_CURRENT" "$SCOPE" > "/opt/xiaolan/deployments/$ID.txt"
echo "DEPLOYED=$ID; SERVICES_PRESERVED; DATABASE_UNCHANGED"
