#!/usr/bin/env bash
set -Eeuo pipefail
umask 077
[[ "$#" == 4 ]] || exit 2
ARCHIVE="$1"
RELEASE_ID="$2"
EXPECTED="$3"
SHA="$4"
ROOT=/opt/xiaolan
SCOPE=room-status-authority-and-voice-model-table
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "$SHA" =~ ^[a-f0-9]{64}$ && -f "$ARCHIVE" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" && ! -L "$EXPECTED" ]] || exit 2
if [[ "$(id -u)" -ne 0 ]]; then exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$SHA"; fi
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || { echo 'another deployment holds lock' >&2; exit 3; }
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]] || exit 3
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'current release changed' >&2; exit 3; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" && ! -L "$TARGET" ]] || exit 3
python3 -B - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
    files=[]
    for item in archive.getmembers():
        path=pathlib.PurePosixPath(item.name)
        if path.is_absolute() or '..' in path.parts or item.issym() or item.islnk():
            raise SystemExit('unsafe archive member')
        if not item.isfile() and not item.isdir():
            raise SystemExit('nonregular archive member')
        if item.isfile(): files.append(str(path))
    if sorted(files)!=['bin/core-service','bin/management-service']:
        raise SystemExit('Core and Management binaries only required')
PY
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
assert_idle() {
  local count
  count=$(mysql --connect-timeout=5 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','resuming')")
  [[ "$count" == 0 ]] || { echo 'active or paused session: restart refused' >&2; return 1; }
  python3 -B - <<'PY'
import json,os,urllib.request
bases=[b.strip().rstrip('/') for b in (os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(',') if b.strip()]
if len(bases)!=1: raise SystemExit('this overlay requires a separately verified single Core service')
base=bases[0]
headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']}
def get(path):
    with urllib.request.urlopen(urllib.request.Request(base+path,headers=headers),timeout=8) as response:
        return json.load(response)
rooms=get('/internal/v1/rooms').get('items')
if not isinstance(rooms,list): raise SystemExit('invalid room inventory')
for room in rooms:
    room_id=int(room['id']); query=f"?tenant_id={int(room['tenant_id'])}"
    speech=get(f'/internal/v1/rooms/{room_id}/speech-runtime'+query)
    engine=get(f'/internal/v1/rooms/{room_id}/audio-engine'+query)
    agent=get(f'/internal/v1/rooms/{room_id}/agent-runtime'+query)
    if agent.get('state')!='stopped' or (speech.get('program') or {}).get('running') or (speech.get('interrupt') or {}).get('status') in ('ready','playing','returning') or str(engine.get('phase') or '').lower() not in ('','idle') or int(engine.get('subscribers') or 0) or room.get('device_online'):
        raise SystemExit(f'room {room_id} has active Agent/audio/device state: restart refused')
PY
}
assert_idle
AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 268435456 )) || { echo 'insufficient free space' >&2; exit 4; }
mkdir -m 700 "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
for FILE in bin/core-service bin/management-service VERSION VERSION.json; do
  [[ -f "$TARGET/$FILE" && ! -L "$TARGET/$FILE" && "$(readlink -f "$TARGET/$FILE")" == "$TARGET/$FILE" ]] || exit 3
  rm -- "$TARGET/$FILE"
done
tar --no-same-owner -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/core-service" "$TARGET/bin/management-service"
chown ecs-user:ecs-user "$TARGET/bin/core-service" "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
cmp "$EXPECTED/web/index.html" "$TARGET/web/index.html"
diff -qr "$EXPECTED/configs" "$TARGET/configs"
python3 -B - "$TARGET" "$RELEASE_ID" "$EXPECTED" "$SHA" "$SCOPE" <<'PY'
import datetime,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
(root/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[2],'base_release':sys.argv[3],'scope':sys.argv[5],'archive_sha256':sys.argv[4],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}),encoding='utf-8')
(root/'VERSION').write_text(sys.argv[2]+'\n',encoding='ascii')
PY
chmod 0644 "$TARGET/VERSION" "$TARGET/VERSION.json"
chown ecs-user:ecs-user "$TARGET" "$TARGET/VERSION" "$TARGET/VERSION.json"
health_wait() {
  for _ in $(seq 1 25); do
    if curl -fsS --max-time 2 "http://127.0.0.1:$1/healthz" >/dev/null; then return 0; fi
    sleep 1
  done
  return 1
}
ACTIVATED=0
rollback() {
  code=$?
  trap - ERR INT TERM
  set +e
  if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    ln -s "$EXPECTED" "$ROOT/.room-authority-rollback-$RELEASE_ID"
    mv -Tf "$ROOT/.room-authority-rollback-$RELEASE_ID" "$ROOT/current"
    systemctl restart xiaolan-core xiaolan-management xiaolan-xiaozhi
    echo "rolled back to $EXPECTED" >&2
  fi
  exit "$code"
}
trap rollback ERR INT TERM
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3
assert_idle
ln -s "$TARGET" "$ROOT/.room-authority-next-$RELEASE_ID"
ACTIVATED=1
mv -Tf "$ROOT/.room-authority-next-$RELEASE_ID" "$ROOT/current"
systemctl restart xiaolan-core
systemctl restart xiaolan-management
systemctl restart xiaolan-xiaozhi
health_wait 8081
health_wait 8080
health_wait 8083
systemctl is-active --quiet xiaolan-core xiaolan-management xiaolan-xiaozhi
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=%s\nsha256=%s\n' "$RELEASE_ID" "$EXPECTED" "$TARGET" "$SCOPE" "$SHA" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "deploy ok: $RELEASE_ID"
