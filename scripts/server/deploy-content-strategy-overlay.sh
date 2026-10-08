#!/usr/bin/env bash
set -Eeuo pipefail
ARCHIVE="${1:?archive required}"
ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
SHA="${4:?archive sha256 required}"
BASE_SHA="${5:?expected management sha256 required}"
ROOT=/opt/xiaolan
[[ "$ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && "$ID" != *..* && "$SHA" =~ ^[a-f0-9]{64}$ && "$BASE_SHA" =~ ^[a-f0-9]{64}$ ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" && ! -L "$EXPECTED" && -f "$ARCHIVE" ]] || exit 2
if [[ $(id -u) != 0 ]];then exec sudo -n bash "$0" "$@";fi
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || { echo 'another release holds the lock' >&2;exit 3; }
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" && "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]] || exit 3
[[ "$(sha256sum "$EXPECTED/bin/management-service" | cut -d' ' -f1)" == "$BASE_SHA" ]] || { echo 'management baseline changed; rebuild required' >&2;exit 3; }
python3 - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
    names=set()
    for item in archive.getmembers():
        p=pathlib.PurePosixPath(item.name)
        if p.is_absolute() or '..' in p.parts or not(item.isfile() or item.isdir()):raise SystemExit('unsafe archive')
        if item.isfile():
            name=p.as_posix()
            if name in names:raise SystemExit('duplicate archive path')
            if name not in {'bin/management-service','CONTENT-STRATEGY-SOURCE.json'} and not name.startswith('configs/prompts/anchor-style/'):raise SystemExit('out-of-scope file')
            names.add(name)
    if not {'bin/management-service','configs/prompts/anchor-style/MANIFEST.json','CONTENT-STRATEGY-SOURCE.json'}.issubset(names):raise SystemExit('incomplete package')
PY
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
assert_idle() {
 local count
 count=$(mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -N -B -e 'SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ("running","paused","starting","resuming")')
 [[ "$count" == 0 ]] || { echo 'active session; management restart deferred' >&2;return 1; }
}
assert_idle
TARGET="$ROOT/releases/$ID"
[[ ! -e "$TARGET" ]] || exit 3
mkdir -m 755 "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
# Detach ONLY files in this new release before tar writes; old release hardlinks
# and rollback artifacts must never be modified through shared inodes.
python3 - "$TARGET" <<'PY'
import pathlib,sys
root=pathlib.Path(sys.argv[1]);mothers=root/'configs/prompts/anchor-style'
for p in [root/'bin/management-service',root/'VERSION',root/'VERSION.json',*mothers.rglob('*')]:
    if p.is_symlink():raise SystemExit('unexpected symlink in replaced scope')
    if p.is_file():
        if root.resolve() not in p.resolve().parents:raise SystemExit('out-of-scope target')
        p.unlink()
PY
tar -xzf "$ARCHIVE" -C "$TARGET"
chmod 0755 "$TARGET/bin/management-service"
chown ecs-user:ecs-user "$TARGET" "$TARGET/bin/management-service" "$TARGET/CONTENT-STRATEGY-SOURCE.json"
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
for surface in web web-desktop web-customer web-sales;do diff -qr "$EXPECTED/$surface" "$TARGET/$surface" >/dev/null;done
python3 - "$EXPECTED" "$TARGET" "$ID" "$SHA" <<'PY'
import datetime,hashlib,json,pathlib,sys
base,target=map(pathlib.Path,sys.argv[1:3]);old=base/'configs/prompts/anchor-style';root=target/'configs/prompts/anchor-style'
allowed={'fact-purpose-analysis.md','fact-purpose-angle-analysis.md','fact-purpose-angle-generation.md','fact-purpose-angle-review.md'}
expected=set()
for entry in json.loads((root/'MANIFEST.json').read_text(encoding='utf-8-sig')):
    p=(root/entry['path']).resolve()
    if root.resolve() not in p.parents or not p.is_file():raise SystemExit('unsafe mother path')
    if hashlib.sha256(p.read_bytes()).hexdigest()!=entry['sha256']:raise SystemExit('mother hash mismatch')
    expected.add(entry['path'])
actual={p.relative_to(root).as_posix() for p in root.rglob('*') if p.is_file() and p.name!='MANIFEST.json'}
if actual!=expected:raise SystemExit('incomplete mother manifest')
for p in old.rglob('*'):
    if p.is_file() and p.name!='MANIFEST.json':
        rel=p.relative_to(old).as_posix()
        if rel not in allowed and p.read_bytes()!=(root/rel).read_bytes():raise SystemExit('unrelated mother changed: '+rel)
for rel in actual:
    if not (old/rel).is_file() and rel not in allowed:raise SystemExit('unrelated mother added: '+rel)
metadata={'release_id':sys.argv[3],'base_release':str(base),'scope':'fact-purpose-angle-content-strategy-management-mothers','archive_sha256':sys.argv[4],'source_commit':json.loads((target/'CONTENT-STRATEGY-SOURCE.json').read_text())['feature_commit'],'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(target/'VERSION.json').write_text(json.dumps(metadata,ensure_ascii=False),encoding='utf-8');(target/'VERSION').write_text(sys.argv[3]+'\n',encoding='ascii')
print('verified_mother_files='+str(len(actual)))
PY
chown ecs-user:ecs-user "$TARGET/VERSION" "$TARGET/VERSION.json"
chmod 0644 "$TARGET/VERSION" "$TARGET/VERSION.json"
ACTIVATED=0
rollback() {
 code=$?;trap - ERR INT TERM;set +e
 if [[ "$ACTIVATED" == 1 && "$(readlink -f "$ROOT/current")" == "$TARGET" ]];then
  ln -s "$EXPECTED" "$ROOT/.content-strategy-rollback-$ID"
  mv -Tf "$ROOT/.content-strategy-rollback-$ID" "$ROOT/current"
  systemctl restart xiaolan-management
  echo "rolled back to $EXPECTED" >&2
 fi
 exit "$code"
}
trap rollback ERR INT TERM
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3
assert_idle
ln -s "$TARGET" "$ROOT/.content-strategy-next-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.content-strategy-next-$ID" "$ROOT/current"
systemctl restart xiaolan-management
healthy=0
for _ in $(seq 1 25);do if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null;then healthy=1;break;fi;sleep 1;done
[[ "$healthy" == 1 ]]
for port in 8081 8083;do curl -fsS --max-time 2 "http://127.0.0.1:$port/healthz" >/dev/null;done
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=content-strategy-management-mothers\nsha256=%s\n' "$ID" "$EXPECTED" "$TARGET" "$SHA" > "$ROOT/deployments/$ID.txt"
echo "deploy ok: $TARGET"
