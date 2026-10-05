#!/usr/bin/env bash
# Commerce closed loop: additive schema + management + desktop/mobile. Core/device untouched.
set -Eeuo pipefail
umask 077
ID=${1:?release id}
EXPECTED=${2:?base release}
ARCHIVE_SHA=${3:?archive digest}
BINARY_SHA=${4:?binary digest}
MANIFEST_SHA=${5:?source manifest digest}
EXPECTED_JS_SHA=${6:?base desktop JS digest}
EXPECTED_HTML_SHA=${7:?base desktop HTML digest}
EXPECTED_BINARY_SHA=${8:?base management binary digest}
EXPECTED_MOBILE_SHA=${9:?base mobile HTML digest}
EXPECTED_SALES_SHA=${10:?base sales HTML digest}
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
for hash in "$ARCHIVE_SHA" "$BINARY_SHA" "$MANIFEST_SHA" "$EXPECTED_JS_SHA" "$EXPECTED_HTML_SHA" "$EXPECTED_BINARY_SHA" "$EXPECTED_MOBILE_SHA" "$EXPECTED_SALES_SHA"; do
  [[ $hash =~ ^[a-f0-9]{64}$ ]] || fail 'invalid digest'
done
for path in "$TARGET" "$STAGE" "$BACKUP" "$ROOT/.speech-next-$ID" "$ROOT/.speech-rollback-$ID"; do
  [[ ! -e $path && ! -L $path ]] || fail "existing target: $path"
done
[[ -f $ARCHIVE && ! -L $ARCHIVE && -f $MANIFEST && ! -L $MANIFEST ]] || fail 'missing package'
exec 9>"$ROOT/deployments/xiaolan-release.lock"
flock -n 9 || fail 'another release holds lock'
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
printf '%s  %s\n' "$EXPECTED_BINARY_SHA" "$EXPECTED/bin/management-service" | sha256sum -c -
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
        assert n in ('bin','bin/management-service') or p.parts[0] in ('web','web-desktop','web-customer','web-sales')
        seen.add(n); size+=e.size
        assert size<1024**3 and len(seen)<50000
    assert {'bin/management-service','web/index.html','web-desktop/index.html','web-customer/index.html','web-sales/index.html'}<=seen
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
financial_snapshot() {
  local out="$1"
  : > "$out"
  for table in catalog_membership_plans catalog_membership_plan_versions catalog_time_card_products catalog_time_card_versions catalog_device_products catalog_device_versions mkt_campaigns mkt_campaign_items mkt_campaign_price_rules mkt_campaign_placements inc_programs inc_program_versions inc_rules; do
    printf '\nTABLE=%s\n' "$table" >> "$out"
    "${MYSQL[@]}" -e "SELECT * FROM $table ORDER BY id" >> "$out"
  done
}
idle_gate
NEW_TABLES="'mkt_campaign_controls','mkt_campaign_item_options','mkt_claim_locks','mkt_claim_reservations','fin_commerce_rule_heads','fin_commerce_rule_versions','fin_order_reward_snapshots'"
[[ $("${MYSQL[@]}" -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ($NEW_TABLES)") == 0 ]] || fail 'commerce tables unexpectedly exist; inspect before retry'
"${MYSQL[@]}" -e "SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() ORDER BY table_name" > "$BACKUP/tables-before.tsv"
printf '%s  %s\n' "$EXPECTED_MOBILE_SHA" "$EXPECTED/web-customer/index.html" | sha256sum -c -
sha256sum "$EXPECTED/web-customer/index.html" > "$BACKUP/base-mobile-index.sha256"
printf '%s  %s\n' "$EXPECTED_SALES_SHA" "$EXPECTED/web-sales/index.html" | sha256sum -c -
sha256sum "$EXPECTED/web-sales/index.html" > "$BACKUP/base-sales-index.sha256"
financial_snapshot "$BACKUP/finance-before.tsv"
snapshot "$BACKUP/business-before.tsv"
"${MYSQL[@]}" -e "EXPLAIN SELECT t.id,COALESCE(NULLIF(TRIM(u.display_name),''),NULLIF(TRIM(t.name),''),NULLIF(TRIM(u.username),''),'未填写客户名称'),COALESCE(u.phone,'') FROM mgmt_tenants t LEFT JOIN mgmt_users u ON u.id=(SELECT owner.id FROM mgmt_users owner WHERE owner.tenant_id=t.id AND owner.role='customer' ORDER BY (owner.status='active') DESC,owner.id ASC LIMIT 1) WHERE t.id=14" >/dev/null
echo 'Backing up database; never automatically restoring business SQL'
timeout 180s mysqldump --single-transaction --no-tablespaces --set-gtid-purged=OFF --column-statistics=0 --hex-blob -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" | gzip > "$BACKUP/database-before.sql.gz"
gzip -t "$BACKUP/database-before.sql.gz"
tar -xzf "$ARCHIVE" -C "$STAGE" --no-same-owner --no-same-permissions
printf '%s  %s\n' "$BINARY_SHA" "$STAGE/bin/management-service" | sha256sum -c -
python3 -B - "$STAGE" "$MANIFEST" <<'PY'
import hashlib,json,sys
from pathlib import Path
root=Path(sys.argv[1]);records=json.load(open(sys.argv[2]))['packaged_files']
expected={r['path']:r['sha256'] for r in records}
actual={p.relative_to(root).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in root.rglob('*') if p.is_file()}
assert expected==actual,'package differs from build manifest'
assert 'content="customer-mobile"' in (root/'web-customer/index.html').read_text()
print('All packaged binary/desktop/mobile hashes match frozen build manifest')
PY
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
python3 -B - "$EXPECTED" "$TARGET" "$STAGE" "$BACKUP" <<'PY'
import hashlib,re,sys,os,json,urllib.request
from pathlib import Path
base,target,stage,backup=map(Path,sys.argv[1:])
def refs(text):return re.findall(r'(?:assets/|\./)([A-Za-z0-9_-]+-[A-Za-z0-9_-]{8}\.(?:js|css))',text)
def graph(root):
    todo=refs((root/'index.html').read_text());seen=set()
    while todo:
        f=todo.pop()
        if f in seen:continue
        seen.add(f)
        p=root/'assets'/f
        assert p.is_file() and not p.is_symlink()
        if f.endswith('.js'):todo+=refs(p.read_text())
    return seen
active=graph(base/'web-desktop')
with (backup/'base-assets.sha256').open('x') as f:
    for n in sorted(active):
        p=base/'web-desktop/assets'/n
        f.write(hashlib.sha256(p.read_bytes()).hexdigest()+'  '+str(p)+'\n')
new=graph(stage/'web-desktop')
assert len(active)==len(new)==27,'unexpected graph expansion'
pattern=r'/assets/([A-Za-z0-9_-]+)-[A-Za-z0-9_-]{8}\.(js|css)'
candidate=(stage/'web-desktop/index.html').read_text()
mapping={}
for m in re.finditer(pattern,candidate):
    key=(m[1],m[2]);assert key not in mapping;mapping[key]=m[0]
for surface in ('web','web-desktop'):
    original=(base/surface/'index.html').read_text()
    normalize=lambda t:re.sub(pattern,r'/assets/\1-HASH.\2',t)
    assert normalize(original)==normalize(candidate),'non-asset HTML changes'
    text=re.sub(pattern,lambda m:mapping[(m[1],m[2])],original)
    temp=target/surface/'index.ops-new.html'
    with temp.open('x',encoding='utf-8') as f:f.write(text)
    os.replace(temp,target/surface/'index.html')
    for n in new:
        assert (target/surface/'assets'/n).read_bytes()==(stage/surface/'assets'/n).read_bytes()
    for n in active:
        assert (target/surface/'assets'/n).read_bytes()==(base/surface/'assets'/n).read_bytes(),'old client asset changed'
(backup/'new-assets.json').write_text(json.dumps(sorted(new)),encoding='utf-8')
PY
python3 -B - "$EXPECTED" "$TARGET" "$STAGE" "$BACKUP" <<'PY'
import hashlib,shutil,json,sys,os
from pathlib import Path
base,target,stage,backup=map(Path,sys.argv[1:])
old={}
for p in (base/'web-customer/_app/immutable').rglob('*'):
    if p.is_file():
        assert not p.is_symlink()
        old[p.relative_to(base/'web-customer').as_posix()]=hashlib.sha256(p.read_bytes()).hexdigest()
for p in (stage/'web-customer').rglob('*'):
    if not p.is_file():continue
    name=p.relative_to(stage/'web-customer').as_posix();dest=target/'web-customer'/name
    if name in old:assert old[name]==hashlib.sha256(p.read_bytes()).hexdigest(),'old mobile hash collision'
    dest.parent.mkdir(parents=True,exist_ok=True)
    if name=='index.html':
        temp=dest.with_name('index.commerce-new.html');shutil.copyfile(p,temp);os.replace(temp,dest)
    else:shutil.copyfile(p,dest)
for name,sha in old.items():assert hashlib.sha256((target/'web-customer'/name).read_bytes()).hexdigest()==sha
(backup/'old-mobile-assets.json').write_text(json.dumps(old))
print('Previous mobile immutable assets preserved')
PY
python3 -B - "$EXPECTED" "$TARGET" "$STAGE" "$BACKUP" <<'PY'
import hashlib,shutil,json,sys,os
from pathlib import Path
base,target,stage,backup=map(Path,sys.argv[1:]);old={}
assert 'content="sales-mobile"' in (stage/'web-sales/index.html').read_text()
for p in (base/'web-sales/_app/immutable').rglob('*'):
    if p.is_file():old[p.relative_to(base/'web-sales').as_posix()]=hashlib.sha256(p.read_bytes()).hexdigest()
for p in (stage/'web-sales').rglob('*'):
    if not p.is_file():continue
    name=p.relative_to(stage/'web-sales').as_posix();dest=target/'web-sales'/name
    if name in old:assert old[name]==hashlib.sha256(p.read_bytes()).hexdigest(),'old sales hash collision'
    dest.parent.mkdir(parents=True,exist_ok=True)
    if name=='index.html':
        temp=dest.with_name('index.commerce-new.html');shutil.copyfile(p,temp);os.replace(temp,dest)
    else:shutil.copyfile(p,dest)
for n,sha in old.items():assert hashlib.sha256((target/'web-sales'/n).read_bytes()).hexdigest()==sha
(backup/'old-sales-assets.json').write_text(json.dumps(old))
print('Previous sales mobile assets preserved')
PY
chown -R ecs-user:ecs-user "$TARGET/web" "$TARGET/web-desktop" "$TARGET/web-customer" "$TARGET/web-sales"
find "$TARGET/web" "$TARGET/web-desktop" "$TARGET/web-customer" "$TARGET/web-sales" -type d -exec chmod 755 '{}' +
find "$TARGET/web" "$TARGET/web-desktop" "$TARGET/web-customer" "$TARGET/web-sales" -type f -exec chmod 644 '{}' +
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
        if n.split('/')[0] in ('web','web-desktop','web-customer','web-sales') or n in ('bin/management-service','VERSION','VERSION.json'):continue
        if p.is_symlink():d[n]=('link',os.readlink(p))
        elif p.is_file():d[n]=('file',hashlib.sha256(p.read_bytes()).hexdigest())
        elif p.is_dir():d[n]=('dir',)
        else:raise RuntimeError('special file')
    return d
assert kept(a)==kept(b),'non-target release contents changed'
(backup/'preserved.json').write_text(json.dumps(kept(a),sort_keys=True),encoding='utf-8')
(b/'VERSION').write_text(sys.argv[4]+'\n',encoding='utf-8')
(b/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[4],'base_release':a.name,'scope':'commerce-management-desktop-customer-sales','deployed_at_utc':datetime.now(timezone.utc).isoformat(),'core_restarted':False,'models_selected':False}),encoding='utf-8')
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
sha256sum -c "$BACKUP/base-assets.sha256" >/dev/null
printf '%s  %s\n' "$EXPECTED_BINARY_SHA" "$EXPECTED/bin/management-service" | sha256sum -c -
idle_gate
snapshot "$BACKUP/business-preswitch.tsv"
financial_snapshot "$BACKUP/finance-preswitch.tsv"
cmp "$BACKUP/finance-before.tsv" "$BACKUP/finance-preswitch.tsv"
sha256sum -c "$BACKUP/base-mobile-index.sha256" >/dev/null
sha256sum -c "$BACKUP/base-sales-index.sha256" >/dev/null
cmp "$BACKUP/business-before.tsv" "$BACKUP/business-preswitch.tsv"
cmp /etc/xiaolan/management.env "$BACKUP/management.env"
STOPPED=1
systemctl stop xiaolan-management
idle_gate
[[ $(readlink -f "$ROOT/current") == "$EXPECTED" ]] || fail 'base changed'
ln -s "$TARGET" "$ROOT/.speech-next-$ID"
sha256sum -c "$BACKUP/base-web-index.sha256" >/dev/null
sha256sum -c "$BACKUP/base-assets.sha256" >/dev/null
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
financial_snapshot "$BACKUP/finance-after.tsv"
cmp "$BACKUP/finance-before.tsv" "$BACKUP/finance-after.tsv"
"${MYSQL[@]}" -e "SELECT table_name FROM information_schema.tables WHERE table_schema=DATABASE() ORDER BY table_name" > "$BACKUP/tables-after.tsv"
python3 -B - "$BACKUP" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1]);before=set((p/'tables-before.tsv').read_text().splitlines());after=set((p/'tables-after.tsv').read_text().splitlines())
expected={'mkt_campaign_controls','mkt_campaign_item_options','mkt_claim_locks','mkt_claim_reservations','fin_commerce_rule_heads','fin_commerce_rule_versions','fin_order_reward_snapshots'}
assert before<=after and after-before==expected,'unexpected schema change'
print('Exactly seven additive commerce tables; no existing tables dropped')
PY
for table in mkt_campaign_controls mkt_campaign_item_options fin_commerce_rule_heads fin_commerce_rule_versions; do
  [[ $("${MYSQL[@]}" -e "SELECT COUNT(*) FROM $table") == 0 ]] || fail 'unexpected new rule/activity activation'
done
cmp "$BACKUP/business-before.tsv" "$BACKUP/business-after.tsv"
sha256sum -c "$BACKUP/other-env.sha256" >/dev/null
python3 -B - "$BACKUP/management.env" <<'PY'
import sys
from pathlib import Path
def old_settings(p):
    return [l for l in Path(p).read_text().splitlines() if not l.strip().startswith('MODEL_CONFIG_ENCRYPTION_KEY=')]
assert old_settings(sys.argv[1])==old_settings('/etc/xiaolan/management.env'),'existing management settings changed'
PY
for spec in 'GET /api/v1/finance/commerce-rules' 'POST /api/v1/finance/commerce-rules/drafts' 'POST /api/v1/finance/commerce-rules/1/publish' 'GET /api/v1/sales/commission-wallet' 'POST /api/v1/sales/commission-withdrawals' 'GET /api/v1/finance/sales-withdrawals' 'POST /api/v1/finance/sales-withdrawals/1/pay' 'POST /api/v1/shop/orders/1/free-claim' 'GET /api/v1/rooms/15/customer-contact' 'GET /api/v1/system/speech-models' 'PUT /api/v1/system/speech-models' 'POST /api/v1/system/speech-models/test' 'POST /api/v1/system/speech-models/models'; do
  read -r method path <<< "$spec"
  code=$(curl -sS --max-time 10 -o /dev/null -w '%{http_code}' -X "$method" -H 'Content-Type: application/json' http://127.0.0.1:8080"$path")
  [[ $code == 401 || $code == 403 ]] || fail "permission guard: $spec ($code)"
  echo "Permission guard: $spec -> $code"
done
curl -fsS --max-time 15 -A 'Mozilla/5.0 (Windows NT 10.0; Win64; x64)' https://www.xiaolandaizi.cn/ | cmp - "$TARGET/web-desktop/index.html"
python3 -B - "$BACKUP/new-assets.json" "$TARGET" <<'PY'
import hashlib,json,sys,urllib.request
from pathlib import Path
root=Path(sys.argv[2])
for n in json.load(open(sys.argv[1])):
    request=urllib.request.Request('https://www.xiaolandaizi.cn/assets/'+n,headers={'User-Agent':'Mozilla/5.0 (Windows NT 10.0; Win64; x64)'})
    with urllib.request.urlopen(request,timeout=15) as r:data=r.read()
    assert hashlib.sha256(data).digest()==hashlib.sha256((root/'web-desktop/assets'/n).read_bytes()).digest(),'public asset mismatch '+n
print('Public desktop assets verified')
PY
MOBILE_UA='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
curl -fsS --max-time 15 -A "$MOBILE_UA" https://www.xiaolandaizi.cn/shop | cmp - "$TARGET/web-customer/index.html"
curl -fsS --max-time 15 https://sales.xiaolandaizi.cn/performance | cmp - "$TARGET/web-sales/index.html"
python3 -B - "$MANIFEST" "$TARGET" "$BACKUP" <<'PY'
import hashlib,json,sys,urllib.request,concurrent.futures
from pathlib import Path
manifest,target,backup=sys.argv[1:];root=Path(target)
ua='Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Version/18.0 Mobile/15E148 Safari/604.1'
files=[r for r in json.load(open(manifest))['packaged_files'] if r['path'].startswith(('web-customer/_app/immutable/','web-sales/_app/immutable/'))]
def verify(r):
    name=r['path'];surface,relative=name.split('/',1)
    origin='https://sales.xiaolandaizi.cn/' if surface=='web-sales' else 'https://www.xiaolandaizi.cn/'
    req=urllib.request.Request(origin+relative,headers={'User-Agent':ua})
    with urllib.request.urlopen(req,timeout=20) as v:data=v.read()
    assert hashlib.sha256(data).hexdigest()==r['sha256'],'public mobile asset mismatch '+relative
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:list(pool.map(verify,files))
old=json.load(open(Path(backup)/'old-mobile-assets.json'))
for n,sha in old.items():assert hashlib.sha256((root/'web-customer'/n).read_bytes()).hexdigest()==sha
old_sales=json.load(open(Path(backup)/'old-sales-assets.json'))
for n,sha in old_sales.items():assert hashlib.sha256((root/'web-sales'/n).read_bytes()).hexdigest()==sha
print('Public customer/sales mobile assets verified; old client assets preserved')
PY
idle_gate
trap - ERR INT TERM
printf 'release_id=%s\nbase=%s\nscope=commerce-management-desktop-customer-sales\ncore_restarted=false\ndevice_restarted=false\nmodel_default_changed=false\nroom_configuration_changed=false\nbackup=%s\n' "$ID" "$EXPECTED" "$BACKUP" > "$ROOT/deployments/$ID.txt"
echo "DEPLOYED=$ID; original models/rooms/financial rules preserved; seven additive tables; no new commission rate activated; backup=$BACKUP"
