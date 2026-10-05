#!/usr/bin/env bash
set -euo pipefail

DEVICE_ID="${1:-}"
ROOM_ID="${2:-}"
ACTION="${3:-bind}"
ENV_FILE="${XIAOZHI_ENV_FILE:-/etc/xiaolan/xiaozhi.env}"

if [[ ! "$DEVICE_ID" =~ ^[A-Za-z0-9:._-]{2,160}$ ]]; then
  echo "invalid device id" >&2
  exit 2
fi
if [[ ! "$ROOM_ID" =~ ^[1-9][0-9]*$ ]]; then
  echo "room id must be a positive integer" >&2
  exit 2
fi
if [[ "$ACTION" != "bind" && "$ACTION" != "unbind" ]]; then
  echo "action must be bind or unbind" >&2
  exit 2
fi
[[ -f "$ENV_FILE" ]] || { echo "xiaozhi env not found: $ENV_FILE" >&2; exit 3; }

set -a
# shellcheck disable=SC1090
source "$ENV_FILE"
set +a
TOKEN="${XIAOZHI_INTERNAL_TOKEN:-}"
[[ -n "$TOKEN" ]] || { echo "XIAOZHI_INTERNAL_TOKEN is empty" >&2; exit 3; }

BASE_URL="http://127.0.0.1:8083/internal/v1/bindings/$DEVICE_ID"
if [[ "$ACTION" == "unbind" ]]; then
  curl -fsS -X DELETE \
    -H "X-Xiaozhi-Internal-Token: $TOKEN" \
    "$BASE_URL"
else
  curl -fsS -X PUT \
    -H "X-Xiaozhi-Internal-Token: $TOKEN" \
    -H "Content-Type: application/json" \
    --data "{\"room_id\":$ROOM_ID}" \
    "$BASE_URL"
fi
printf '\n'
