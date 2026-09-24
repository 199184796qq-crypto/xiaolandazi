#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
OUT_DIR="$ROOT/supervisor/bin"
OUT="$OUT_DIR/livecompanion-supervisor"

mkdir -p "$OUT_DIR"
cd "$ROOT"
go build -o "$OUT" ./supervisor/cmd/supervisor
chmod +x "$OUT"

echo "[OK] built $OUT"
