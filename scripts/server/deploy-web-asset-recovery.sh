#!/usr/bin/env bash
set -euo pipefail

MANIFEST="${1:?asset recovery manifest required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
MANIFEST_SHA="${4:?manifest sha256 required}"
ROOT=/opt/xiaolan
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$RELEASE_ID" != *..* ]] || exit 2
[[ "$MANIFEST_SHA" =~ ^[a-f0-9]{64}$ && -f "$MANIFEST" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || exit 2
if [[ $(id -u) != 0 ]]; then exec sudo -n bash "$0" "$@"; fi
[[ "$(sha256sum "$MANIFEST" | cut -d' ' -f1)" == "$MANIFEST_SHA" ]] || exit 3
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'current changed; recheck before publishing'; exit 3; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || { echo 'release already exists'; exit 3; }

# Verify against the original published files before creating the candidate.
# Only add absent immutable assets; never replace the entry document, an
# existing asset, a service binary, a configuration, or a mobile surface.
python3 - "$MANIFEST" "$EXPECTED" <<'PY'
import hashlib,json,pathlib,re,sys
manifest,base=map(pathlib.Path,sys.argv[1:]);root=pathlib.Path('/opt/xiaolan/releases')
items=json.loads(manifest.read_text(encoding='utf-8-sig'));seen=set()
if not items:raise SystemExit('empty recovery manifest')
for item in items:
 name=item['name'];release=item['source_release'];digest=item['sha256']
 if not re.fullmatch(r'[A-Za-z0-9_.-]+',name) or name in seen:raise SystemExit('unsafe or duplicate asset name')
 if not re.fullmatch(r'[A-Za-z0-9_.-]+',release) or '..' in release:raise SystemExit('unsafe source release')
 if not re.fullmatch(r'[a-f0-9]{64}',digest):raise SystemExit('invalid asset checksum')
 seen.add(name);source=(root/release/'web/assets'/name).resolve()
 if root.resolve() not in source.parents or not source.is_file():raise SystemExit('missing original asset: '+name)
 data=source.read_bytes()
 if hashlib.sha256(data).hexdigest()!=digest:raise SystemExit('original checksum mismatch: '+name)
 if any(marker in data for marker in ['主播口播调教台'.encode(),b'/operations/live/style-workbench',b'AnchorStyleWorkbenchView']):raise SystemExit('retired workbench asset: '+name)
 for surface in ('web','web-desktop'):
  existing=base/surface/'assets'/name
  if existing.exists() and (not existing.is_file() or hashlib.sha256(existing.read_bytes()).hexdigest()!=digest):raise SystemExit('existing asset collision: '+name)
print('original_assets_verified='+str(len(seen)))
PY
mkdir "$TARGET"
# Hard links are safe here: inherited files are read-only and never overwritten.
cp -al "$EXPECTED/." "$TARGET/"
python3 - "$MANIFEST" "$TARGET" "$RELEASE_ID" "$EXPECTED" "$MANIFEST_SHA" <<'PY'
import datetime,hashlib,json,pathlib,shutil,sys
manifest,target=map(pathlib.Path,sys.argv[1:3]);root=pathlib.Path('/opt/xiaolan/releases')
items=json.loads(manifest.read_text(encoding='utf-8-sig'));added=[]
for item in items:
 source=root/item['source_release']/'web/assets'/item['name']
 for surface in ('web','web-desktop'):
  dest=target/surface/'assets'/item['name']
  if dest.exists():continue
  shutil.copy2(source,dest)
  if hashlib.sha256(dest.read_bytes()).hexdigest()!=item['sha256']:raise SystemExit('copy checksum mismatch')
  added.append(surface+'/assets/'+item['name'])
for name in ('VERSION','VERSION.json'):
 p=target/name
 if p.exists():p.unlink() # Break inherited hard links before changing metadata.
data={'release_id':sys.argv[3],'base_release':sys.argv[4],'scope':'historical-web-assets-only','service_restarted':False,'manifest_sha256':sys.argv[5],'added_assets':added,'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
(target/'VERSION.json').write_text(json.dumps(data,ensure_ascii=False),encoding='utf-8')
(target/'VERSION').write_text(sys.argv[3]+'\n',encoding='ascii')
print('assets_added='+str(len(added)))
PY
for file in bin/core-service bin/management-service bin/xiaozhi-gateway web/index.html web-desktop/index.html; do cmp "$EXPECTED/$file" "$TARGET/$file"; done
diff -qr "$EXPECTED/configs" "$TARGET/configs" >/dev/null
diff -qr "$EXPECTED/web-customer" "$TARGET/web-customer" >/dev/null
diff -qr "$EXPECTED/web-sales" "$TARGET/web-sales" >/dev/null
chown ecs-user:ecs-user "$TARGET" "$TARGET/VERSION" "$TARGET/VERSION.json"
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'concurrent release; not activated'; exit 3; }
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"
if ! curl -fsS --max-time 10 https://www.xiaolandaizi.cn/ >/dev/null; then
  # Do not roll back a concurrent publisher that has already moved current.
  if [[ "$(readlink -f "$ROOT/current")" == "$TARGET" ]]; then
    ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
    mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  fi
  exit 4
fi
mkdir -p "$ROOT/deployments"
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=historical-web-assets-only\nservices_restarted=false\n' "$RELEASE_ID" "$EXPECTED" "$TARGET" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "historical asset recovery ok: $TARGET (entry documents and services unchanged)"
