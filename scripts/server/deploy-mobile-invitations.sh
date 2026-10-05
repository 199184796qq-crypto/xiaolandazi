#!/usr/bin/env bash
set -euo pipefail

[[ "$(id -u)" == 0 ]] || exit 2
ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
SHA="${4:?SHA required}"
INVITE_ASSET="${5:?invite route asset required}"
REGISTER_ASSET="${6:?register route asset required}"

[[ "$ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$ID.tar.gz" && "$SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2
[[ "$EXPECTED_CURRENT" == /opt/xiaolan/releases/* && "$EXPECTED_CURRENT" != *..* ]] || exit 2
[[ "$INVITE_ASSET" =~ ^_app/immutable/nodes/[A-Za-z0-9._-]+\.js$ ]] || exit 2
[[ "$REGISTER_ASSET" =~ ^_app/immutable/nodes/[A-Za-z0-9._-]+\.js$ ]] || exit 2

TARGET="/opt/xiaolan/releases/$ID"
exec 9>/opt/xiaolan/deployments/xiaolan-release.lock
flock -n 9 || exit 3
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" && ! -e "$TARGET" ]] || exit 3
printf '%s  %s\n' "$SHA" "$ARCHIVE" | sha256sum -c -
tar -tzf "$ARCHIVE" | awk '{ if ($0 !~ /^web-customer\// || $0 ~ /(^|\/)\.\.(\/|$)/) exit 1 }'

PIDS="$(systemctl show -p MainPID --value xiaolan-management xiaolan-core xiaolan-xiaozhi)"
BIN_SHA="$(cd "$EXPECTED_CURRENT" && sha256sum bin/management-service bin/core-service bin/xiaozhi-gateway)"
mkdir "$TARGET"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
tar -xzf "$ARCHIVE" -C "$TARGET"
[[ "$(cd "$TARGET" && sha256sum bin/management-service bin/core-service bin/xiaozhi-gateway)" == "$BIN_SHA" ]]
cmp "$EXPECTED_CURRENT/web-desktop/index.html" "$TARGET/web-desktop/index.html"
grep -q 'name="xiaolan-app" content="customer-mobile"' "$TARGET/web-customer/index.html"
grep -q 'MY INVITE CODE' "$TARGET/web-customer/$INVITE_ASSET"
grep -q 'INVITATION REGISTRATION' "$TARGET/web-customer/$REGISTER_ASSET"

printf '%s\n' "$ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"mobile-invitations","deployed_at_utc":"%s"}\n' \
  "$ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
chown -R ecs-user:ecs-user "$TARGET"

ACTIVATED=0
rollback() {
  trap - ERR
  if [[ "$ACTIVATED" == 1 && "$(readlink -f /opt/xiaolan/current)" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "/opt/xiaolan/.rollback-$ID"
    mv -Tf "/opt/xiaolan/.rollback-$ID" /opt/xiaolan/current
  fi
  echo 'Mobile invitation deployment failed; previous release restored' >&2
}
trap rollback ERR

ln -s "$TARGET" "/opt/xiaolan/.current-$ID"
ACTIVATED=1
mv -Tf "/opt/xiaolan/.current-$ID" /opt/xiaolan/current

MOBILE_UA='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
curl -fsS --max-time 15 -A "$MOBILE_UA" https://www.xiaolandaizi.cn/invite | cmp - "$TARGET/web-customer/index.html"
curl -fsS --max-time 15 -A "$MOBILE_UA" https://www.xiaolandaizi.cn/register?invite=CHECK | cmp - "$TARGET/web-customer/index.html"
curl -fsS --max-time 15 -A "$MOBILE_UA" "https://www.xiaolandaizi.cn/$INVITE_ASSET" | cmp - "$TARGET/web-customer/$INVITE_ASSET"
curl -fsS --max-time 15 -A "$MOBILE_UA" "https://www.xiaolandaizi.cn/$REGISTER_ASSET" | cmp - "$TARGET/web-customer/$REGISTER_ASSET"
[[ "$(systemctl show -p MainPID --value xiaolan-management xiaolan-core xiaolan-xiaozhi)" == "$PIDS" ]]
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
trap - ERR

printf 'release_id=%s\nprevious=%s\nscope=mobile-invitations\nservices_restarted=false\ndatabase_changed=false\n' \
  "$ID" "$EXPECTED_CURRENT" > "/opt/xiaolan/deployments/$ID.txt"
echo "DEPLOYED=$ID; SERVICES_PRESERVED; DATABASE_UNCHANGED; DESKTOP_UNCHANGED"
