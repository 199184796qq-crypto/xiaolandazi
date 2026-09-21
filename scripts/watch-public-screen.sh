#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

LOG_FILE="${CORE_LOG_FILE:-data/logs/core-service.log}"

echo
echo "[Live Companion] Public Screen Monitor"
echo "[Live Companion] Only showing public-screen events."
echo "[Live Companion] Press Ctrl+C to close this monitor."
echo

if [ ! -f "$LOG_FILE" ]; then
  echo "[ERROR] Core log file does not exist: $LOG_FILE"
  echo "Please start Core Service first."
  exit 1
fi

tail -n 100 -F "$LOG_FILE" | grep --line-buffered '\[PUBLIC\]'