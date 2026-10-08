#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:?management archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
SHA="${4:?archive sha256 required}"
SCOPE="${5:-management-media-download-proxy-only}"
ROOT=/opt/xiaolan

[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$SCOPE" =~ ^[A-Za-z0-9._-]+$ && "$SHA" =~ ^[a-f0-9]{64}$ && -f "$ARCHIVE" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || exit 2
if [[ "$(id -u)" -ne 0 ]]; then exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$SHA" "$SCOPE"; fi
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]] || exit 3
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'concurrent release changed; recheck before publishing' >&2; exit 3; }

python3 - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
    files=[]
    for item in archive.getmembers():
        path=pathlib.PurePosixPath(item.name)
        if path.is_absolute() or '..' in path.parts or item.issym() or item.islnk():
            raise SystemExit('unsafe archive member')
        if item.isfile(): files.append(str(path))
    if files != ['bin/management-service']:
        raise SystemExit('management-only archive required')
PY

AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 268435456 )) || { echo 'insufficient free space; old releases are not deleted' >&2; exit 4; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || exit 3
mkdir "$TARGET"
[[ "$(readlink -f "$TARGET")" == "$TARGET" ]] || exit 3

# Hard-link immutable files from the pinned production release. Only the new
# release's management binary and metadata links are unlinked and replaced.
cp -al "$EXPECTED/." "$TARGET/"
for FILE in bin/management-service VERSION VERSION.json; do
  [[ "$(readlink -f "$TARGET/$FILE")" == "$TARGET/$FILE" && -f "$TARGET/$FILE" ]] || exit 3
  rm -- "$TARGET/$FILE"
done
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/management-service"
chown ecs-user:ecs-user "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
cmp "$EXPECTED/web/index.html" "$TARGET/web/index.html"
diff -qr "$EXPECTED/configs" "$TARGET/configs"

python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" "$SCOPE" <<'PY'
import datetime,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
(root/'VERSION.json').write_text(json.dumps({
  'release_id':sys.argv[2],
  'base_release':sys.argv[3],
  'scope':sys.argv[4],
  'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),
},ensure_ascii=False),encoding='utf-8')
(root/'VERSION').write_text(sys.argv[2]+'\n',encoding='ascii')
PY
chown ecs-user:ecs-user "$TARGET" "$TARGET/VERSION" "$TARGET/VERSION.json"
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3

rollback() {
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  systemctl restart xiaolan-management.service || true
  echo "rolled back to $EXPECTED" >&2
}
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"
if ! systemctl restart xiaolan-management.service; then rollback; exit 5; fi
HEALTHY=false
for _ in $(seq 1 25); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then HEALTHY=true; break; fi
  sleep 1
done
if [[ "$HEALTHY" != true ]]; then rollback; exit 6; fi

mkdir -p "$ROOT/deployments"
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=%s\nsha256=%s\n' \
  "$RELEASE_ID" "$EXPECTED" "$TARGET" "$SCOPE" "$SHA" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "management deploy ok: $TARGET scope=$SCOPE (web/Core/Xiaozhi/configs unchanged)"
