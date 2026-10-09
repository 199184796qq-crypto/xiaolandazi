#!/usr/bin/env bash
set -Eeuo pipefail
ARCHIVE="${1:?archive}"; ID="${2:?release}"; EXPECTED="${3:?baseline}"; SHA="${4:?archive sha256}"
ROOT=/opt/xiaolan
[[ $(id -u) == 0 ]] || exec sudo -n bash "$0" "$@"
[[ "$ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]+$ && "$ID" != *..* && "$SHA" =~ ^[a-f0-9]{64}$ && "$EXPECTED" =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ ]] || exit 2
[[ -f "$ARCHIVE" && -d "$EXPECTED" && ! -L "$EXPECTED" ]]
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || { echo 'another deployment is in progress'; exit 3; }
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]]
python3 - "$ARCHIVE" "$EXPECTED" <<'PY'
import hashlib,json,pathlib,sys,tarfile
base=pathlib.Path(sys.argv[2])
with tarfile.open(sys.argv[1]) as a:
 names=set()
 for m in a.getmembers():
  p=pathlib.PurePosixPath(m.name)
  if p.is_absolute() or '..' in p.parts or not(m.isfile() or m.isdir()):raise SystemExit('unsafe archive')
  if m.isfile():
   name=p.as_posix()
   if name in names:raise SystemExit('duplicate member')
   if name not in {'bin/management-service','INTERACTION-LEARNING-SOURCE.json'} and not name.startswith(('web/','web-desktop/','configs/prompts/anchor-style/')):raise SystemExit('unexpected member')
   names.add(name)
 if not {'bin/management-service','INTERACTION-LEARNING-SOURCE.json','web/index.html','web-desktop/index.html','configs/prompts/anchor-style/MANIFEST.json'}.issubset(names):raise SystemExit('incomplete overlay')
 source=json.load(a.extractfile('INTERACTION-LEARNING-SOURCE.json'))
 if source['base_release']!=str(base):raise SystemExit('wrong base')
 for rel,sha in source['baseline_files'].items():
  if hashlib.sha256((base/rel).read_bytes()).hexdigest()!=sha:raise SystemExit('baseline changed: '+rel)
 for rel,sha in source['overlay_files'].items():
  if hashlib.sha256(a.extractfile(rel).read()).hexdigest()!=sha:raise SystemExit('overlay hash mismatch: '+rel)
PY
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
assert_idle() {
 local count
 count=$(mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B -e 'SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ("running","paused","starting","resuming")')
 [[ "$count" == 0 ]] || { echo "active_sessions=$count; release not activated"; return 1; }
}
assert_base() { [[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'production baseline changed; release not activated'; return 1; }; }
assert_base
assert_idle
CORE_PID=$(systemctl show xiaolan-core -p MainPID --value)
GATEWAY_PID=$(systemctl show xiaolan-xiaozhi -p MainPID --value)
TARGET="$ROOT/releases/$ID"
[[ ! -e "$TARGET" ]]
mkdir -m 755 "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
python3 - "$ARCHIVE" "$TARGET" <<'PY'
import pathlib,sys,tarfile
root=pathlib.Path(sys.argv[2]).resolve()
with tarfile.open(sys.argv[1]) as a:
 paths=[root/m.name for m in a.getmembers() if m.isfile()]+[root/'VERSION',root/'VERSION.json']
 for p in paths:
  if root not in p.resolve().parents or p.is_symlink():raise SystemExit('unsafe detach')
  if p.is_file():p.unlink()
PY
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/management-service"
python3 - "$EXPECTED" "$TARGET" "$ID" "$SHA" <<'PY'
import datetime,hashlib,json,pathlib,sys
base,target=map(pathlib.Path,sys.argv[1:3]);root=target/'configs/prompts/anchor-style';old=base/'configs/prompts/anchor-style'
changed={'fact-purpose-angle-review.md','interaction-answer-assessment.md','interaction-learning-simulation.md','interaction-learning-user-authority.md','speech-review-authority.md'}
entries=json.loads((root/'MANIFEST.json').read_text(encoding='utf-8-sig'))
actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() and p.name!='MANIFEST.json'}
if actual!={e['path'] for e in entries}:raise SystemExit('incomplete mother manifest')
for e in entries:
 p=(root/e['path']).resolve()
 if root.resolve() not in p.parents or hashlib.sha256(p.read_bytes()).hexdigest()!=e['sha256']:raise SystemExit('invalid mother hash')
for p in old.rglob('*'):
 if p.is_file() and p.name!='MANIFEST.json' and p.relative_to(old).as_posix() not in changed:
  if p.read_bytes()!=(root/p.relative_to(old)).read_bytes():raise SystemExit('unrelated mother changed')
for rel in actual:
 if not(old/rel).is_file() and rel not in changed:raise SystemExit('unrelated mother added')
for surface in ('web','web-desktop'):
 for p in (base/surface).rglob('*'):
  if p.is_file() and p.name!='index.html' and p.read_bytes()!=(target/p.relative_to(base)).read_bytes():raise SystemExit('old web asset overwritten')
source=json.loads((target/'INTERACTION-LEARNING-SOURCE.json').read_text())
metadata={'release_id':sys.argv[3],'base_release':str(base),'source_commit':source['source_commit'],'scope':'user-owned interaction corrections; compliance-only speech review','archive_sha256':sys.argv[4],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(target/'VERSION.json').write_text(json.dumps(metadata,ensure_ascii=False),encoding='utf-8');(target/'VERSION').write_text(sys.argv[3]+'\n',encoding='ascii')
print('verified_prompt_files='+str(len(actual)))
PY
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
diff -qr "$EXPECTED/web-customer" "$TARGET/web-customer" >/dev/null
diff -qr "$EXPECTED/web-sales" "$TARGET/web-sales" >/dev/null
chown ecs-user:ecs-user "$TARGET" "$TARGET/bin/management-service" "$TARGET/VERSION" "$TARGET/VERSION.json"
ACTIVATED=0
rollback() {
 code=$?; trap - ERR INT TERM; set +e
 if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]];then
  ln -s "$EXPECTED" "$ROOT/.interaction-learning-rollback-$ID"
  mv -Tf "$ROOT/.interaction-learning-rollback-$ID" "$ROOT/current"
  systemctl restart xiaolan-management
  echo "rolled back to $EXPECTED"
 fi
 exit "$code"
}
trap rollback ERR INT TERM
assert_base
assert_idle
ln -s "$TARGET" "$ROOT/.interaction-learning-next-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.interaction-learning-next-$ID" "$ROOT/current"
systemctl restart xiaolan-management
healthy=0
for _ in $(seq 1 120);do
 if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1;then healthy=1;break;fi
 sleep 1
done
[[ "$healthy" == 1 ]]
curl -fsS --max-time 3 http://127.0.0.1:8081/healthz >/dev/null
curl -fsS --max-time 3 http://127.0.0.1:8083/healthz >/dev/null
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
[[ "$(systemctl show xiaolan-core -p MainPID --value)" == "$CORE_PID" && "$(systemctl show xiaolan-xiaozhi -p MainPID --value)" == "$GATEWAY_PID" ]]
SCHEMA_COUNT=$(mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B -e "SELECT (SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='agent_learning_sessions' AND column_name IN ('review_metadata_json','pending_review_key'))+(SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='agent_learning_sessions' AND index_name='uk_agent_learning_pending_review')")
[[ "$SCHEMA_COUNT" == 3 ]]
echo 'interaction_learning_schema=verified'
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nmanagement_restarted=true\ncore_pid_preserved=%s\ngateway_pid_preserved=%s\narchive_sha256=%s\n' "$ID" "$EXPECTED" "$TARGET" "$CORE_PID" "$GATEWAY_PID" "$SHA" > "$ROOT/deployments/$ID.txt"
echo "deploy ok: $TARGET"
