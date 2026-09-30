#!/usr/bin/env bash
set -euo pipefail

TEMPLATE="${1:-}"
TARGET="${2:-}"
MAIN_CERT="${3:-}"
MAIN_KEY="${4:-}"
SALES_CERT="${5:-}"
SALES_KEY="${6:-}"

for file in "$TEMPLATE" "$MAIN_CERT" "$MAIN_KEY" "$SALES_CERT" "$SALES_KEY"; do
  [[ -f "$file" ]] || { echo "required file not found: $file" >&2; exit 2; }
done
[[ -n "$TARGET" ]] || { echo 'target nginx config path is required' >&2; exit 2; }

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$TEMPLATE" "$TARGET" "$MAIN_CERT" "$MAIN_KEY" "$SALES_CERT" "$SALES_KEY"
fi

mkdir -p "$(dirname "$TARGET")" /etc/nginx/xiaolan-backups
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP=""
if [[ -f "$TARGET" ]]; then
  BACKUP="/etc/nginx/xiaolan-backups/$(basename "$TARGET").$STAMP"
  cp -a "$TARGET" "$BACKUP"
fi

CONTENT="$(cat "$TEMPLATE")"
CONTENT="${CONTENT//__MAIN_CERT__/$MAIN_CERT}"
CONTENT="${CONTENT//__MAIN_KEY__/$MAIN_KEY}"
CONTENT="${CONTENT//__SALES_CERT__/$SALES_CERT}"
CONTENT="${CONTENT//__SALES_KEY__/$SALES_KEY}"
printf '%s\n' "$CONTENT" > "$TARGET"

if ! nginx -t; then
  if [[ -n "$BACKUP" ]]; then cp -a "$BACKUP" "$TARGET"; else rm -f "$TARGET"; fi
  nginx -t || true
  echo 'nginx validation failed; previous config restored' >&2
  exit 3
fi

systemctl reload nginx
echo "nginx routing installed: $TARGET"
[[ -n "$BACKUP" ]] && echo "backup: $BACKUP"
