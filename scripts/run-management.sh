#!/usr/bin/env sh
set -eu
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
export MGMT_AVATAR_DIR="${MGMT_AVATAR_DIR:-$ROOT/data/avatars}"
mkdir -p "$MGMT_AVATAR_DIR"
exec "$ROOT/management-service/bin/management-service"