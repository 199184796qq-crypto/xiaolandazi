#!/usr/bin/env bash
set -euo pipefail

[[ "$(id -u)" == 0 ]] || { echo 'root required' >&2; exit 2; }
set -a
source /etc/xiaolan/xiaozhi.env
set +a

understand() {
  local id="$1"
  local text="$2"
  curl -fsS --max-time 8 \
    -H 'Content-Type: application/json' \
    -H "X-Xiaozhi-Internal-Token: $XIAOZHI_INTERNAL_TOKEN" \
    --data "{\"hardware_mac\":\"02:00:00:00:ff:fe\",\"request_id\":\"$id\",\"text\":\"$text\"}" \
    http://127.0.0.1:8080/internal/v1/xiaozhi/command-understand
}

FONT="$(understand ctl-semantic-verify-font-0001 '字有点小，我看不清')"
CAPTURE="$(understand ctl-semantic-verify-camera-0001 '帮我瞧瞧周围')"
AMBIGUOUS="$(understand ctl-semantic-verify-ambiguous-0001 '帮我调一下')"
RISKY="$(understand ctl-semantic-verify-risky-0001 '恢复出厂设置')"

grep -q '"status":"understood"' <<< "$FONT"
grep -q '"action":"font_size"' <<< "$FONT"
grep -q '"operation":"adjust"' <<< "$FONT"
grep -q '"value":1' <<< "$FONT"
grep -q '"charged_beans":0' <<< "$FONT"

grep -q '"status":"understood"' <<< "$CAPTURE"
grep -q '"action":"capture"' <<< "$CAPTURE"
grep -q '"operation":"set"' <<< "$CAPTURE"
grep -q '"charged_beans":0' <<< "$CAPTURE"

grep -Eq '"status":"(clarify|unsupported)"' <<< "$AMBIGUOUS"
grep -q '"status":"unsupported"' <<< "$RISKY"

echo "FONT=$FONT"
echo "CAPTURE=$CAPTURE"
echo "AMBIGUOUS=$AMBIGUOUS"
echo "RISKY=$RISKY"
