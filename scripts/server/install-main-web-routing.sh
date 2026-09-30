#!/usr/bin/env bash
set -euo pipefail

TEMPLATE="${1:-}"
TARGET="${2:-/etc/nginx/sites-enabled/xiaolan}"
MAIN_CERT="${3:-/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.pem}"
MAIN_KEY="${4:-/etc/nginx/ssl/xiaolan/www.xiaolandaizi.cn.key}"

for file in "$TEMPLATE" "$MAIN_CERT" "$MAIN_KEY"; do
  [[ -f "$file" ]] || { echo "required file not found: $file" >&2; exit 2; }
done

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$TEMPLATE" "$TARGET" "$MAIN_CERT" "$MAIN_KEY"
fi

mkdir -p /etc/nginx/xiaolan-backups
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP="/etc/nginx/xiaolan-backups/$(basename "$TARGET").$STAMP"
cp -a "$TARGET" "$BACKUP"

CONTENT="$(cat "$TEMPLATE")"
CONTENT="${CONTENT//__MAIN_CERT__/$MAIN_CERT}"
CONTENT="${CONTENT//__MAIN_KEY__/$MAIN_KEY}"
printf '%s\n' "$CONTENT" > "$TARGET"

if ! nginx -t; then
  cp -a "$BACKUP" "$TARGET"
  nginx -t || true
  echo 'nginx validation failed; previous config restored' >&2
  exit 3
fi

systemctl reload nginx
echo "main web routing installed: $TARGET"
echo "backup: $BACKUP"
