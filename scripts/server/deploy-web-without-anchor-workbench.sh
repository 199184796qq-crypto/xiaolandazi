#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:?frontend archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
EXPECTED_SHA256="${4:?archive sha256 required}"
ROOT=/opt/xiaolan

[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'invalid release id' >&2; exit 2; }
[[ "$EXPECTED_SHA256" =~ ^[a-f0-9]{64}$ ]] || { echo 'invalid sha256' >&2; exit 2; }
[[ -f "$ARCHIVE" ]] || { echo 'frontend archive not found' >&2; exit 2; }
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || { echo 'invalid base release' >&2; exit 2; }

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$EXPECTED_SHA256"
fi

[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$EXPECTED_SHA256" ]] || {
  echo 'frontend archive sha256 mismatch' >&2
  exit 3
}
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || {
  echo 'current release changed; recheck before publishing' >&2
  exit 3
}

python3 - "$ARCHIVE" <<'PY'
import pathlib
import sys
import tarfile

with tarfile.open(sys.argv[1]) as archive:
    files = set()
    for item in archive.getmembers():
        path = pathlib.PurePosixPath(item.name)
        if path.is_absolute() or '..' in path.parts:
            raise SystemExit('unsafe archive path')
        if not (item.isfile() or item.isdir()):
            raise SystemExit('archive may contain only regular files and directories')
        normalized = pathlib.PurePosixPath(*[part for part in path.parts if part != '.'])
        if item.isfile():
            if not normalized.parts:
                raise SystemExit('invalid archive member')
            files.add(normalized.as_posix())
    if 'index.html' not in files:
        raise SystemExit('frontend archive is missing index.html')
PY

AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 134217728 )) || { echo 'insufficient free space; prior releases retained' >&2; exit 4; }

TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || { echo 'release already exists' >&2; exit 3; }
mkdir "$TARGET"
cp -a --reflink=auto "$EXPECTED/." "$TARGET/"

# The base release intentionally retains hashed files for browser rollback. For
# this release, remove inherited assets that contain the retired workbench so
# neither its route nor its lazy-loaded page is present in production assets.
for surface in web web-desktop; do
  ASSET_ROOT="$TARGET/$surface/assets"
  [[ -d "$ASSET_ROOT" ]] || { echo "missing $surface assets" >&2; exit 4; }
  find "$ASSET_ROOT" -maxdepth 1 -type f -name 'AnchorStyleWorkbenchView-*' -delete
  while IFS= read -r -d '' file; do
    if grep -aq \
      -e '主播口播调教台' \
      -e '/operations/live/style-workbench' \
      -e 'AnchorStyleWorkbenchView' \
      "$file"; then
      rm -- "$file"
    fi
  done < <(find "$ASSET_ROOT" -maxdepth 1 -type f -print0)
done

tar -xzf "$ARCHIVE" -C "$TARGET/web"
tar -xzf "$ARCHIVE" -C "$TARGET/web-desktop"

grep -q 'name="xiaolan-app" content="desktop"' "$TARGET/web/index.html"
cmp "$TARGET/web/index.html" "$TARGET/web-desktop/index.html"

for surface in web web-desktop; do
  if find "$TARGET/$surface/assets" -maxdepth 1 -type f -name 'AnchorStyleWorkbenchView-*' -print -quit | grep -q .; then
    echo "retired workbench chunk remains in $surface" >&2
    exit 4
  fi
  if grep -aRIl \
    -e '主播口播调教台' \
    -e '/operations/live/style-workbench' \
    -e 'AnchorStyleWorkbenchView' \
    "$TARGET/$surface" | grep -q .; then
    echo "retired workbench marker remains in $surface" >&2
    exit 4
  fi
done

cmp "$EXPECTED/bin/management-service" "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
diff -qr "$EXPECTED/configs" "$TARGET/configs"

python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" <<'PY'
import datetime
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
metadata = {
    'release_id': sys.argv[2],
    'base_release': sys.argv[3],
    'scope': 'desktop-web-remove-anchor-workbench',
    'service_restarted': False,
    'deployed_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
(root / 'VERSION.json').write_text(json.dumps(metadata, ensure_ascii=False), encoding='utf-8')
(root / 'VERSION').write_text(sys.argv[2] + '\n', encoding='ascii')
PY
chown -R ecs-user:ecs-user "$TARGET"

[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || {
  echo 'concurrent release detected; not activated' >&2
  exit 3
}
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"

if ! curl -fsS --max-time 10 https://www.xiaolandaizi.cn/ >/dev/null; then
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  echo 'public page check failed; previous release restored' >&2
  exit 5
fi

mkdir -p "$ROOT/deployments"
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=desktop-web-remove-anchor-workbench\nservice_restarted=false\n' \
  "$RELEASE_ID" "$EXPECTED" "$TARGET" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "web deploy ok: $TARGET (retired workbench absent; services not restarted)"
