#!/usr/bin/env bash
# Publish only payment-related application components. Never enables collection.
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
[[ ! -e "$TARGET" && ! -L "$TARGET" ]] || exit 3
exec 9>/opt/xiaolan/deployments/wechatpay-deployment.lock
flock -n 9 || exit 3
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]] || exit 3
python3 -B -c '
import re,sys
from pathlib import Path
text=Path("/etc/xiaolan/management.env").read_text()
values=re.findall(r"^\s*WECHAT_PAY_ENABLED\s*=([^\r\n]*)$",text,re.M)
sys.exit(0 if len(values)==1 and values[0].strip().strip("\"").lower()=="false" else 1)
'
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
# Only directories and regular files in the four expected application locations.
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
            raise SystemExit('release entry outside payment scope')
        if entry.name in names:
            raise SystemExit('duplicate release entry')
        names.add(entry.name)
    if not {'bin/management-service','web-customer/index.html','web-desktop/index.html','web/index.html'} <= names:
        raise SystemExit('incomplete payment release')
PY
PRESERVED_PIDS="$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)"
PRESERVED_HASHES="$(cd "$EXPECTED_CURRENT" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html)"
mkdir -m 0755 "$TARGET"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
# Keep previous hashed frontend assets for pages already open in browsers.
tar -xzf "$ARCHIVE" -C "$TARGET" --no-same-owner
chmod 0755 "$TARGET/bin/management-service"
printf '%s  %s\n' "$MANAGEMENT_SHA" "$TARGET/bin/management-service" | sha256sum -c -
[[ "$(cd "$TARGET" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html)" == "$PRESERVED_HASHES" ]]
grep -q 'name="xiaolan-app" content="customer-mobile"' "$TARGET/web-customer/index.html"
grep -q 'name="xiaolan-app" content="desktop"' "$TARGET/web-desktop/index.html"
printf '%s\n' "$ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"wechatpay-jsapi-disabled","collection_enabled":false,"deployed_at_utc":"%s"}\n' "$ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
chown -R ecs-user:ecs-user "$TARGET"
chmod 0755 "$TARGET"
ACTIVATED=0
rollback() {
  trap - ERR
  if [[ "$ACTIVATED" == 1 && "$(readlink -f /opt/xiaolan/current)" == "$TARGET" ]]; then
    ln -s "$EXPECTED_CURRENT" "/opt/xiaolan/.rollback-$ID"
    mv -Tf "/opt/xiaolan/.rollback-$ID" /opt/xiaolan/current
    systemctl restart xiaolan-management.service || true
    echo 'Previous release restored; additive payment schema retained; collection remains disabled' >&2
  fi
}
trap rollback ERR
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]]
ln -s "$TARGET" "/opt/xiaolan/.current-$ID"
ACTIVATED=1
mv -Tf "/opt/xiaolan/.current-$ID" /opt/xiaolan/current
echo 'Payment candidate activated; restarting management only, collection remains disabled'
systemctl restart xiaolan-management.service
HEALTHY=0
for attempt in $(seq 1 100); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then
    HEALTHY=1
    break
  fi
  if (( attempt % 10 == 0 )); then echo 'Waiting for bounded management startup migrations'; fi
  sleep 2
done
[[ "$HEALTHY" == 1 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' https://www.xiaolandaizi.cn/api/v1/payments/wechat/status)" == 401 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' https://www.xiaolandaizi.cn/api/v1/payments/wechat/oauth/callback)" == 503 ]]
[[ "$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST https://www.xiaolandaizi.cn/api/v1/payments/wechat/notify)" == 503 ]]
MOBILE_UA='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
curl -fsS --max-time 15 -A "$MOBILE_UA" https://www.xiaolandaizi.cn/shop/checkout | cmp - "$TARGET/web-customer/index.html"
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' https://www.xiaolandaizi.cn/ | cmp - "$TARGET/web-desktop/index.html"
[[ "$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)" == "$PRESERVED_PIDS" ]]
[[ "$(cd "$TARGET" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html)" == "$PRESERVED_HASHES" ]]
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
trap - ERR
printf 'release_id=%s\nprevious=%s\nscope=wechatpay-jsapi-disabled\ncollection_enabled=false\nmanagement_restarted=true\ncore_and_device_services_preserved=true\ndatabase_changes=additive_payment_schema\n' "$ID" "$EXPECTED_CURRENT" > "/opt/xiaolan/deployments/$ID.txt"
echo "DEPLOYED=$ID; COLLECTION_DISABLED; CORE_AND_DEVICE_PRESERVED; NO_PAYMENT_ORDER_CREATED"
