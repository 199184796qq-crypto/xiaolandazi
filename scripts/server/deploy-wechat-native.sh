#!/usr/bin/env bash
# Desktop Native QR release. Preserve mobile, core, device, worker, sales and secrets.
set -euo pipefail
[[ "$(id -u)" == 0 ]] || exit 2
ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
ARCHIVE_SHA="${4:?archive SHA required}"
MANAGEMENT_SHA="${5:?management SHA required}"
SCHEMA_CHECK="${6:?read-only schema check required}"
[[ "$ID" =~ ^[A-Za-z0-9._-]+$ && "$ARCHIVE" == "/tmp/$ID.tar.gz" ]] || exit 2
[[ "$SCHEMA_CHECK" == "/tmp/$ID-schema.py" && -f "$SCHEMA_CHECK" && ! -L "$SCHEMA_CHECK" ]] || exit 2
[[ "$EXPECTED_CURRENT" =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ ]] || exit 2
[[ "$ARCHIVE_SHA" =~ ^[a-fA-F0-9]{64}$ && "$MANAGEMENT_SHA" =~ ^[a-fA-F0-9]{64}$ ]] || exit 2
TARGET="/opt/xiaolan/releases/$ID"
STAGE="/opt/xiaolan/deployments/$ID-candidate"
[[ ! -e "$TARGET" && ! -L "$TARGET" && ! -e "$STAGE" && ! -L "$STAGE" ]] || exit 3
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
        if not path.parts or path.is_absolute() or '..' in path.parts or not (entry.isfile() or entry.isdir()):
            raise SystemExit('unsafe release entry')
        if not (entry.name in ('bin','bin/','bin/management-service') or path.parts[0] in ('web-desktop','web')):
            raise SystemExit('release entry outside Native scope')
        if entry.name in names:
            raise SystemExit('duplicate release entry')
        names.add(entry.name)
    if not {'bin/management-service','web-desktop/index.html','web/index.html'} <= names:
        raise SystemExit('incomplete Native release')
PY
PRESERVED_PIDS="$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)"
PRESERVED_HASHES="$(cd "$EXPECTED_CURRENT" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html web-customer/index.html)"
mkdir -m 0755 "$TARGET"
mkdir -m 0700 "$STAGE"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner
printf '%s  %s\n' "$MANAGEMENT_SHA" "$STAGE/bin/management-service" | sha256sum -c -
grep -q 'name="xiaolan-app" content="desktop"' "$STAGE/web-desktop/index.html"
cmp "$STAGE/web-desktop/index.html" "$STAGE/web/index.html"
cp "$STAGE/bin/management-service" "$TARGET/bin/management-service"
chmod 0755 "$TARGET/bin/management-service"
chown -R ecs-user:ecs-user "$TARGET"
printf '%s\n' "$ID" > "$TARGET/VERSION"
printf '{"release_id":"%s","base_release":"%s","scope":"wechat-desktop-native-qr-recharge","wechat_pay_enabled":true,"deployed_at_utc":"%s"}\n' "$ID" "$(basename "$EXPECTED_CURRENT")" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" > "$TARGET/VERSION.json"
ACTIVATED=0
failure() {
  trap - ERR
  if [[ "$ACTIVATED" == 1 ]]; then
    echo 'Verification failed. Payment/refund-capable backend retained; inspect the gate failure before publishing desktop UI.' >&2
  else
    echo 'Preflight failed; running release was not switched.' >&2
  fi
}
trap failure ERR
[[ "$(readlink -f /opt/xiaolan/current)" == "$EXPECTED_CURRENT" ]]
ln -s "$TARGET" "/opt/xiaolan/.current-$ID"
mv -Tf "/opt/xiaolan/.current-$ID" /opt/xiaolan/current
ACTIVATED=1
echo 'Restarting management only; old desktop remains until backend gates pass'
systemctl restart xiaolan-management.service
READY=0
for attempt in $(seq 1 100); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then READY=1; break; fi
  if (( attempt % 10 == 0 )); then echo 'Waiting for bounded management startup migrations'; fi
  sleep 2
done
[[ "$READY" == 1 ]]
python3 -B "$SCHEMA_CHECK" --require-native
BASE='https://www.xiaolandaizi.cn'
probe() {
  local expected="$1" path="$2" actual
  shift 2
  actual="$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' "$@" "$BASE$path")"
  printf 'HTTP gate %s expected=%s actual=%s\n' "$path" "$expected" "$actual"
  [[ "$actual" == "$expected" ]]
}
probe 401 /api/v1/payments/wechat/status
probe 401 /api/v1/payments/wechat/oauth/callback
for callback in notify refund-notify; do
  probe 400 "/api/v1/payments/wechat/$callback" -X POST -H 'Content-Type: application/json' --data '{}'
done
for collection in recharges wechat-refunds; do
  probe 401 "/api/v1/wallet/$collection" -X POST -H "Origin: $BASE" -H 'Content-Type: application/json' --data '{}'
  probe 403 "/api/v1/wallet/$collection" -X POST -H 'Origin: https://evil.test' -H 'Content-Type: application/json' --data '{}'
done
probe 401 /api/v1/wallet/wechat-refunds
probe 401 /api/v1/wallet/wechat-refunds/0/query -X POST -H "Origin: $BASE"
probe 401 /api/v1/finance/wechat-refunds
probe 401 /api/v1/finance/wechat-refunds/0/query -X POST -H "Origin: $BASE"
probe 403 /api/v1/finance/wechat-refunds/0/query -X POST -H 'Origin: https://evil.test'
probe 401 /api/v1/wallet/recharges/0
for action in wechat-prepay wechat-native-prepay wechat-query; do
  probe 401 "/api/v1/wallet/recharges/0/$action" -X POST -H "Origin: $BASE"
  probe 403 "/api/v1/wallet/recharges/0/$action" -X POST -H 'Origin: https://evil.test'
done
[[ "$(sha256sum /etc/xiaolan/management.env | cut -d ' ' -f1)" == "$ENV_HASH" ]]
[[ "$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)" == "$PRESERVED_PIDS" ]]
[[ "$(cd "$TARGET" && sha256sum bin/core-service bin/xiaozhi-gateway collector-worker/worker.mjs web-sales/index.html web-customer/index.html)" == "$PRESERVED_HASHES" ]]
echo 'Backend, schema and financial security gates passed; publishing desktop QR UI'
# Keep previous hashed assets for existing sessions. Publish indexes last.
for surface in web-desktop web; do
  find "$STAGE/$surface" -mindepth 1 -maxdepth 1 ! -name index.html -exec cp -a '{}' "$TARGET/$surface/" ';'
  cp "$STAGE/$surface/index.html" "$TARGET/$surface/.index-$ID"
  mv -f "$TARGET/$surface/.index-$ID" "$TARGET/$surface/index.html"
done
chown -R ecs-user:ecs-user "$TARGET/web-desktop" "$TARGET/web"
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' "$BASE/finance" | cmp - "$STAGE/web-desktop/index.html"
MOBILE_UA='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
curl -fsS --max-time 15 -A "$MOBILE_UA" "$BASE/wallet" | cmp - "$TARGET/web-customer/index.html"
systemctl is-active xiaolan-management xiaolan-core xiaolan-xiaozhi
[[ "$(sha256sum /etc/xiaolan/management.env | cut -d ' ' -f1)" == "$ENV_HASH" ]]
[[ "$(systemctl show -p MainPID --value xiaolan-core xiaolan-xiaozhi)" == "$PRESERVED_PIDS" ]]
trap - ERR
printf 'release_id=%s\nprevious=%s\nscope=wechat-desktop-native-qr-recharge\nwechat_pay_enabled=true\nconfiguration_sha256=%s\nconfiguration_preserved=true\ncore_device_and_mobile_preserved=true\ndatabase_changes=additive_provider_code_url_column\nno_live_payment_or_refund_submitted_by_deployment=true\n' "$ID" "$EXPECTED_CURRENT" "$ENV_HASH" > "/opt/xiaolan/deployments/$ID.txt"
echo "DEPLOYED=$ID; NATIVE_ROUTES_ENABLED; CONFIGURATION_CORE_DEVICE_MOBILE_PRESERVED; NO_LIVE_PAYMENT_OR_REFUND_SUBMITTED"
