#!/usr/bin/env bash
set -euo pipefail

RELEASE_ID="${1:-}"
ROOT="${XIAOLAN_SERVER_ROOT:-/opt/xiaolan}"

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n env XIAOLAN_SERVER_ROOT="$ROOT" bash "$0" "$RELEASE_ID"
fi

CURRENT="$ROOT/current"
CURRENT_REAL="$(readlink -f "$CURRENT" 2>/dev/null || true)"

if [[ -n "$RELEASE_ID" ]]; then
  TARGET="$ROOT/releases/$RELEASE_ID"
else
  CURRENT_ID="$(basename "$CURRENT_REAL")"
  RECORD="$ROOT/deployments/$CURRENT_ID.txt"
  [[ -f "$RECORD" ]] || { echo "no deployment record for $CURRENT_ID" >&2; exit 2; }
  TARGET="$(awk -F= '$1=="previous" {print substr($0,10)}' "$RECORD")"
fi

[[ -d "$TARGET" ]] || { echo "rollback target not found: $TARGET" >&2; exit 3; }
[[ -x "$TARGET/bin/core-service" ]] || { echo 'rollback target missing core-service' >&2; exit 3; }
[[ -x "$TARGET/bin/management-service" ]] || { echo 'rollback target missing management-service' >&2; exit 3; }

TMP_LINK="$ROOT/.rollback-$$"
ln -s "$TARGET" "$TMP_LINK"
mv -Tf "$TMP_LINK" "$CURRENT"

systemctl restart xiaolan-core.service
systemctl restart xiaolan-management.service

for url in http://127.0.0.1:8081/healthz http://127.0.0.1:8080/healthz; do
  ok=0
  for _ in $(seq 1 30); do
    if curl -fsS --max-time 2 "$url" >/dev/null; then ok=1; break; fi
    sleep 1
  done
  [[ "$ok" -eq 1 ]] || { echo "rollback health failed: $url" >&2; exit 4; }
done

echo "rollback ok: $(basename "$TARGET")"
