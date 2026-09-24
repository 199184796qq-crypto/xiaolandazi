#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
"$ROOT/scripts/build-supervisor.sh"

UNIT_DIR="$HOME/.config/systemd/user"
UNIT_FILE="$UNIT_DIR/livecompanion-supervisor.service"
mkdir -p "$UNIT_DIR"

cat > "$UNIT_FILE" <<EOF
[Unit]
Description=LiveCompanion Supervisor
After=network.target

[Service]
Type=simple
WorkingDirectory=$ROOT
ExecStart=$ROOT/supervisor/bin/livecompanion-supervisor -root $ROOT -config $ROOT/configs/supervisor.json
Restart=always
RestartSec=2
Environment=LIVE_COMPANION_ROOT=$ROOT

[Install]
WantedBy=default.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now livecompanion-supervisor.service

echo "[OK] installed systemd user service: $UNIT_FILE"
