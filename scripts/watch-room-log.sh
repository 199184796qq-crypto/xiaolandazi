#!/usr/bin/env sh
set -eu

ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT_DIR"

LOG_FILE="${CORE_LOG_FILE:-data/logs/core-service.log}"
ROOM_FILTER="${1:-}"
MODE="${2:-public}"

if [ ! -f "$LOG_FILE" ]; then
  echo "[ERROR] Core log file does not exist: $LOG_FILE"
  exit 1
fi

echo "[Live Companion] Room Log Monitor"
if [ -n "$ROOM_FILTER" ]; then
  echo "[Live Companion] Room filter: $ROOM_FILTER"
else
  echo "[Live Companion] Room filter: all rooms"
fi
echo "[Live Companion] Mode: $MODE"
echo "[Live Companion] Ctrl+C closes this viewer only."
echo

tail -n 150 -F "$LOG_FILE" | awk -v room="$ROOM_FILTER" -v mode="$MODE" '
{
  line=$0
  room_ok = (room == "")

  if (!room_ok) {
    if (line ~ ("room=" room "([[:space:]]|$)")) room_ok=1
    if (line ~ ("web_rid=" room "([[:space:]]|$)")) room_ok=1
  }

  if (!room_ok) next

  if (mode == "public") {
    if (line ~ /\[PUBLIC\]/) print line
    next
  }

  if (mode == "errors") {
    lower=tolower(line)
    if (
      lower ~ /error/ ||
      lower ~ /failed/ ||
      lower ~ /offline/ ||
      lower ~ /timeout/ ||
      lower ~ /panic/ ||
      lower ~ /fatal/ ||
      lower ~ /warn/
    ) print line
    next
  }

  print line
  fflush()
}
'