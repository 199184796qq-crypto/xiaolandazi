#!/usr/bin/env bash
# Scoped two-phase release: payment-capable backend first, recharge UI second.
# Never roll back to a backend unable to settle recharge after health is ready.
set -euo pipefail
[[ "$(id -u)" == 0 ]] || exit 2
ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
ARCHIVE_SHA="${4:?archive SHA required}"
MANAGEMENT_SHA="${5:?management SHA required}"
[[ "$ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$ID.tar.gz" ]] || exit 2
[[ "$EXPECTED_CURRENT" =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ ]] || exit 2
[[ "$ARCHIVE_SHA" =~ ^[a-fA-F0-9]{64}$ && "$MANAGEMENT_SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2
TARGET="/opt/xiaolan/releases/$ID"
STAGE="/opt/xiaolan/deployments/$ID-candidate"
[[ ! -e "$TARGET" && ! -L "$TARGET" && ! -e "$STAGE" ]] || exit 3
exec 9>/opt/xiaolan/deployments/wechatpay-deployment.lock
flock -n 9 || exit 3
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]] || exit 3
python3 -B -c '
import re,sys
from pathlib import Path
values=re.findall(r"^\s*WECHAT_PAY_ENABLED\s*=([^\r\n]*)$",Path("/etc/xiaolan/management.env").read_text(),re.M)
sys.exit(0 if len(values)==1 and values[0].strip().strip("\"").lower()=="true" else 1)
'
ENV_HASH="$(sha256sum /etc/xiaolan/management.env | cut -d ' ' -f1)"
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
python3 -B - "$ARCHIVE" <<'PY'
import sys,tarfile
from pathlib import PurePosixPath
with tarfile.open(sys.argv[1], 'r:gz') as archive:
    names=set()
    for entry in archive.getmembers():
        path=PurePosixPath(entry.name)
        if path.is_absolute() or '..' in path.parts or not (entry.isfile() or entry.isdir()):
            raise SystemExit('unsafe release entry')
        if not (entry.name in ('bin','bin/','bin/management-service') or path.parts[0] in ('web-customer','web-desktop','web')):
            raise SystemExit('release entry outside recharge scope')
        if entry.name in names:
            raise SystemExit('duplicate release entry')
        names.add(entry.name)
    if not {'bin/management-service','web-customer/index.html','web-desktop/index.html','web/index.html'} <= names:
        raise SystemExit('incomplete recharge release')
PY
PRESERVED_PIDS="$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)"
PRESERVED_HASHES="$(cd "$EXPECTED_CURRENT" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html)"
mkdir -m 0755 "$TARGET"
mkdir -m 0700 "$STAGE"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner
printf '%s  %s\n' "$MANAGEMENT_SHA" "$STAGE/bin/management-service" | sha256sum -c -
grep -q 'name="xiaolan-app" content="customer-mobile"' "$STAGE/web-customer/index.html"
grep -q 'name="xiaolan-app" content="desktop"' "$STAGE/web-desktop/index.html"
cp "$STAGE/bin/management-service" "$TARGET/bin/management-service"
chmod 0755 "$TARGET/bin/management-service"
chown -R ecs-user:ecs-user "$TARGET"
printf '%s\n' "$ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"wechat-wallet-recharge","wechat_pay_enabled":true,"deployed_at_utc":"%s"}\n' "$ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
ACTIVATED=0
READY=0
failure() {
  trap - ERR
  if [[ "$ACTIVATED" == 1 && "$READY" == 0 && "$(readlink -f /opt/xiaolan/current)" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "/opt/xiaolan/.rollback-$ID"
    mv -Tf "/opt/xiaolan/.rollback-$ID" /opt/xiaolan/current
    systemctl restart xiaolan-management.service || true
    echo 'Startup failed; previous release restored before recharge UI was exposed' >&2
  elif [[ "$READY" == 1 ]]; then
    echo 'Verification failed; recharge-capable backend retained to protect payment settlement; inspect candidate and frontend state' >&2
  fi
}
trap failure ERR
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]]
ln -s "$TARGET" "/opt/xiaolan/.current-$ID"
ACTIVATED=1
mv -Tf "/opt/xiaolan/.current-$ID" /opt/xiaolan/current
echo 'Restarting management only; old frontend remains in place until backend checks pass'
systemctl restart xiaolan-management.service
for attempt in $(seq 1 100); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then READY=1; break; fi
  if (( attempt % 10 == 0 )); then echo 'Waiting for bounded management startup migrations'; fi
  sleep 2
done
[[ "$READY" == 1 ]]
BASE='https://www.xiaolandaizi.cn'
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' "$BASE/api/v1/payments/wechat/status")" == 401 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' "$BASE/api/v1/payments/wechat/oauth/callback")" == 401 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{}' "$BASE/api/v1/payments/wechat/notify")" == 400 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{}' "$BASE/api/v1/wallet/recharges")" == 401 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H "Origin: $BASE" -H 'Sec-Fetch-Site: same-origin' -H 'Content-Type: application/json' --data '{}' "$BASE/api/v1/wallet/recharges")" == 401 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'Origin: https://evil.test' -H 'Content-Type: application/json' --data '{}' "$BASE/api/v1/wallet/recharges")" == 403 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' "$BASE/api/v1/wallet/recharges/0")" == 401 ]]
for action in wechat-prepay wechat-query; do
  [[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST "$BASE/api/v1/wallet/recharges/0/$action")" == 401 ]]
done
[[ "$(sha256sum /etc/xiaolan/management.env | cut -d ' ' -f1)" == "$ENV_HASH" ]]
[[ "$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)" == "$PRESERVED_PIDS" ]]
[[ "$(cd "$TARGET" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html)" == "$PRESERVED_HASHES" ]]
# Preserve previous hashed assets. Publish each index last via atomic rename.
for surface in web-customer web-desktop web; do
  find "$STAGE/$surface" -mindepth 1 -maxdepth 1 ! -name index.html -exec cp -a '{}' "$TARGET/$surface/" ';'
  cp "$STAGE/$surface/index.html" "$TARGET/$surface/.index-$ID"
  mv -f "$TARGET/$surface/.index-$ID" "$TARGET/$surface/index.html"
done
chown -R ecs-user:ecs-user "$TARGET/web-customer" "$TARGET/web-desktop" "$TARGET/web"
MOBILE_UA='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
curl -fsS --max-time 15 -A "$MOBILE_UA" "$BASE/wallet" | cmp - "$STAGE/web-customer/index.html"
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' "$BASE/finance" | cmp - "$STAGE/web-desktop/index.html"
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
[[ "$(sha256sum /etc/xiaolan/management.env | cut -d ' ' -f1)" == "$ENV_HASH" ]]
trap - ERR
printf 'release_id=%s\nprevious=%s\nscope=wechat-wallet-recharge\nwechat_pay_enabled=true\nconfiguration_preserved=true\ncore_and_device_services_preserved=true\ndatabase_changes=additive_recharge_payment_link\n' "$ID" "$EXPECTED_CURRENT" > "/opt/xiaolan/deployments/$ID.txt"
echo "DEPLOYED=$ID; PAYMENT_ENABLED; CONFIGURATION_CORE_AND_DEVICE_PRESERVED; NO_LIVE_PAYMENT_CREATED"
