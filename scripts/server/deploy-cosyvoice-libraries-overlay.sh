#!/usr/bin/env bash
set -Eeuo pipefail
ARCHIVE=${1:?archive}; ID=${2:?release}; EXPECTED=${3:?baseline}; SHA=${4:?sha256}
ROOT=/opt/xiaolan
[[ $(id -u) == 0 ]] || exec sudo -n bash "$0" "$@"
[[ $ID =~ ^[A-Za-z0-9][A-Za-z0-9._-]+$ && $ID != *..* && $SHA =~ ^[a-f0-9]{64}$ && $EXPECTED =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ ]]
[[ -f $ARCHIVE && -d $EXPECTED && ! -L $EXPECTED ]]
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || { echo 'another deployment is in progress'; exit 3; }
[[ $(sha256sum "$ARCHIVE" | cut -d' ' -f1) == "$SHA" ]]
assert_source() {
 python3 - "$ARCHIVE" "$EXPECTED" <<'PY'
import hashlib,json,pathlib,sys,tarfile
base=pathlib.Path(sys.argv[2])
with tarfile.open(sys.argv[1]) as a:
 names=set()
 for m in a.getmembers():
  p=pathlib.PurePosixPath(m.name);n=p.as_posix()
  assert not p.is_absolute() and '..' not in p.parts and '\\' not in n and (m.isfile() or m.isdir())
  if m.isfile():
   assert n not in names
   assert n in {'bin/management-service','COSYVOICE-SOURCE.json'} or n.startswith(('web/','web-desktop/','configs/prompts/anchor-style/'))
   names.add(n)
 assert {'bin/management-service','COSYVOICE-SOURCE.json','web/index.html','web-desktop/index.html','configs/prompts/anchor-style/MANIFEST.json'}<=names
 source=json.load(a.extractfile('COSYVOICE-SOURCE.json'))
 assert source['base_release']==str(base)
 for n,sha in source['baseline_files'].items():
  assert hashlib.sha256((base/n).read_bytes()).hexdigest()==sha,'baseline changed: '+n
 for n,sha in source['overlay_files'].items():
  assert hashlib.sha256(a.extractfile(n).read()).hexdigest()==sha,'package hash mismatch: '+n
PY
}
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
MYSQL=(mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B)
assert_base() { [[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]]; }
assert_idle() {
 [[ $("${MYSQL[@]}" -e 'SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ("running","paused","starting","resuming")') == 0 ]] || { echo 'live sessions active; not activated'; return 1; }
 python3 - <<'PY'
import json,os,urllib.request
for b in (os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(','):
 b=b.strip().rstrip('/')
 def get(path):
  r=urllib.request.Request(b+path,headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']})
  with urllib.request.urlopen(r,timeout=8) as v:return json.load(v)
 for room in get('/internal/v1/rooms')['items']:
  speech=get(f"/internal/v1/rooms/{room['id']}/speech-runtime?tenant_id={room['tenant_id']}")
  p=speech.get('program') or {}
  assert not p.get('running') and not p.get('suspended'),'active audio program'
  assert all(str((speech.get(t) or {}).get('status','')).lower() in {'','idle','completed','failed'} for t in ('mainline','interrupt')),'active audio track'
PY
}
business_hash() {
 "${MYSQL[@]}" -e 'SELECT id,tenant_id,voice_id,provider,CAST(config_json AS CHAR),clone_status FROM voice_profiles ORDER BY id; SELECT id,tenant_id,profile_id,model,voice_id,status FROM voice_model_bindings ORDER BY id; SELECT tenant_id,room_id,plan_id,version_id FROM live_agent_room_plan_publications ORDER BY tenant_id,room_id,plan_id; SELECT id,CAST(speech_config_json AS CHAR) FROM live_agent_config_versions ORDER BY id;' | sha256sum | cut -d' ' -f1
}
assert_base; assert_source; assert_idle
CORE_PID=$(systemctl show xiaolan-core -p MainPID --value)
GATEWAY_PID=$(systemctl show xiaolan-xiaozhi -p MainPID --value)
TARGET="$ROOT/releases/$ID"
[[ ! -e $TARGET ]]
mkdir -m 755 "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
python3 - "$ARCHIVE" "$TARGET" <<'PY'
import pathlib,sys,tarfile
root=pathlib.Path(sys.argv[2]).resolve()
with tarfile.open(sys.argv[1]) as a:
 paths=[root/m.name for m in a.getmembers() if m.isfile()]+[root/'VERSION',root/'VERSION.json']
 for p in paths:
  assert root in p.resolve().parents and not p.is_symlink(),'unsafe detach'
  if p.is_file():p.unlink()
PY
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/management-service"
python3 - "$EXPECTED" "$TARGET" "$ID" "$SHA" <<'PY'
import datetime,hashlib,json,pathlib,sys
base,target=map(pathlib.Path,sys.argv[1:3]);old=base/'configs/prompts/anchor-style';root=target/'configs/prompts/anchor-style'
entries=json.loads((root/'MANIFEST.json').read_text(encoding='utf-8-sig'))
actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() and p.name!='MANIFEST.json'}
assert actual=={e['path'] for e in entries},'incomplete mother manifest'
for e in entries:assert hashlib.sha256((root/e['path']).read_bytes()).hexdigest()==e['sha256'],'mother hash mismatch'
for p in old.rglob('*'):
 if p.is_file() and p.name!='MANIFEST.json':assert p.read_bytes()==(root/p.relative_to(old)).read_bytes(),'existing mother changed'
assert actual-{p.relative_to(old).as_posix() for p in old.rglob('*') if p.is_file() and p.name!='MANIFEST.json'}=={'cosyvoice-tts-delivery.md'},'unexpected new mother'
for surface in ('web','web-desktop'):
 for p in (base/surface).rglob('*'):
  if p.is_file() and p.name!='index.html':assert p.read_bytes()==(target/p.relative_to(base)).read_bytes(),'old web asset overwritten'
source=json.loads((target/'COSYVOICE-SOURCE.json').read_text())
meta={'release_id':sys.argv[3],'base_release':str(base),'source_commit':source['source_commit'],'scope':'CosyVoice 3.5 providers and isolated voice libraries','archive_sha256':sys.argv[4],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(target/'VERSION.json').write_text(json.dumps(meta,ensure_ascii=False),encoding='utf-8');(target/'VERSION').write_text(sys.argv[3]+'\n',encoding='ascii')
print('complete_mother_manifest='+str(len(entries)))
PY
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
diff -qr "$EXPECTED/web-customer" "$TARGET/web-customer" >/dev/null
diff -qr "$EXPECTED/web-sales" "$TARGET/web-sales" >/dev/null
chown ecs-user:ecs-user "$TARGET" "$TARGET/bin/management-service" "$TARGET/VERSION" "$TARGET/VERSION.json"
ACTIVATED=0
rollback() {
 code=$?;trap - ERR INT TERM;set +e
 if [[ $ACTIVATED == 1 && $(readlink -f "$ROOT/current") == "$TARGET" ]];then
  ln -s "$EXPECTED" "$ROOT/.cosyvoice-rollback-$ID"
  mv -Tf "$ROOT/.cosyvoice-rollback-$ID" "$ROOT/current"
  systemctl restart xiaolan-management
  echo "rolled back to $EXPECTED"
 fi
 exit "$code"
}
trap rollback ERR INT TERM
assert_base;assert_source;assert_idle
BEFORE=$(business_hash)
ln -s "$TARGET" "$ROOT/.cosyvoice-next-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.cosyvoice-next-$ID" "$ROOT/current"
systemctl restart xiaolan-management
healthy=0
for _ in $(seq 1 45);do
 if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1;then healthy=1;break;fi
 sleep 1
done
[[ $healthy == 1 ]]
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
[[ $(systemctl show xiaolan-core -p MainPID --value) == "$CORE_PID" && $(systemctl show xiaolan-xiaozhi -p MainPID --value) == "$GATEWAY_PID" ]]
[[ $(business_hash) == "$BEFORE" ]]
for spec in 'GET /api/v1/live/official-voices?model=cosyvoice-v3.5-flash' 'POST /api/v1/live/voice-profiles/design' 'POST /api/v1/live/voice-profiles/clone';do
 read -r method path <<< "$spec"
 code=$(curl -sS --max-time 8 -o /dev/null -w '%{http_code}' -X "$method" -H 'Content-Type: application/json' http://127.0.0.1:8080"$path")
 [[ $code == 401 || $code == 403 ]]
done
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\nsource_commit=%s\ncore_pid_preserved=%s\ngateway_pid_preserved=%s\nexisting_voice_config_preserved=true\narchive_sha256=%s\n' "$ID" "$EXPECTED" "$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["source_commit"])' "$TARGET/COSYVOICE-SOURCE.json")" "$CORE_PID" "$GATEWAY_PID" "$SHA" > "$ROOT/deployments/$ID.txt"
echo "DEPLOYED=$TARGET; existing room voices preserved; Core/Gateway not restarted"
