#!/usr/bin/env bash
# Reviewed, bounded content-refresh rollout. This script never grants access,
# selects a mode, starts a room, restores SQL, or deletes production/test data.
set -Eeuo pipefail
umask 077

ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED_CURRENT="${3:?expected current required}"
ARCHIVE_SHA="${4:?archive SHA256 required}"
CORE_SHA="${5:?core binary SHA256 required}"
MANAGEMENT_SHA="${6:?management binary SHA256 required}"
DIRTY_MANIFEST="${7:?dirty source manifest JSON required}"
DIRTY_SHA="${8:?dirty manifest SHA256 required}"
# Expected observed mode, never a mode to select. Default preserves earlier
# recording-mode rollout callers; later style-only releases pin current state.
EXPECTED_ROOM15_MODE="${9:-user_audio}"
case "$EXPECTED_ROOM15_MODE" in user_audio|ai_pregenerated|ai_dynamic) ;; *) echo 'invalid expected room mode' >&2; exit 1 ;; esac
ROOT=/opt/xiaolan
TARGET="$ROOT/releases/$ID"
STAGE="$ROOT/deployments/$ID-candidate"
BACKUP="$ROOT/deployments/$ID-backup"
PUBLIC_BASE=https://www.xiaolandaizi.cn

fail() { echo "ABORT: $*" >&2; return 1; }
[[ "$(id -u)" == 0 ]] || fail 'root required'
[[ "$ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "$ID" != *..* ]] || fail 'invalid release id'
[[ "$EXPECTED_CURRENT" =~ ^/opt/xiaolan/releases/[A-Za-z0-9][A-Za-z0-9._-]*$ && "$EXPECTED_CURRENT" != *..* ]] || fail 'invalid expected-current path'
[[ "$ARCHIVE" == "/tmp/$ID.tar.gz" && -f "$ARCHIVE" && ! -L "$ARCHIVE" ]] || fail 'invalid archive path'
[[ "$DIRTY_MANIFEST" == "/tmp/$ID-dirty-manifest.json" && -f "$DIRTY_MANIFEST" && ! -L "$DIRTY_MANIFEST" ]] || fail 'invalid dirty manifest path'
for digest in "$ARCHIVE_SHA" "$CORE_SHA" "$MANAGEMENT_SHA" "$DIRTY_SHA"; do
  [[ "$digest" =~ ^[a-fA-F0-9]{64}$ ]] || fail 'invalid SHA256'
done
for tool in python3 tar sha256sum mysql mysqldump gzip curl flock systemctl journalctl timeout; do
  command -v "$tool" >/dev/null || fail "missing required tool: $tool"
done
mkdir -p "$ROOT/deployments"
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another release operation holds the global lock'
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || fail 'current release changed'
for path in "$TARGET" "$STAGE" "$BACKUP" "$ROOT/.content-refresh-current-$ID" "$ROOT/.content-refresh-rollback-$ID"; do
  [[ ! -e "$path" && ! -L "$path" ]] || fail "release path already exists: $path"
done
[[ -d "$EXPECTED_CURRENT" && ! -L "$EXPECTED_CURRENT" ]] || fail 'base release must be a real directory'
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
printf '%s  %s\n' "$DIRTY_SHA" "$DIRTY_MANIFEST" | sha256sum -c -

# Reject traversal, duplicate normalized names, symlinks, hardlinks, devices,
# unexpected trees, and oversized/unbounded input before extraction.
python3 -B - "$ARCHIVE" "$DIRTY_MANIFEST" <<'PY'
import json, re, sys, tarfile
from pathlib import PurePosixPath
manifest = json.load(open(sys.argv[2], encoding='utf-8-sig'))
if not isinstance(manifest, dict) or manifest.get('dirty') is not True or not re.fullmatch(r'[a-fA-F0-9]{40,64}', str(manifest.get('git_head', ''))):
    raise SystemExit('dirty source manifest requires git_head and dirty:true')
if not isinstance(manifest.get('files'), list) or not manifest['files']:
    raise SystemExit('dirty source manifest must contain the full reviewed file inventory')
for item in manifest['files']:
    if not isinstance(item, dict) or not isinstance(item.get('path'), str) or not isinstance(item.get('status'), str):
        raise SystemExit('dirty manifest file requires path/status')
    path = PurePosixPath(item['path'].replace('\\', '/'))
    if path.is_absolute() or '..' in path.parts:
        raise SystemExit('unsafe manifest path')
with tarfile.open(sys.argv[1], 'r:gz') as archive:
    seen, size, count = set(), 0, 0
    for entry in archive:
        raw = entry.name.rstrip('/')
        path = PurePosixPath(raw)
        if not raw or '\\' in raw or '\n' in raw or '\r' in raw or path.is_absolute() or '..' in path.parts or str(path) != raw:
            raise SystemExit('unsafe release entry')
        if not (entry.isfile() or entry.isdir()) or entry.mode & 0o6000:
            raise SystemExit('links/special files/setid permissions are forbidden')
        allowed = raw in ('bin', 'bin/core-service', 'bin/management-service') or path.parts[0] in ('web', 'web-desktop')
        if not allowed or raw in seen or (raw == 'bin' and not entry.isdir()):
            raise SystemExit('duplicate/out-of-scope release entry')
        seen.add(raw)
        size += entry.size
        count += 1
        if size > 2 * 1024**3 or count > 50000:
            raise SystemExit('release archive exceeds bounded size/count')
    required = {'bin/core-service', 'bin/management-service', 'web/index.html', 'web-desktop/index.html'}
    if not required <= seen:
        raise SystemExit('incomplete content-refresh release')
PY

mkdir -m 700 "$BACKUP" "$STAGE"
cp -a "$DIRTY_MANIFEST" "$BACKUP/dirty-manifest.json"
cp -a /etc/xiaolan/management.env /etc/xiaolan/core.env /etc/xiaolan/xiaozhi.env "$BACKUP/"
find /etc/xiaolan -maxdepth 1 -type f -name '*.env' -print0 | sort -z | xargs -0 sha256sum > "$BACKUP/environment.sha256"
PRESERVED_DEVICE_PID="$(systemctl show -p MainPID --value xiaolan-xiaozhi.service)"
[[ "$PRESERVED_DEVICE_PID" =~ ^[1-9][0-9]*$ ]] || fail 'device gateway is not running'
systemctl is-active --quiet xiaolan-core.service xiaolan-management.service xiaolan-xiaozhi.service

set -a
source /etc/xiaolan/management.env
set +a
: "${DB_HOST:?}" "${DB_USER:?}" "${DB_NAME:?}" "${DB_PASSWORD:?}" "${CORE_INTERNAL_TOKEN:?}"
export MYSQL_PWD="$DB_PASSWORD"
MYSQL=(mysql --connect-timeout=5 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names --default-character-set=utf8mb4)

verify_access_and_defaults() {
  [[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM core_rooms r JOIN live_room_content_access a ON a.tenant_id=r.tenant_id AND a.room_id=r.id WHERE r.id=15 AND r.tenant_id=14 AND r.name='菜籽油' AND a.selected_mode='$EXPECTED_ROOM15_MODE' AND a.dynamic_authorized=1 AND r.monitor_enabled=0")" == 1 ]] || { fail 'room15 identity/mode/grant/monitor guard failed; do not modify automatically'; return 1; }
  [[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_content_policies WHERE tenant_id=0 AND room_id=0 AND JSON_EXTRACT(policy_json,'$.mainline_ttl_seconds')=7200 AND JSON_EXTRACT(policy_json,'$.faq_ttl_seconds')=7200 AND JSON_EXTRACT(policy_json,'$.replacement_percent')=25")" == 1 ]] || { fail 'expected 2h/25% defaults changed'; return 1; }
}

assert_no_active_work() {
  [[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming')")" == 0 ]] || { fail 'live runtime session is active; no Core restart allowed'; return 1; }
  [[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM core_rooms WHERE monitor_enabled=1 OR LOWER(status) NOT IN ('pending','stopped','offline','idle','failed','error','disabled','')")" == 0 ]] || { fail 'Core room is active/starting/unknown; no restart allowed'; return 1; }
  python3 -B - <<'PY'
import json, os, urllib.request
idle = {'', 'idle', 'completed', 'failed'}
urls = [u.strip().rstrip('/') for u in (os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(',') if u.strip()]
if not urls:
    raise SystemExit('no Core nodes configured')
def get(base, path):
    request = urllib.request.Request(base + path, headers={'X-Core-Token': os.environ['CORE_INTERNAL_TOKEN']})
    with urllib.request.urlopen(request, timeout=8) as response:
        return json.load(response)
for base in urls:
    data = get(base, '/internal/v1/rooms')
    if not isinstance(data.get('items'), list):
        raise SystemExit('invalid Core room inventory; fail closed')
    for room in data['items']:
        if room.get('monitor_enabled') or str(room.get('status', '')).lower() not in {'', 'pending','stopped','offline','idle','failed','error','disabled'}:
            raise SystemExit('Core has active/starting room; no restart allowed')
        rid, tid = int(room['id']), int(room['tenant_id'])
        runtime = get(base, f'/internal/v1/rooms/{rid}/speech-runtime?tenant_id={tid}')
        program = runtime.get('program') or {}
        if program.get('running') or program.get('suspended'):
            raise SystemExit('Core has running/paused audio program; no restart allowed')
        if any(str((runtime.get(track) or {}).get('status', '')).lower() not in idle for track in ('mainline', 'interrupt')):
            raise SystemExit('Core has queued/playing speech; no restart allowed')
        engine = get(base, f'/internal/v1/rooms/{rid}/audio-engine?tenant_id={tid}')
        if str(engine.get('phase', '')).lower() not in {'idle', 'error'}:
            raise SystemExit('Core audio engine is active/paused; no restart allowed')
print('Read-only idle gate passed for all Core rooms/nodes')
PY
}

verify_access_and_defaults
assert_no_active_work
"${MYSQL[@]}" -e "SELECT COALESCE(MAX(id),0),COUNT(*) FROM live_runtime_sessions WHERE tenant_id=14 AND room_id=15" > "$BACKUP/room15-sessions-before.tsv"
"${MYSQL[@]}" -e "SELECT tenant_id,room_id,selected_mode,dynamic_authorized,authorization_source FROM live_room_content_access WHERE tenant_id=14 AND room_id=15" > "$BACKUP/room15-access-before.tsv"
echo 'Backing up MySQL; backup is retained, never automatically restored'
timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 --hex-blob -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
gzip -t "$BACKUP/database-before.sql.gz"
[[ -s "$BACKUP/database-before.sql.gz" ]]

mkdir -m 755 "$TARGET"
cp -a "$EXPECTED_CURRENT/." "$TARGET/"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner --no-same-permissions --delay-directory-restore
printf '%s  %s\n' "$CORE_SHA" "$STAGE/bin/core-service" | sha256sum -c -
printf '%s  %s\n' "$MANAGEMENT_SHA" "$STAGE/bin/management-service" | sha256sum -c -
for surface in web web-desktop; do
  [[ -d "$TARGET/$surface" && ! -L "$TARGET/$surface" ]] || fail 'existing web surface must not be a symlink'
  grep -q 'name="xiaolan-app" content="desktop"' "$STAGE/$surface/index.html"
done
cmp "$STAGE/web/index.html" "$STAGE/web-desktop/index.html"
[[ -d "$TARGET/bin" && ! -L "$TARGET/bin" && ! -L "$TARGET/bin/core-service" && ! -L "$TARGET/bin/management-service" ]] || fail 'existing binary target must not be a symlink'
python3 -B - "$TARGET" <<'PY'
import sys
from pathlib import Path
for surface in ('web', 'web-desktop'):
    if any(path.is_symlink() for path in (Path(sys.argv[1])/surface).rglob('*')):
        raise SystemExit('mutable web tree contains a symlink; refusing overlay')
PY

# Keep old hashed assets for open browser sessions; only these two web trees and
# the two requested binaries may differ from the base release.
cp -a "$STAGE/web/." "$TARGET/web/"
cp -a "$STAGE/web-desktop/." "$TARGET/web-desktop/"
install -m 755 "$STAGE/bin/core-service" "$TARGET/bin/core-service"
install -m 755 "$STAGE/bin/management-service" "$TARGET/bin/management-service"
chown -R ecs-user:ecs-user "$TARGET/web" "$TARGET/web-desktop"
find "$TARGET/web" "$TARGET/web-desktop" -type d -exec chmod 755 '{}' +
find "$TARGET/web" "$TARGET/web-desktop" -type f -exec chmod 644 '{}' +
chown ecs-user:ecs-user "$TARGET/bin/core-service" "$TARGET/bin/management-service"
python3 -B - "$EXPECTED_CURRENT" "$TARGET" "$ID" "$DIRTY_SHA" "$ARCHIVE_SHA" "$BACKUP" <<'PY'
import hashlib, json, os, sys
from datetime import datetime, timezone
from pathlib import Path
base, target = Path(sys.argv[1]), Path(sys.argv[2])
def digest_file(path):
    digest = hashlib.sha256()
    with path.open('rb') as handle:
        for chunk in iter(lambda: handle.read(1024*1024), b''):
            digest.update(chunk)
    return digest.hexdigest()
def preserved(root):
    records = {}
    for path in root.rglob('*'):
        rel = path.relative_to(root).as_posix()
        if rel.split('/')[0] in ('web','web-desktop') or rel in ('bin/core-service','bin/management-service','VERSION','VERSION.json'):
            continue
        if path.is_symlink(): records[rel] = ['link', os.readlink(path)]
        elif path.is_file(): records[rel] = ['file', digest_file(path)]
        elif path.is_dir(): records[rel] = ['dir']
        else: raise SystemExit('unexpected special file in base release')
    return records
if preserved(base) != preserved(target):
    raise SystemExit('non-target release content changed')
Path(sys.argv[6], 'preserved-release-files.json').write_text(json.dumps(preserved(base),ensure_ascii=False,sort_keys=True), encoding='utf-8')
(target/'VERSION').write_text(sys.argv[3]+'\n', encoding='utf-8')
(target/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[3], 'base_release':base.name, 'scope':'content-refresh', 'dirty':True, 'dirty_manifest_sha256':sys.argv[4], 'archive_sha256':sys.argv[5], 'dirty_manifest_backup':str(Path(sys.argv[6], 'dirty-manifest.json')), 'replaced':['bin/core-service','bin/management-service','web','web-desktop'], 'preserved':['collector-worker','xiaozhi','web-customer','web-sales','environment'], 'deployed_at_utc':datetime.now(timezone.utc).isoformat()},ensure_ascii=False,indent=2), encoding='utf-8')
PY
chmod 644 "$TARGET/VERSION" "$TARGET/VERSION.json"
chown ecs-user:ecs-user "$TARGET/VERSION" "$TARGET/VERSION.json"

ACTIVATED=0
MANAGEMENT_STOPPED=0
CORE_RESTART_ATTEMPTED=0
health_wait() {
  local port="$1"
  for _ in $(seq 1 90); do
    if curl -fsS --max-time 2 "http://127.0.0.1:$port/healthz" >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}
rollback() {
  local failed_status="$?"
  [[ "$failed_status" != 0 ]] || failed_status=1
  trap - ERR INT TERM
  set +e
  if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    systemctl stop xiaolan-management.service
    MANAGEMENT_STOPPED=1
    # Never interrupt a live room created during the verification window.
    # If Core is unhealthy, the database gate still prevents a blind restart.
    local sessions
    sessions="$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming')" 2>/dev/null)"
    local safe_to_restart=1
    if [[ "$sessions" != 0 ]]; then safe_to_restart=0; fi
    if curl -fsS --max-time 2 http://127.0.0.1:8081/healthz >/dev/null 2>&1; then
      if ! assert_no_active_work; then safe_to_restart=0; fi
    else
      [[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM core_rooms WHERE monitor_enabled=1 OR LOWER(status) NOT IN ('pending','stopped','offline','idle','failed','error','disabled','')" 2>/dev/null)" == 0 ]] || safe_to_restart=0
    fi
    if [[ "$safe_to_restart" != 1 ]]; then
      systemctl start xiaolan-management.service
      echo "Rollback halted: active/unknown runtime. Current release retained; manual intervention required. Backup=$BACKUP" >&2
      exit 7
    fi
    ln -s "$EXPECTED_CURRENT" "$ROOT/.content-refresh-rollback-$ID"
    mv -Tf "$ROOT/.content-refresh-rollback-$ID" "$ROOT/current"
    if [[ "$CORE_RESTART_ATTEMPTED" == 1 ]]; then systemctl restart xiaolan-core.service; health_wait 8081; fi
    systemctl start xiaolan-management.service
    health_wait 8080
    echo "ROLLED_BACK_TO=$EXPECTED_CURRENT; database migrations retained; no SQL restore performed" >&2
  elif [[ "$MANAGEMENT_STOPPED" == 1 ]]; then
    systemctl start xiaolan-management.service
  fi
  echo "Content-refresh deployment failed. Candidate and backup retained: $STAGE $BACKUP" >&2
  exit "$failed_status"
}
trap rollback ERR INT TERM

[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || fail 'current changed before activation'
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
verify_access_and_defaults
assert_no_active_work
# Quiesce management only after all idle gates pass, then recheck in-flight
# starts before touching Core. Devices/collector are not restarted or reconfigured.
MANAGEMENT_STOPPED=1
systemctl stop xiaolan-management.service
sleep 2
assert_no_active_work
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED_CURRENT" ]] || fail 'concurrent release detected'
ln -s "$TARGET" "$ROOT/.content-refresh-current-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.content-refresh-current-$ID" "$ROOT/current"
DEPLOY_STARTED_AT="$(date '+%Y-%m-%d %H:%M:%S')"
CORE_RESTART_ATTEMPTED=1
systemctl restart xiaolan-core.service
health_wait 8081
systemctl start xiaolan-management.service
MANAGEMENT_STOPPED=0
health_wait 8080
health_wait 8083

probe_denied() {
  local method="$1" endpoint="$2" code
  local -a request=(-sS --max-time 10 -o /dev/null -w '%{http_code}' -X "$method" -H 'Content-Type: application/json')
  if [[ "$method" != GET ]]; then request+=(--data-binary '{}'); fi
  code="$(curl "${request[@]}" "$endpoint")"
  [[ "$code" == 401 || "$code" == 403 ]] || fail "unauthorized guard failed: $method $endpoint ($code)"
  printf 'Unauthorized guard %s %s => %s\n' "$method" "$endpoint" "$code"
}
for path in /api/v1/system/live-content-policy /api/v1/liveops/rooms/15/content-policy /api/v1/live/rooms/15/content-mode; do
  probe_denied GET "http://127.0.0.1:8080$path"
  probe_denied PUT "http://127.0.0.1:8080$path"
done
probe_denied POST 'http://127.0.0.1:8080/api/v1/live-agent-plans/2/anchor-style/test'
probe_denied POST 'http://127.0.0.1:8080/api/v1/live-agent-plans/2/scripts/1/analysis-confirm'
probe_denied GET 'http://127.0.0.1:8081/internal/v1/rooms/15/audio/program?tenant_id=14'
for action in refresh refresh/renew refresh/cancel; do
  probe_denied POST "http://127.0.0.1:8081/internal/v1/rooms/15/audio/program/$action?tenant_id=14"
done
journalctl -u xiaolan-management.service --since "$DEPLOY_STARTED_AT" --no-pager -o cat > "$BACKUP/management-startup.log"
grep -q 'content refresh scheduler started realm=' "$BACKUP/management-startup.log" || fail 'content refresh scheduler startup not confirmed'
verify_access_and_defaults
assert_no_active_work
"${MYSQL[@]}" -e "SELECT COALESCE(MAX(id),0),COUNT(*) FROM live_runtime_sessions WHERE tenant_id=14 AND room_id=15" > "$BACKUP/room15-sessions-after.tsv"
"${MYSQL[@]}" -e "SELECT tenant_id,room_id,selected_mode,dynamic_authorized,authorization_source FROM live_room_content_access WHERE tenant_id=14 AND room_id=15" > "$BACKUP/room15-access-after.tsv"
cmp "$BACKUP/room15-sessions-before.tsv" "$BACKUP/room15-sessions-after.tsv"
cmp "$BACKUP/room15-access-before.tsv" "$BACKUP/room15-access-after.tsv"
[[ "$("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_content_refresh_jobs WHERE tenant_id=14 AND room_id=15 AND status IN ('pending','running','retry','ready')")" == 0 ]] || fail 'stopped room unexpectedly has automatic refresh work'
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' "$PUBLIC_BASE/" | cmp - "$TARGET/web-desktop/index.html"
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
DEVICE_FOLLOWED_CORE_RESTART=false
if [[ "$(systemctl show -p MainPID --value xiaolan-xiaozhi.service)" != "$PRESERVED_DEVICE_PID" ]]; then
  # The installed gateway Requires Core; Core restart propagates a dependency
  # stop/start. Its binary, bindings and environment are preserved, not upgraded.
  [[ " $(systemctl show -p Requires --value xiaolan-xiaozhi.service) " == *' xiaolan-core.service '* ]] || fail 'gateway restarted without the expected Core dependency'
  systemctl is-active --quiet xiaolan-xiaozhi.service
  health_wait 8083
  DEVICE_FOLLOWED_CORE_RESTART=true
  echo 'Gateway followed existing Requires=Core dependency; binary/configuration unchanged'
fi
systemctl is-active --quiet xiaolan-core.service xiaolan-management.service xiaolan-xiaozhi.service
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nbackup=%s\nscope=core-management-desktop-content-refresh-and-style-recovery\ndirty_manifest_sha256=%s\nroom15_mode=%s\nroom15_dynamic_grant=1\nroom15_started=false\nroom15_mode_changed=false\ncore_and_management_restarted=true\ngateway_followed_core_dependency=%s\nother_surfaces_and_environment_preserved=true\n' "$ID" "$EXPECTED_CURRENT" "$TARGET" "$BACKUP" "$DIRTY_SHA" "$EXPECTED_ROOM15_MODE" "$DEVICE_FOLLOWED_CORE_RESTART" > "$ROOT/deployments/$ID.txt"
echo "DEPLOYED=$ID; NO_LIVE_ROOM_STARTED; ROOM15_MODE_AND_GRANT_PRESERVED; BACKUP=$BACKUP"
