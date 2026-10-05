#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:-}"
RELEASE_ID="${2:-}"
ROOT="${XIAOLAN_SERVER_ROOT:-/opt/xiaolan}"

if [[ -z "$ARCHIVE" || ! -f "$ARCHIVE" ]]; then
  echo "release archive not found: $ARCHIVE" >&2
  exit 2
fi
if [[ -z "$RELEASE_ID" ]]; then
  RELEASE_ID="$(basename "$ARCHIVE" .tar.gz)"
fi
if [[ ! "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "invalid release id: $RELEASE_ID" >&2
  exit 2
fi

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n env XIAOLAN_SERVER_ROOT="$ROOT" bash "$0" "$ARCHIVE" "$RELEASE_ID"
fi

RELEASES="$ROOT/releases"
TARGET="$RELEASES/$RELEASE_ID"
STAGING="$RELEASES/.staging-$RELEASE_ID-$$"
CURRENT="$ROOT/current"
PREVIOUS="$(readlink -f "$CURRENT" 2>/dev/null || true)"

mkdir -p "$RELEASES" "$ROOT/deployments"
if [[ -e "$TARGET" ]]; then
  echo "release already exists: $TARGET" >&2
  exit 3
fi
trap 'rm -rf "$STAGING"' EXIT
mkdir -p "$STAGING"
tar -xzf "$ARCHIVE" -C "$STAGING"

required=(
  "bin/core-service"
  "bin/management-service"
  "bin/xiaozhi-gateway"
  "collector-worker/worker.mjs"
  "collector-worker/node_modules/playwright-core/package.json"
  "web/index.html"
  "web-desktop/index.html"
  "web-customer/index.html"
  "web-sales/index.html"
  "systemd/xiaolan-xiaozhi.service"
  "plugins/anchor-style/self-correction/plugin.json"
  "plugins/anchor-style/numeric-repair/plugin.json"
  "VERSION.json"
)
for rel in "${required[@]}"; do
  [[ -e "$STAGING/$rel" ]] || { echo "release missing $rel" >&2; exit 4; }
done

grep -q 'name="xiaolan-app" content="desktop"' "$STAGING/web-desktop/index.html"
grep -q 'name="xiaolan-app" content="customer-mobile"' "$STAGING/web-customer/index.html"
grep -q 'name="xiaolan-app" content="sales-mobile"' "$STAGING/web-sales/index.html"

chmod 0755 "$STAGING/bin/core-service" "$STAGING/bin/management-service" "$STAGING/bin/xiaozhi-gateway"
chown -R ecs-user:ecs-user "$STAGING"
mv "$STAGING" "$TARGET"
trap - EXIT

XIAOZHI_ENV=/etc/xiaolan/xiaozhi.env
STATE_DIR="$ROOT/state"
mkdir -p /etc/xiaolan "$STATE_DIR"
chown ecs-user:ecs-user "$STATE_DIR"

if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "xiaozhi gateway requires ffmpeg with libopus support" >&2
  exit 4
fi
FFMPEG_ENCODERS="$(ffmpeg -hide_banner -encoders 2>/dev/null || true)"
if ! grep -q 'libopus' <<<"$FFMPEG_ENCODERS"; then
  echo "ffmpeg does not provide libopus encoder" >&2
  exit 4
fi

if [[ ! -f "$XIAOZHI_ENV" ]]; then
  command -v openssl >/dev/null 2>&1 || { echo "openssl is required to initialize xiaozhi secrets" >&2; exit 4; }
  DEVICE_SECRET="$(openssl rand -hex 32)"
  INTERNAL_TOKEN="$(openssl rand -hex 32)"
  cat > "$XIAOZHI_ENV" <<EOF
XIAOZHI_ADDR="127.0.0.1:8083"
XIAOZHI_PUBLIC_WS_URL="wss://www.xiaolandaizi.cn/xiaozhi/v1/"
XIAOZHI_CORE_BASE_URL="http://127.0.0.1:8081"
XIAOZHI_DEVICE_SECRET="$DEVICE_SECRET"
XIAOZHI_INTERNAL_TOKEN="$INTERNAL_TOKEN"
XIAOZHI_BINDINGS_FILE="$STATE_DIR/xiaozhi-bindings.json"
FFMPEG_PATH="ffmpeg"
EOF
  chmod 0600 "$XIAOZHI_ENV"
fi

install -m 0644 "$TARGET/systemd/xiaolan-xiaozhi.service" /etc/systemd/system/xiaolan-xiaozhi.service
systemctl daemon-reload
systemctl enable xiaolan-xiaozhi.service >/dev/null

TMP_LINK="$ROOT/.current-$RELEASE_ID-$$"
ln -s "$TARGET" "$TMP_LINK"
mv -Tf "$TMP_LINK" "$CURRENT"

health_wait() {
  local url="$1"
  local name="$2"
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 2 "$url" >/dev/null; then
      echo "$name health: ok"
      return 0
    fi
    sleep 1
  done
  echo "$name health failed: $url" >&2
  return 1
}

rollback() {
  if [[ -n "$PREVIOUS" && -d "$PREVIOUS" ]]; then
    local rollback_link="$ROOT/.rollback-$$"
    ln -s "$PREVIOUS" "$rollback_link"
    mv -Tf "$rollback_link" "$CURRENT"
    systemctl restart xiaolan-core.service || true
    systemctl restart xiaolan-management.service || true
    if [[ -x "$PREVIOUS/bin/xiaozhi-gateway" ]]; then
      systemctl restart xiaolan-xiaozhi.service || true
    else
      systemctl stop xiaolan-xiaozhi.service || true
    fi
    echo "rolled back to $PREVIOUS" >&2
  fi
}

if ! systemctl restart xiaolan-core.service; then
  rollback
  exit 5
fi
if ! systemctl restart xiaolan-management.service; then
  rollback
  exit 5
fi
if ! systemctl restart xiaolan-xiaozhi.service; then
  rollback
  exit 5
fi
if ! health_wait http://127.0.0.1:8081/healthz core; then
  rollback
  exit 6
fi
if ! health_wait http://127.0.0.1:8080/healthz management; then
  rollback
  exit 6
fi
if ! health_wait http://127.0.0.1:8083/healthz xiaozhi; then
  rollback
  exit 6
fi

cat > "$ROOT/deployments/$RELEASE_ID.txt" <<EOF
release_id=$RELEASE_ID
deployed_at_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)
previous=$PREVIOUS
current=$TARGET
EOF

echo "deploy ok: $RELEASE_ID"
echo "current: $TARGET"
