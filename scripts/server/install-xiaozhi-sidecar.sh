#!/usr/bin/env bash
set -euo pipefail

BINARY="${1:-}"
UNIT_TEMPLATE="${2:-}"
ROOT="${XIAOZHI_SIDECAR_ROOT:-/opt/xiaolan/xiaozhi-sidecar}"
STATE_DIR="${XIAOLAN_SERVER_ROOT:-/opt/xiaolan}/state"
ENV_FILE="/etc/xiaolan/xiaozhi.env"
UNIT_FILE="/etc/systemd/system/xiaolan-xiaozhi.service"

[[ -f "$BINARY" ]] || { echo "gateway binary not found: $BINARY" >&2; exit 2; }
[[ -f "$UNIT_TEMPLATE" ]] || { echo "unit template not found: $UNIT_TEMPLATE" >&2; exit 2; }

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n env XIAOZHI_SIDECAR_ROOT="$ROOT" XIAOLAN_SERVER_ROOT="${XIAOLAN_SERVER_ROOT:-/opt/xiaolan}" bash "$0" "$BINARY" "$UNIT_TEMPLATE"
fi

command -v ffmpeg >/dev/null 2>&1 || { echo "ffmpeg is required" >&2; exit 3; }
FFMPEG_ENCODERS="$(ffmpeg -hide_banner -encoders 2>/dev/null || true)"
grep -q "libopus" <<<"$FFMPEG_ENCODERS" || { echo "ffmpeg libopus encoder is required" >&2; exit 3; }
command -v openssl >/dev/null 2>&1 || { echo "openssl is required" >&2; exit 3; }

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
VERSION_DIR="$ROOT/versions/$STAMP"
CURRENT="$ROOT/current"
PREVIOUS="$(readlink -f "$CURRENT" 2>/dev/null || true)"
UNIT_BACKUP=""
if [[ -f "$UNIT_FILE" ]]; then
  UNIT_BACKUP="/etc/systemd/system/xiaolan-xiaozhi.service.$STAMP.bak"
  cp -a "$UNIT_FILE" "$UNIT_BACKUP"
fi

rollback() {
  set +e
  if [[ -n "$PREVIOUS" && -d "$PREVIOUS" ]]; then
    ln -sfn "$PREVIOUS" "$CURRENT"
  else
    rm -f "$CURRENT"
  fi
  if [[ -n "$UNIT_BACKUP" && -f "$UNIT_BACKUP" ]]; then
    cp -a "$UNIT_BACKUP" "$UNIT_FILE"
    systemctl daemon-reload
    systemctl restart xiaolan-xiaozhi.service
  else
    systemctl stop xiaolan-xiaozhi.service
    rm -f "$UNIT_FILE"
    systemctl daemon-reload
  fi
}
trap rollback ERR

mkdir -p "$VERSION_DIR" "$STATE_DIR" /etc/xiaolan
install -m 0755 "$BINARY" "$VERSION_DIR/xiaozhi-gateway"
chown -R ecs-user:ecs-user "$ROOT" "$STATE_DIR"
ln -sfn "$VERSION_DIR" "$CURRENT"

if [[ ! -f "$ENV_FILE" ]]; then
  DEVICE_SECRET="$(openssl rand -hex 32)"
  INTERNAL_TOKEN="$(openssl rand -hex 32)"
  cat > "$ENV_FILE" <<EOF
XIAOZHI_ADDR="127.0.0.1:8083"
XIAOZHI_PUBLIC_WS_URL="wss://www.xiaolandaizi.cn/xiaozhi/v1/"
XIAOZHI_CORE_BASE_URL="http://127.0.0.1:8081"
XIAOZHI_DEVICE_SECRET="$DEVICE_SECRET"
XIAOZHI_INTERNAL_TOKEN="$INTERNAL_TOKEN"
XIAOZHI_BINDINGS_FILE="$STATE_DIR/xiaozhi-bindings.json"
FFMPEG_PATH="ffmpeg"
EOF
  chmod 0600 "$ENV_FILE"
fi

sed 's#/opt/xiaolan/current/bin/xiaozhi-gateway#/opt/xiaolan/xiaozhi-sidecar/current/xiaozhi-gateway#' "$UNIT_TEMPLATE" > "$UNIT_FILE"
chmod 0644 "$UNIT_FILE"
systemctl daemon-reload
systemctl enable xiaolan-xiaozhi.service >/dev/null
systemctl restart xiaolan-xiaozhi.service

ok=0
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:8083/healthz >/dev/null; then
    ok=1
    break
  fi
  sleep 1
done
[[ "$ok" -eq 1 ]] || { echo "xiaozhi gateway health failed" >&2; false; }

trap - ERR
echo "xiaozhi sidecar installed: $VERSION_DIR"
sha256sum "$VERSION_DIR/xiaozhi-gateway"
systemctl is-active xiaolan-xiaozhi.service
curl -fsS http://127.0.0.1:8083/healthz
