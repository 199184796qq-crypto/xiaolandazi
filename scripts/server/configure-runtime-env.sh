#!/usr/bin/env bash
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0"
fi

CORE_ENV=/etc/xiaolan/core.env
MGMT_ENV=/etc/xiaolan/management.env
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_DIR=/etc/xiaolan/backups/runtime-env-$STAMP
mkdir -p "$BACKUP_DIR"
cp -a "$CORE_ENV" "$BACKUP_DIR/core.env"
cp -a "$MGMT_ENV" "$BACKUP_DIR/management.env"

set_env() {
  local file="$1" key="$2" value="$3"
  local escaped
  escaped="$(printf '%s' "$value" | sed 's/[&|]/\\&/g')"
  if grep -q "^${key}=" "$file"; then
    sed -i "s|^${key}=.*|${key}=\"${escaped}\"|" "$file"
  else
    printf '%s="%s"\n' "$key" "$value" >> "$file"
  fi
}

set_env "$CORE_ENV" COLLECTOR_WORKER_PATH /opt/xiaolan/current/collector-worker/worker.mjs
set_env "$CORE_ENV" CORE_PUBLIC_URL https://www.xiaolandaizi.cn/core-audio
set_env "$MGMT_ENV" AUTH_COOKIE_SECURE true

echo "runtime env configured; backup: $BACKUP_DIR"
grep -E '^(CORE_PUBLIC_URL|COLLECTOR_WORKER_PATH)=' "$CORE_ENV"
grep -E '^AUTH_COOKIE_SECURE=' "$MGMT_ENV"
