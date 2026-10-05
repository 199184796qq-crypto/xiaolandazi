#!/usr/bin/env bash
# Model discovery: scoped management/desktop rollout; preserve models and live state.
set -Eeuo pipefail
umask 077
ID=${1:?release id}
EXPECTED=${2:?base release}
ARCHIVE_SHA=${3:?archive digest}
BINARY_SHA=${4:?binary digest}
MANIFEST_SHA=${5:?source manifest digest}
EXPECTED_JS_SHA=${6:?base desktop JS digest}
EXPECTED_HTML_SHA=${7:?base desktop HTML digest}
ROOT=/opt/xiaolan
ARCHIVE=/tmp/$ID.tar.gz
MANIFEST=/tmp/$ID-dirty-manifest.json
TARGET=$ROOT/releases/$ID
STAGE=$ROOT/deployments/$ID-candidate
BACKUP=$ROOT/deployments/$ID-backup
fail() { echo "ABORT: $*" >&2; return 1; }
[[ $(id -u) == 0 ]] || fail 'root required'
[[ $ID =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && $ID != *..* ]] || fail 'invalid id'
[[ $EXPECTED =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ && $EXPECTED != *..* ]] || fail 'invalid base'
for hash in "$ARCHIVE_SHA" "$BINARY_SHA" "$MANIFEST_SHA" "$EXPECTED_JS_SHA" "$EXPECTED_HTML_SHA"; do
  [[ $hash =~ ^[a-f0-9]{64}$ ]] || fail 'invalid digest'
done
for path in "$TARGET" "$STAGE" "$BACKUP" "$ROOT/.speech-next-$ID" "$ROOT/.speech-rollback-$ID"; do
  [[ ! -e $path && ! -L $path ]] || fail "existing target: $path"
done
[[ -f $ARCHIVE && ! -L $ARCHIVE && -f $MANIFEST && ! -L $MANIFEST ]] || fail 'missing package'
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another release holds lock'
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
printf '%s  %s\n' "$MANIFEST_SHA" "$MANIFEST" | sha256sum -c -
python3 -B - "$ARCHIVE" "$MANIFEST" <<'PY'
import json, sys, tarfile
from pathlib import PurePosixPath
m=json.load(open(sys.argv[2],encoding='utf-8-sig'))
assert m['dirty'] is True and m['files'] and m['git_head']
with tarfile.open(sys.argv[1],'r:gz') as archive:
    seen=set(); size=0
    for e in archive:
        n=e.name.rstrip('/'); p=PurePosixPath(n)
        assert n and '\\' not in n and '\n' not in n and '\r' not in n
        assert not p.is_absolute() and '..' not in p.parts and str(p)==n
        assert e.isfile() or e.isdir()
        assert not e.mode & 0o6000 and n not in seen
        assert n in ('bin','bin/management-service') or p.parts[0] in ('web','web-desktop')
        seen.add(n); size+=e.size
        assert size<1024**3 and len(seen)<50000
    assert {'bin/management-service','web/index.html','web-desktop/index.html'}<=seen
PY
mkdir -m 700 "$BACKUP" "$STAGE"
cp -a "$MANIFEST" "$BACKUP/dirty-manifest.json"
cp -a /etc/xiaolan/management.env "$BACKUP/management.env"
find /etc/xiaolan -maxdepth 1 -type f -name '*.env' ! -name management.env -print0 | sort -z | xargs -0 sha256sum > "$BACKUP/other-env.sha256"
CORE_PID=$(systemctl show -p MainPID --value xiaolan-core)
DEVICE_PID=$(systemctl show -p MainPID --value xiaolan-xiaozhi)
systemctl is-active --quiet xiaolan-core xiaolan-management xiaolan-xiaozhi
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
MYSQL=(mysql --connect-timeout=5 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names --default-character-set=utf8mb4)
idle_gate() {
  [[ $("${MYSQL[@]}" -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming')") == 0 ]] || { fail 'live sessions active'; return 1; }
  # A listener-only room can remain live: Core/collector are not restarted.
  # Still refuse running sessions or queued/playing speech during management swap.
  python3 -B - <<'PY'
import json,os,urllib.request
def get(b,p):
    r=urllib.request.Request(b+p,headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']})
    with urllib.request.urlopen(r,timeout=8) as v: return json.load(v)
bases=(os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(',')
for b in bases:
    b=b.strip().rstrip('/'); rooms=get(b,'/internal/v1/rooms')['items']
    for r in rooms:
        p=f"/internal/v1/rooms/{int(r['id'])}"
        q=f"?tenant_id={int(r['tenant_id'])}"
        s=get(b,p+'/speech-runtime'+q); program=s.get('program') or {}
        assert not program.get('running') and not program.get('suspended')
        assert all(str((s.get(t) or {}).get('status','')).lower() in {'','idle','completed','failed'} for t in ('mainline','interrupt'))
        assert str(get(b,p+'/audio-engine'+q).get('phase','')).lower() in {'idle','error'}
print('No running sessions/audio; listener-only rooms preserved; Core not restarted')
PY
}
snapshot() {
  "${MYSQL[@]}" -e "SELECT id,tenant_id,name,status,monitor_enabled FROM core_rooms ORDER BY id; SELECT * FROM live_room_content_access ORDER BY tenant_id,room_id; SELECT * FROM live_content_policies ORDER BY tenant_id,room_id; SELECT * FROM live_agent_plan_room_bindings ORDER BY id; SELECT * FROM live_agent_room_plan_selections ORDER BY tenant_id,room_id; SELECT * FROM live_agent_room_plan_publications ORDER BY tenant_id,room_id,plan_id; SELECT id,tenant_id,room_id,plan_id,version_no,lifecycle_status,SHA2(variants_json,256) FROM live_agent_plan_versions ORDER BY id; SELECT COUNT(*),COALESCE(MAX(id),0) FROM live_runtime_sessions; SELECT id,SHA2(config_json,256),SHA2(keys_json,256) FROM mgmt_anchor_model_settings ORDER BY id;" > "$1"
}
idle_gate
snapshot "$BACKUP/business-before.tsv"
echo 'Backing up database; never automatically restoring business SQL'
timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 --hex-blob -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
gzip -t "$BACKUP/database-before.sql.gz"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner --no-same-permissions
printf '%s  %s\n' "$BINARY_SHA" "$STAGE/bin/management-service" | sha256sum -c -
cmp "$STAGE/web/index.html" "$STAGE/web-desktop/index.html"
grep -q 'name="xiaolan-app" content="desktop"' "$STAGE/web/index.html"
LIVE_JS=$(grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' "$EXPECTED/web-desktop/index.html")
[[ $LIVE_JS =~ ^/assets/index-[A-Za-z0-9_-]+\.js$ ]] || fail 'unexpected live entry'
grep -q 'speech-models' "$EXPECTED/web-desktop$LIVE_JS"
printf '%s  %s\n' "$EXPECTED_JS_SHA" "$EXPECTED/web-desktop$LIVE_JS" | sha256sum -c -
printf '%s  %s\n' "$EXPECTED_HTML_SHA" "$EXPECTED/web-desktop/index.html" | sha256sum -c -
sha256sum "$EXPECTED/web-desktop/index.html" > "$BACKUP/base-web-index.sha256"
mkdir -m 755 "$TARGET"
cp -a "$EXPECTED/." "$TARGET/"
for surface in web web-desktop; do
  [[ -d $TARGET/$surface && ! -L $TARGET/$surface ]] || fail 'web symlink'
  cp -a "$STAGE/$surface/assets/." "$TARGET/$surface/assets/"
done
python3 -B - "$EXPECTED" "$TARGET" "$STAGE" <<'PY'
import re,sys
from pathlib import Path
base,target,stage=map(Path,sys.argv[1:])
pattern=r'(?<=<script type="module" crossorigin src=")/assets/index-[A-Za-z0-9_-]+\.js(?="></script>)'
candidate=re.findall(pattern,(stage/'web-desktop/index.html').read_text())
assert len(candidate)==1
assert 'SpeechModelsView' in (stage/'web-desktop'/candidate[0].lstrip('/')).read_text()
for surface in ('web','web-desktop'):
    text,n=re.subn(pattern,candidate[0],(base/surface/'index.html').read_text())
    assert n==1
    import os
    temp=target/surface/'index.speech-discovery-new.html'
    with temp.open('x',encoding='utf-8') as f:f.write(text)
    os.replace(temp,target/surface/'index.html')
PY
chown -R ecs-user:ecs-user "$TARGET/web" "$TARGET/web-desktop"
find "$TARGET/web" "$TARGET/web-desktop" -type d -exec chmod 755 '{}' +
find "$TARGET/web" "$TARGET/web-desktop" -type f -exec chmod 644 '{}' +
[[ ! -L $TARGET/bin && ! -L $TARGET/bin/management-service ]] || fail 'binary symlink'
install -m 755 -o ecs-user -g ecs-user "$STAGE/bin/management-service" "$TARGET/bin/management-service"
python3 -B - "$EXPECTED" "$TARGET" "$BACKUP" "$ID" <<'PY'
import hashlib,json,os,sys
from pathlib import Path
from datetime import datetime,timezone
a,b,backup=map(Path,sys.argv[1:4])
def kept(root):
    d={}
    for p in root.rglob('*'):
        n=p.relative_to(root).as_posix()
        if n.split('/')[0] in ('web','web-desktop') or n in ('bin/management-service','VERSION','VERSION.json'):continue
        if p.is_symlink():d[n]=('link',os.readlink(p))
        elif p.is_file():d[n]=('file',hashlib.sha256(p.read_bytes()).hexdigest())
        elif p.is_dir():d[n]=('dir',)
        else:raise RuntimeError('special file')
    return d
assert kept(a)==kept(b),'non-target release contents changed'
(backup/'preserved.json').write_text(json.dumps(kept(a),sort_keys=True),encoding='utf-8')
(b/'VERSION').write_text(sys.argv[4]+'\n',encoding='utf-8')
(b/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[4],'base_release':a.name,'scope':'speech-model-discovery-management-desktop','deployed_at_utc':datetime.now(timezone.utc).isoformat(),'core_restarted':False,'models_selected':False}),encoding='utf-8')
PY
ACTIVATED=0
STOPPED=0
health() { for _ in $(seq 1 45); do if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then return 0; fi; sleep 1; done; return 1; }
rollback() {
  local code=$?
  trap - ERR INT TERM
  set +e
  if [[ $ACTIVATED == 1 && $(readlink -f "$ROOT/current") == "$TARGET" ]]; then
    systemctl stop xiaolan-management
    ln -s "$EXPECTED" "$ROOT/.speech-rollback-$ID"
    mv -Tf "$ROOT/.speech-rollback-$ID" "$ROOT/current"
    # Retain the added encryption key and additive table; never restore SQL.
    systemctl start xiaolan-management
    health
    echo "ROLLED_BACK=$EXPECTED; backup=$BACKUP" >&2
  elif [[ $STOPPED == 1 ]]; then systemctl start xiaolan-management; fi
  exit "${code:-1}"
}
trap rollback ERR INT TERM
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'concurrent deployment'
sha256sum -c "$BACKUP/base-web-index.sha256" >/dev/null
idle_gate
snapshot "$BACKUP/business-preswitch.tsv"
cmp "$BACKUP/business-before.tsv" "$BACKUP/business-preswitch.tsv"
cmp /etc/xiaolan/management.env "$BACKUP/management.env"
STOPPED=1
systemctl stop xiaolan-management
idle_gate
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
ln -s "$TARGET" "$ROOT/.speech-next-$ID"
sha256sum -c "$BACKUP/base-web-index.sha256" >/dev/null
ACTIVATED=1
mv -Tf "$ROOT/.speech-next-$ID" "$ROOT/current"
systemctl start xiaolan-management
STOPPED=0
health
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
[[ $(systemctl show -p MainPID --value xiaolan-core) == "$CORE_PID" ]] || fail 'Core PID changed'
[[ $(systemctl show -p MainPID --value xiaolan-xiaozhi) == "$DEVICE_PID" ]] || fail 'device PID changed'
cmp /etc/xiaolan/management.env "$BACKUP/management.env"
snapshot "$BACKUP/business-after.tsv"
cmp "$BACKUP/business-before.tsv" "$BACKUP/business-after.tsv"
sha256sum -c "$BACKUP/other-env.sha256" >/dev/null
python3 -B - "$BACKUP/management.env" <<'PY'
import sys
from pathlib import Path
def old_settings(p):
    return [l for l in Path(p).read_text().splitlines() if not l.strip().startswith('MODEL_CONFIG_ENCRYPTION_KEY=')]
assert old_settings(sys.argv[1])==old_settings('/etc/xiaolan/management.env'),'existing management settings changed'
PY
for spec in 'GET /api/v1/system/speech-models' 'PUT /api/v1/system/speech-models' 'POST /api/v1/system/speech-models/test' 'POST /api/v1/system/speech-models/models'; do
  read -r method path <<< "$spec"
  code=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X "$method" -H 'Content-Type: application/json' http://127.0.0.1:8080"$path")
  [[ $code == 401 || $code == 403 ]] || fail "permission guard: $spec ($code)"
  echo "Permission guard: $spec -> $code"
done
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' https://www.xiaolandaizi.cn/ | cmp - "$TARGET/web-desktop/index.html"
idle_gate
trap - ERR INT TERM
printf 'release_id=%s\nbase=%s\nscope=speech-model-discovery-management-desktop\ncore_restarted=false\ndevice_restarted=false\nmodel_default_changed=false\nroom_configuration_changed=false\nbackup=%s\n' "$ID" "$EXPECTED" "$BACKUP" > "$ROOT/deployments/$ID.txt"
echo "DEPLOYED=$ID; original models/rooms preserved; backup=$BACKUP"
