#!/usr/bin/env bash
# Desktop assets only. Never restart services, change credentials or business data.
set -Eeuo pipefail
umask 077
ID=${1:?release id}
EXPECTED=${2:?base release}
ARCHIVE_SHA=${3:?archive digest}
HTML_SHA=${4:?base HTML digest}
JS_SHA=${5:?base JS digest}
ROOT=/opt/xiaolan
ARCHIVE=/tmp/$ID.tar.gz
TARGET=$ROOT/releases/$ID
STAGE=$ROOT/deployments/$ID-candidate
BACKUP=$ROOT/deployments/$ID-backup
fail() { echo "ABORT: $*" >&2; exit 1; }
[[ $(id -u) == 0 ]] || fail 'root required'
[[ $ID =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ && $ID != *..* ]] || fail 'invalid id'
[[ $EXPECTED =~ ^/opt/xiaolan/releases/[A-Za-z0-9._-]+$ && $EXPECTED != *..* ]] || fail 'invalid base'
for hash in "$ARCHIVE_SHA" "$HTML_SHA" "$JS_SHA"; do [[ $hash =~ ^[a-f0-9]{64}$ ]] || fail 'invalid digest'; done
for path in "$TARGET" "$STAGE" "$BACKUP" "$ROOT/.picker-next-$ID" "$ROOT/.picker-rollback-$ID"; do
  [[ ! -e $path && ! -L $path ]] || fail "existing target: $path"
done
[[ -f $ARCHIVE && ! -L $ARCHIVE ]] || fail 'missing package'
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another release holds lock'
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
printf '%s  %s\n' "$ARCHIVE_SHA" "$ARCHIVE" | sha256sum -c -
printf '%s  %s\n' "$HTML_SHA" "$EXPECTED/web-desktop/index.html" | sha256sum -c -
BASE_JS=$(grep -oE '/assets/index-[A-Za-z0-9_-]+\.js' "$EXPECTED/web-desktop/index.html")
[[ $BASE_JS =~ ^/assets/index-[A-Za-z0-9_-]+\.js$ ]] || fail 'unexpected entry'
printf '%s  %s\n' "$JS_SHA" "$EXPECTED/web-desktop$BASE_JS" | sha256sum -c -
python3 -B - "$ARCHIVE" <<'PY'
import sys,tarfile
from pathlib import PurePosixPath
with tarfile.open(sys.argv[1]) as archive:
    names=set();size=0
    for e in archive:
        n=e.name.rstrip('/');p=PurePosixPath(n)
        assert n and str(p)==n and not p.is_absolute() and '..' not in p.parts
        assert '\\' not in n and '\n' not in n and '\r' not in n
        assert e.isfile() or e.isdir()
        assert n not in names and not e.mode & 0o6000
        assert n in {'index.html','manifest.json','assets'} or (len(p.parts)==2 and p.parts[0]=='assets' and e.isfile())
        names.add(n);size+=e.size
        assert size<30*1024*1024 and len(names)<100
    assert {'index.html','manifest.json','assets'} <= names
PY
mkdir -m 700 "$STAGE" "$BACKUP"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner --no-same-permissions
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
systemctl show -p MainPID xiaolan-management xiaolan-core xiaolan-xiaozhi > "$BACKUP/pids-before.txt"
find /etc/xiaolan -maxdepth 1 -type f -name '*.env' -print0 | sort -z | xargs -0 sha256sum > "$BACKUP/env-before.sha256"
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
snapshot_models() {
  mysql --connect-timeout=5 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names -e 'SELECT id,SHA2(config_json,256),SHA2(keys_json,256) FROM mgmt_anchor_model_settings ORDER BY id;' > "$1"
}
snapshot_models "$BACKUP/models-before.tsv"
python3 -B - "$EXPECTED" "$STAGE" "$BACKUP" <<'PY'
import hashlib,json,re,sys
from pathlib import Path
base,stage,backup=map(Path,sys.argv[1:])
pattern=r'([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)'
def normal(text):return re.sub(pattern,r'\1-HASH.\2',text)
def refs(text):return set(re.findall(r'(?:assets/|\./)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))',text))
html=(base/'web-desktop/index.html').read_text()
live=refs(html);todo=list(live)
while todo:
    n=todo.pop()
    p=base/'web-desktop/assets'/n
    assert p.is_file() and not p.is_symlink()
    if p.suffix=='.js':
        for x in refs(p.read_text()):
            if x not in live:live.add(x);todo.append(x)
def key(n):
    m=re.fullmatch(pattern,n);assert m
    return m[1],m[2]
mapping={key(n):n for n in live}
assert len(mapping)==len(live),'ambiguous base assets'
manifest=json.loads((stage/'manifest.json').read_text())
assert manifest['scope']=='model-picker-desktop-only'
candidate=set(p.name for p in (stage/'assets').iterdir())
assert candidate==set(manifest['files'])
assert key(manifest['main'])==('index','js')
for n in candidate:
    k=key(n);assert k in mapping,'unexpected additional chunk: '+n
    if k[0]=='SpeechModelsView':continue
    a=base/'web-desktop/assets'/mapping[k];b=stage/'assets'/n
    if k[1]=='js':assert normal(a.read_text())==normal(b.read_text()),'non-target JS changed: '+n
    else:assert a.read_bytes()==b.read_bytes(),'non-target CSS changed: '+n
active=refs((stage/'index.html').read_text());todo=list(active)
while todo:
    n=todo.pop();p=stage/'assets'/n
    assert p.is_file()
    if p.suffix=='.js':
        for x in refs(p.read_text()):
            if x not in active:active.add(x);todo.append(x)
assert active==candidate,'candidate graph incomplete or includes stale assets'
with (backup/'base-assets.sha256').open('w') as f:
    for n in sorted(live):
        p=base/'web-desktop/assets'/n
        f.write(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p)+'\n')
    p=base/'web-desktop/index.html'
    f.write(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p)+'\n')
print('Verified: only SpeechModelsView changed; all other JS/CSS preserved')
PY
mkdir -m 755 "$TARGET"
cp -a --reflink=auto "$EXPECTED/." "$TARGET/"
python3 -B - "$EXPECTED" "$TARGET" "$STAGE" "$BACKUP" <<'PY'
import hashlib,json,os,re,shutil,sys
from pathlib import Path
base,target,stage,backup=map(Path,sys.argv[1:])
manifest=json.loads((stage/'manifest.json').read_text())
pattern=r'(?<=<script type="module" crossorigin src=")/assets/index-[A-Za-z0-9_-]+\.js(?="></script>)'
for surface in ('web','web-desktop'):
    assert not (target/surface).is_symlink()
    for source in (stage/'assets').iterdir():
        dest=target/surface/'assets'/source.name
        assert not dest.is_symlink()
        if dest.exists():assert dest.read_bytes()==source.read_bytes(),'asset name collision'
        else:shutil.copyfile(source,dest);dest.chmod(0o644)
    text,count=re.subn(pattern,'/assets/'+manifest['main'],(base/surface/'index.html').read_text())
    assert count==1
    temp=target/surface/'index.picker-new.html'
    with temp.open('x') as f:f.write(text)
    temp.chmod(0o644);os.replace(temp,target/surface/'index.html')
def kept(root):
    d={}
    for p in root.rglob('*'):
        n=p.relative_to(root).as_posix()
        if n.split('/')[0] in ('web','web-desktop') or n in ('VERSION','VERSION.json'):continue
        if p.is_symlink():d[n]=('link',os.readlink(p))
        elif p.is_file():d[n]=('file',hashlib.sha256(p.read_bytes()).hexdigest())
        elif p.is_dir():d[n]=('dir',)
        else:raise RuntimeError('special file')
    return d
assert kept(base)==kept(target),'non-web content changed'
(backup/'manifest.json').write_text(json.dumps(manifest))
(target/'VERSION').write_text(target.name+'\n')
(target/'VERSION.json').write_text(json.dumps({'release_id':target.name,'base_release':base.name,'scope':'model-picker-desktop-only','services_restarted':False,'business_data_changed':False}))
PY
ACTIVATED=0
rollback() {
  local code=$?
  trap - ERR INT TERM
  set +e
  if [[ $ACTIVATED == 1 && $(readlink -f "$ROOT/current") == "$TARGET" ]]; then
    ln -s "$EXPECTED" "$ROOT/.picker-rollback-$ID"
    mv -Tf "$ROOT/.picker-rollback-$ID" "$ROOT/current"
    echo "ROLLED_BACK=$EXPECTED; no services restarted" >&2
  fi
  exit "${code:-1}"
}
trap rollback ERR INT TERM
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'concurrent deployment'
sha256sum -c "$BACKUP/base-assets.sha256" >/dev/null
sha256sum -c "$BACKUP/env-before.sha256" >/dev/null
snapshot_models "$BACKUP/models-preswitch.tsv"
cmp "$BACKUP/models-before.tsv" "$BACKUP/models-preswitch.tsv"
ln -s "$TARGET" "$ROOT/.picker-next-$ID"
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'concurrent deployment'
sha256sum -c "$BACKUP/base-assets.sha256" >/dev/null
ACTIVATED=1
mv -Tf "$ROOT/.picker-next-$ID" "$ROOT/current"
curl -fsS --max-time 10 http://127.0.0.1:8080/healthz >/dev/null
systemctl is-active --quiet xiaolan-management xiaolan-core xiaolan-xiaozhi
systemctl show -p MainPID xiaolan-management xiaolan-core xiaolan-xiaozhi > "$BACKUP/pids-after.txt"
cmp "$BACKUP/pids-before.txt" "$BACKUP/pids-after.txt"
snapshot_models "$BACKUP/models-after.tsv"
cmp "$BACKUP/models-before.tsv" "$BACKUP/models-after.tsv"
sha256sum -c "$BACKUP/env-before.sha256" >/dev/null
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' https://www.xiaolandaizi.cn/ | cmp - "$TARGET/web-desktop/index.html"
python3 -B - "$TARGET" <<'PY'
import re,sys,urllib.request
from pathlib import Path
r=Path(sys.argv[1]);html=(r/'web-desktop/index.html').read_text()
main=re.search(r'src="(/assets/index-[A-Za-z0-9_-]+\.js)"',html)[1]
text=(r/'web-desktop'/main.lstrip('/')).read_text()
models=set(re.findall(r'(?:assets/|\./)(SpeechModelsView-[A-Za-z0-9_-]+\.(?:js|css))',text))
assert len(models)==2
for path in {main,*('/assets/'+n for n in models)}:
    with urllib.request.urlopen('https://www.xiaolandaizi.cn'+path,timeout=15) as response:
        assert response.read()==(r/'web-desktop'/path.lstrip('/')).read_bytes()
print('Public main entry and model picker JS/CSS verified')
PY
trap - ERR INT TERM
echo "DEPLOYED=$ID; frontend only; all service PIDs, encrypted keys and default model unchanged; backup=$BACKUP"
