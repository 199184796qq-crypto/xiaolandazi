#!/usr/bin/env bash
# Management + desktop only. Preserve Core/device PIDs, all other artifacts and env.
set -Eeuo pipefail
umask 077
ID=${1:?release id}
EXPECTED=${2:?base release}
ARCHIVE_SHA=${3:?archive digest}
ROOT=/opt/xiaolan
ARCHIVE=/tmp/$ID.tar.gz
TARGET=$ROOT/releases/$ID
STAGE=$ROOT/deployments/$ID-candidate
BACKUP=$ROOT/deployments/$ID-backup
fail() { echo "ABORT: $*" >&2; return 1; }
[[ $(id -u) == 0 ]] || fail 'root required'
[[ $ID =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && $ID != *..* ]] || fail 'invalid id'
[[ $EXPECTED =~ ^/opt/xiaolan/releases/[A-Za-z0-9][A-Za-z0-9._-]*$ && $EXPECTED != *..* ]] || fail 'invalid base'
[[ $ARCHIVE_SHA =~ ^[a-fA-F0-9]{64}$ ]] || fail 'invalid digest'
[[ -f $ARCHIVE && ! -L $ARCHIVE && -d $EXPECTED && ! -L $EXPECTED ]] || fail 'invalid inputs'
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another deployment holds lock'
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
for path in "$TARGET" "$STAGE" "$BACKUP" "$ROOT/.mainline-next-$ID" "$ROOT/.mainline-rollback-$ID"; do
  [[ ! -e $path && ! -L $path ]] || fail "target exists: $path"
done
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
python3 -B - "$ARCHIVE" <<'PY'
import sys, tarfile
from pathlib import PurePosixPath
with tarfile.open(sys.argv[1]) as a:
    names=set(); size=0
    for e in a:
        n=e.name.rstrip('/'); p=PurePosixPath(n)
        assert n and str(p)==n and not p.is_absolute() and '..' not in p.parts
        assert '\\' not in n and '\n' not in n and '\r' not in n
        assert e.isfile() or e.isdir()
        assert n not in names and not e.mode & 0o6000
        assert n in {'bin','bin/management-service','source-manifest.json'} or p.parts[0] in {'web','web-desktop'}
        names.add(n); size+=e.size
        assert size<512*1024*1024 and len(names)<20000
    assert {'bin/management-service','web/index.html','web-desktop/index.html','source-manifest.json'} <= names
PY
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
mkdir -m 700 "$STAGE" "$BACKUP"
systemctl show -p MainPID xiaolan-core xiaolan-xiaozhi > "$BACKUP/preserved-pids.txt"
find /etc/xiaolan -maxdepth 1 -type f -name '*.env' -print0 | sort -z | xargs -0 sha256sum > "$BACKUP/environment.sha256"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner --no-same-permissions
cp -a "$STAGE/source-manifest.json" "$BACKUP/"
grep -q 'name="xiaolan-app" content="desktop"' "$STAGE/web-desktop/index.html"
cmp "$STAGE/web/index.html" "$STAGE/web-desktop/index.html"
mkdir -m 755 "$TARGET"
cp -a --reflink=auto "$EXPECTED/." "$TARGET/"
[[ -d $TARGET/bin && ! -L $TARGET/bin && ! -L $TARGET/bin/management-service ]] || fail 'unsafe binary target'
for surface in web web-desktop; do
  [[ -d $TARGET/$surface && ! -L $TARGET/$surface ]] || fail 'unsafe web target'
done
python3 -B - "$TARGET" <<'PY'
import sys
from pathlib import Path
for s in ('web','web-desktop'):
    assert not any(p.is_symlink() for p in (Path(sys.argv[1])/s).rglob('*')), 'web symlinks forbidden'
PY
install -m 755 "$STAGE/bin/management-service" "$TARGET/bin/management-service"
# Preserve previous hashed assets for already-open browser sessions.
for surface in web web-desktop; do
  cp -a "$STAGE/$surface/." "$TARGET/$surface/"
  find "$TARGET/$surface" -type d -exec chmod 755 '{}' +
  find "$TARGET/$surface" -type f -exec chmod 644 '{}' +
  chown -R ecs-user:ecs-user "$TARGET/$surface"
done
python3 -B - "$EXPECTED" "$TARGET" "$ID" "$ARCHIVE_SHA" "$BACKUP" <<'PY'
import hashlib,json,os,sys
from pathlib import Path
from datetime import datetime,timezone
base,target=map(Path,sys.argv[1:3])
def kept(root):
    result={}
    for p in root.rglob('*'):
        n=p.relative_to(root).as_posix()
        if n.split('/')[0] in ('web','web-desktop') or n in ('bin/management-service','VERSION','VERSION.json'): continue
        if p.is_symlink(): result[n]=['link',os.readlink(p)]
        elif p.is_file(): result[n]=['file',hashlib.sha256(p.read_bytes()).hexdigest()]
        elif p.is_dir(): result[n]=['dir']
        else: raise RuntimeError('special file')
    return result
assert kept(base)==kept(target), 'non-target artifacts changed'
Path(sys.argv[5],'preserved-artifacts.json').write_text(json.dumps(kept(base),sort_keys=True))
(target/'VERSION').write_text(sys.argv[3]+'\n')
(target/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[3],'base_release':base.name,'scope':'mainline-management-desktop','archive_sha256':sys.argv[4],'core_restarted':False,'device_restarted':False,'deployed_at_utc':datetime.now(timezone.utc).isoformat()},indent=2))
PY
chmod 644 "$TARGET/VERSION" "$TARGET/VERSION.json"
health_wait() {
  for _ in $(seq 1 60); do
    if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then return 0; fi
    sleep 2
  done
  return 1
}
ACTIVATED=0
rollback() {
  local code=$?
  trap - ERR INT TERM
  set +e
  if [[ $ACTIVATED == 1 && $(readlink -f "$ROOT/current") == "$TARGET" ]]; then
    ln -s "$EXPECTED" "$ROOT/.mainline-rollback-$ID"
    mv -Tf "$ROOT/.mainline-rollback-$ID" "$ROOT/current"
    systemctl restart xiaolan-management
    health_wait
    echo "ROLLED_BACK=$EXPECTED; Core untouched; no SQL restored" >&2
  fi
  exit "$code"
}
trap rollback ERR INT TERM
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'concurrent release'
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
systemctl show -p MainPID xiaolan-core xiaolan-xiaozhi | cmp - "$BACKUP/preserved-pids.txt"
ln -s "$TARGET" "$ROOT/.mainline-next-$ID"
ACTIVATED=1
mv -Tf "$ROOT/.mainline-next-$ID" "$ROOT/current"
systemctl restart xiaolan-management
health_wait
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
systemctl show -p MainPID xiaolan-core xiaolan-xiaozhi | cmp - "$BACKUP/preserved-pids.txt"
sha256sum -c "$BACKUP/environment.sha256" >/dev/null
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' https://www.xiaolandaizi.cn/ | cmp - "$TARGET/web-desktop/index.html"
code=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data '{}' http://127.0.0.1:8080/api/v1/live-agent-plans/2/anchor-style/test)
[[ $code == 401 || $code == 403 ]] || fail 'test API unauthorized guard failed'
trap - ERR INT TERM
printf 'release_id=%s\nprevious=%s\nscope=management-desktop\ncore_device_pids_preserved=true\nenvironment_preserved=true\n' "$ID" "$EXPECTED" > "$ROOT/deployments/$ID.txt"
echo "DEPLOYED=$ID; management health=200; Core/device PIDs and environment unchanged; backup=$BACKUP"
