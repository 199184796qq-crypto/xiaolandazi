#!/usr/bin/env bash
set -Eeuo pipefail
ARCHIVE="${1:?archive required}"; ID="${2:?release id required}"; EXPECTED="${3:?expected release required}"; SHA="${4:?archive sha256 required}"
CORE_SHA="${5:?core baseline required}"; MGMT_SHA="${6:?management baseline required}"; GATEWAY_SHA="${7:?gateway baseline required}"
ROOT=/opt/xiaolan
[[ "$ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "$ID" != *..* && "$SHA" =~ ^[a-f0-9]{64}$ && "$CORE_SHA" =~ ^[a-f0-9]{64}$ && "$MGMT_SHA" =~ ^[a-f0-9]{64}$ && "$GATEWAY_SHA" =~ ^[a-f0-9]{64}$ ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" && ! -L "$EXPECTED" && -f "$ARCHIVE" ]] || exit 2
if [[ $(id -u) != 0 ]];then exec sudo -n bash "$0" "$@";fi
mkdir -p "$ROOT/deployments"
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || { echo 'another deployment holds release lock' >&2; exit 3; }
assert_base() {
 [[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]]
 [[ "$(sha256sum "$EXPECTED/bin/core-service" | cut -d' ' -f1)" == "$CORE_SHA" ]]
 [[ "$(sha256sum "$EXPECTED/bin/management-service" | cut -d' ' -f1)" == "$MGMT_SHA" ]]
 [[ "$(sha256sum "$EXPECTED/bin/xiaozhi-gateway" | cut -d' ' -f1)" == "$GATEWAY_SHA" ]]
}
assert_base
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]]
python3 - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
 names=set()
 for item in archive.getmembers():
  p=pathlib.PurePosixPath(item.name)
  if p.is_absolute() or '..' in p.parts or not(item.isfile() or item.isdir()):raise SystemExit('unsafe archive')
  if item.isfile():
   name=p.as_posix()
   if name in names:raise SystemExit('duplicate file')
   if name not in {'bin/core-service','bin/management-service','INTERRUPT-PHASE1-SOURCE.json'} and not name.startswith('configs/prompts/anchor-style/'):raise SystemExit('out-of-scope overlay')
   names.add(name)
 if not {'bin/core-service','bin/management-service','INTERRUPT-PHASE1-SOURCE.json','configs/prompts/anchor-style/MANIFEST.json'}.issubset(names):raise SystemExit('incomplete overlay')
PY
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
assert_idle() {
 local count
 count=$(mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B -e 'SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ("running","paused","starting","resuming")')
 [[ "$count" == 0 ]] || { echo 'active/paused session; restart refused' >&2; return 1; }
 python3 <<'PY'
import json,os,subprocess,urllib.request
args=['mysql','--connect-timeout=8','-h',os.environ['DB_HOST'],'-P',os.environ.get('DB_PORT','3306'),'-u',os.environ['DB_USER'],os.environ['DB_NAME'],'-N','-B','-e','SELECT tenant_id,id FROM core_rooms']
rooms=subprocess.check_output(args,text=True).splitlines()
for row in rooms:
 tenant,room=row.split('\t');headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']}
 for tail in ('audio/program','audio-engine'):
  req=urllib.request.Request('http://127.0.0.1:8081/internal/v1/rooms/'+room+'/'+tail+'?tenant_id='+tenant,headers=headers)
  with urllib.request.urlopen(req,timeout=4) as response:state=json.load(response)
  if tail=='audio/program' and state.get('running'):raise SystemExit('room audio program active: '+room)
  if tail=='audio-engine' and state.get('phase','idle')!='idle':raise SystemExit('room PCM engine active: '+room)
print('idle_sessions=0 checked_rooms='+str(len(rooms)))
PY
}
assert_idle
TARGET="$ROOT/releases/$ID"
[[ ! -e "$TARGET" ]] || exit 3
mkdir -m 755 "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
python3 - "$TARGET" <<'PY'
import pathlib,sys
root=pathlib.Path(sys.argv[1]);mothers=root/'configs/prompts/anchor-style'
for p in [root/'bin/core-service',root/'bin/management-service',root/'VERSION',root/'VERSION.json',*mothers.rglob('*')]:
 if p.is_symlink():raise SystemExit('unexpected replacement symlink')
 if p.is_file():
  if root.resolve() not in p.resolve().parents:raise SystemExit('out-of-scope detach')
  p.unlink()
PY
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/core-service" "$TARGET/bin/management-service"
chown ecs-user:ecs-user "$TARGET" "$TARGET/bin/core-service" "$TARGET/bin/management-service" "$TARGET/INTERRUPT-PHASE1-SOURCE.json"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
for surface in web web-desktop web-customer web-sales;do diff -qr "$EXPECTED/$surface" "$TARGET/$surface" >/dev/null;done
python3 - "$EXPECTED" "$TARGET" "$ID" "$SHA" <<'PY'
import datetime,hashlib,json,pathlib,sys
base,target=map(pathlib.Path,sys.argv[1:3]);old=base/'configs/prompts/anchor-style';root=target/'configs/prompts/anchor-style'
allowed={'interaction-scheduling.json','interaction-continuity-generation.md','interaction-purpose-resolution.md','interaction-return-core-answer.md','interaction-return-bridge-generation.md','fact-purpose-selected-generation.md','interaction-task-angle-probe-analysis.md','interaction-task-angle-probe-speech.md'}
expected=set()
for entry in json.loads((root/'MANIFEST.json').read_text(encoding='utf-8-sig')):
 p=(root/entry['path']).resolve()
 if root.resolve() not in p.parents or not p.is_file():raise SystemExit('unsafe mother')
 if hashlib.sha256(p.read_bytes()).hexdigest()!=entry['sha256']:raise SystemExit('mother hash mismatch')
 expected.add(entry['path'])
actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() and p.name!='MANIFEST.json'}
if actual!=expected:raise SystemExit('mother manifest incomplete')
for p in old.rglob('*'):
 if p.is_file() and p.name!='MANIFEST.json':
  rel=p.relative_to(old).as_posix()
  if rel not in allowed and p.read_bytes()!=(root/rel).read_bytes():raise SystemExit('unrelated mother changed: '+rel)
for rel in actual:
 if not(old/rel).is_file() and rel not in allowed:raise SystemExit('unrelated mother added: '+rel)
source=json.loads((target/'INTERRUPT-PHASE1-SOURCE.json').read_text())
for name,sha in source['binaries'].items():
 if hashlib.sha256((target/'bin'/name).read_bytes()).hexdigest()!=sha:raise SystemExit('binary hash mismatch')
metadata={'release_id':sys.argv[3],'base_release':str(base),'scope':'interrupt-event-execution-and-answer-bridge-return','archive_sha256':sys.argv[4],'source_commit':source['feature_commit'],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(target/'VERSION.json').write_text(json.dumps(metadata,ensure_ascii=False),encoding='utf-8');(target/'VERSION').write_text(sys.argv[3]+'\n',encoding='ascii')
print('verified_mother_files='+str(len(actual)))
PY
chown ecs-user:ecs-user "$TARGET/VERSION" "$TARGET/VERSION.json"
ACTIVATED=0
rollback() {
 code=$?;trap - ERR INT TERM;set +e
 if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]];then
  ln -s "$EXPECTED" "$ROOT/.interrupt-rollback-$ID"
  mv -Tf "$ROOT/.interrupt-rollback-$ID" "$ROOT/current"
  systemctl restart xiaolan-core xiaolan-management
  echo "rolled back to $EXPECTED" >&2
 fi
 exit "$code"
}
trap rollback ERR INT TERM
assert_base
assert_idle
ln -s "$TARGET" "$ROOT/.interrupt-next-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.interrupt-next-$ID" "$ROOT/current"
systemctl restart xiaolan-core xiaolan-management
healthy=0
for _ in $(seq 1 25);do if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null && curl -fsS --max-time 2 http://127.0.0.1:8081/healthz >/dev/null;then healthy=1;break;fi;sleep 1;done
[[ "$healthy" == 1 ]]
curl -fsS --max-time 2 http://127.0.0.1:8083/healthz >/dev/null
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=interrupt-execution-answer-return\nsha256=%s\n' "$ID" "$EXPECTED" "$TARGET" "$SHA" > "$ROOT/deployments/$ID.txt"
echo "deploy ok: $TARGET"
