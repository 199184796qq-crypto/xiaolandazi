#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

: "${CORE_LOG_FILE:=data/logs/core-service.log}"
export CORE_LOG_FILE

echo "[Live Companion] Core Service"
echo "[Live Companion] Log file: $CORE_LOG_FILE"
echo "[Live Companion] Press Ctrl+C to stop."
echo

exec ./core-service/bin/core-service