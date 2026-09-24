#!/usr/bin/env sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
"$ROOT/scripts/build-supervisor.sh"

PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_FILE="$PLIST_DIR/com.banbo.livecompanion.supervisor.plist"
mkdir -p "$PLIST_DIR" "$ROOT/data/logs"

cat > "$PLIST_FILE" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.banbo.livecompanion.supervisor</string>
  <key>ProgramArguments</key>
  <array>
    <string>$ROOT/supervisor/bin/livecompanion-supervisor</string>
    <string>-root</string>
    <string>$ROOT</string>
    <string>-config</string>
    <string>$ROOT/configs/supervisor.json</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$ROOT</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>LIVE_COMPANION_ROOT</key>
    <string>$ROOT</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>$ROOT/data/logs/supervisor.launchd.stdout.log</string>
  <key>StandardErrorPath</key>
  <string>$ROOT/data/logs/supervisor.launchd.stderr.log</string>
</dict>
</plist>
EOF

launchctl bootout "gui/$(id -u)" "$PLIST_FILE" >/dev/null 2>&1 || true
launchctl bootstrap "gui/$(id -u)" "$PLIST_FILE"

echo "[OK] installed LaunchAgent: $PLIST_FILE"
