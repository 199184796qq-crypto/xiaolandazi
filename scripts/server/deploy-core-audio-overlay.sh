#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

ARCHIVE="${1:?core archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
SHA="${4:?archive sha256 required}"
ROOT=/opt/xiaolan
TARGET="$ROOT/releases/$RELEASE_ID"
BACKUP="$ROOT/deployments/$RELEASE_ID-backup"

fail() { echo "ABORT: $*" >&2; return 1; }
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "$SHA" =~ ^[a-f0-9]{64}$ && -f "$ARCHIVE" ]] || fail 'invalid inputs'
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" && ! -L "$EXPECTED" ]] || fail 'invalid expected release'
if [[ "$(id -u)" -ne 0 ]]; then exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$SHA"; fi
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another deployment holds lock'
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]] || fail 'archive digest mismatch'
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || fail 'current release changed'
[[ ! -e "$TARGET" && ! -L "$TARGET" && ! -e "$BACKUP" ]] || fail 'target already exists'

python3 -B - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
    files=[]
    for item in archive.getmembers():
        path=pathlib.PurePosixPath(item.name)
        if path.is_absolute() or '..' in path.parts or item.issym() or item.islnk():
            raise SystemExit('unsafe archive member')
        if item.isfile(): files.append(str(path))
    if files != ['bin/core-service']:
        raise SystemExit('core-only archive required')
PY

set -a
source /etc/xiaolan/management.env
set +a
: "${DB_HOST:?}" "${DB_USER:?}" "${DB_NAME:?}" "${DB_PASSWORD:?}" "${CORE_INTERNAL_TOKEN:?}"
export MYSQL_PWD="$DB_PASSWORD"
MYSQL=(mysql --connect-timeout=5 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names --default-character-set=utf8mb4)
[[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming')")" == 0 ]] || fail 'live runtime session is active'

python3 -B - <<'PY'
import json,os,urllib.request
base=(os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(',')[0].strip().rstrip('/')
headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']}
def get(path):
    with urllib.request.urlopen(urllib.request.Request(base+path,headers=headers),timeout=8) as response:
        return json.load(response)
rooms=get('/internal/v1/rooms').get('items')
if not isinstance(rooms,list): raise SystemExit('invalid Core room inventory')
for room in rooms:
    room_id=int(room['id']); tenant_id=int(room['tenant_id']); query=f'?tenant_id={tenant_id}'
    speech=get(f'/internal/v1/rooms/{room_id}/speech-runtime'+query)
    engine=get(f'/internal/v1/rooms/{room_id}/audio-engine'+query)
    program=speech.get('program') or {}
    if program.get('running') or str(engine.get('phase') or '').lower() not in ('','idle') or int(engine.get('subscribers') or 0) != 0 or room.get('device_online'):
        raise SystemExit(f'room {room_id} has active audio/device state; Core restart refused')
    print(f"SAFE_ROOM={room_id}:{room.get('status')} monitor={room.get('monitor_enabled')} audio=idle device=offline")
PY

AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 268435456 )) || fail 'insufficient free space'
mkdir -m 700 "$TARGET" "$BACKUP"
systemctl is-active --quiet xiaolan-core xiaolan-management xiaolan-xiaozhi
systemctl show -p MainPID xiaolan-management > "$BACKUP/preserved-pids.txt"
find /etc/xiaolan -maxdepth 1 -type f -name '*.env' -print0 | sort -z | xargs -0 sha256sum > "$BACKUP/environment.sha256"

cp -al "$EXPECTED/." "$TARGET/"
for FILE in bin/core-service VERSION VERSION.json; do
  [[ -f "$TARGET/$FILE" && ! -L "$TARGET/$FILE" ]] || fail "unsafe target $FILE"
  rm -- "$TARGET/$FILE"
done
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/core-service"
chown ecs-user:ecs-user "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/management-service" "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
cmp "$EXPECTED/web/index.html" "$TARGET/web/index.html"
diff -qr "$EXPECTED/configs" "$TARGET/configs"
python3 -B - "$TARGET" "$RELEASE_ID" "$EXPECTED" "$SHA" <<'PY'
import datetime,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
(root/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[2],'base_release':sys.argv[3],'scope':'core-audio-edge-guards','archive_sha256':sys.argv[4],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},ensure_ascii=False),encoding='utf-8')
(root/'VERSION').write_text(sys.argv[2]+'\n',encoding='ascii')
PY
chmod 0644 "$TARGET/VERSION" "$TARGET/VERSION.json"
chown ecs-user:ecs-user "$TARGET" "$TARGET/VERSION" "$TARGET/VERSION.json"

health_wait() {
  for _ in $(seq 1 45); do
    if curl -fsS --max-time 2 http://127.0.0.1:8081/healthz >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}
ACTIVATED=0
rollback() {
  code=$?
  trap - ERR INT TERM
  set +e
  if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    ln -s "$EXPECTED" "$ROOT/.core-audio-rollback-$RELEASE_ID"
    mv -Tf "$ROOT/.core-audio-rollback-$RELEASE_ID" "$ROOT/current"
    systemctl restart xiaolan-core
    health_wait
    echo "ROLLED_BACK=$EXPECTED" >&2
  fi
  exit "$code"
}
trap rollback ERR INT TERM

[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || fail 'concurrent release detected'
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
systemctl show -p MainPID xiaolan-management | cmp - "$BACKUP/preserved-pids.txt"
ln -s "$TARGET" "$ROOT/.core-audio-next-$RELEASE_ID"
ACTIVATED=1
mv -Tf "$ROOT/.core-audio-next-$RELEASE_ID" "$ROOT/current"
systemctl restart xiaolan-core
health_wait
systemctl is-active --quiet xiaolan-core xiaolan-management xiaolan-xiaozhi
systemctl show -p MainPID xiaolan-management | cmp - "$BACKUP/preserved-pids.txt"
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\nscope=core-audio-edge-guards\nmanagement_pid_preserved=true\nxiaozhi_dependency_restart=expected\n' "$RELEASE_ID" "$EXPECTED" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "DEPLOYED=$RELEASE_ID; Core health=200; management PID and environment unchanged; Xiaozhi dependency active"
