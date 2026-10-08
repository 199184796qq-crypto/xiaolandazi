#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:?risk confirmation archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
EXPECTED_SHA256="${4:?archive sha256 required}"
ROOT=/opt/xiaolan

[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ ]] || { echo 'invalid release id' >&2; exit 2; }
[[ "$EXPECTED_SHA256" =~ ^[a-f0-9]{64}$ ]] || { echo 'invalid sha256' >&2; exit 2; }
[[ -f "$ARCHIVE" ]] || { echo 'release archive not found' >&2; exit 2; }
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || { echo 'invalid base release' >&2; exit 2; }
if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$EXPECTED_SHA256"
fi

[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$EXPECTED_SHA256" ]] || { echo 'archive sha256 mismatch' >&2; exit 3; }
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'current release changed; recheck before publishing' >&2; exit 3; }

assert_management_idle() {
  local active
  active=$(bash -c 'set -e; set -a; source /etc/xiaolan/management.env; set +a; MYSQL_PWD="$DB_PASSWORD" mysql --connect-timeout=8 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --skip-column-names -e "SELECT COUNT(*) FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN (\"running\",\"paused\",\"starting\",\"resuming\")"')
  [[ "$active" == 0 ]] || { echo 'live runtime session active; management restart deferred' >&2; exit 4; }
}
assert_management_idle

python3 - "$ARCHIVE" <<'PY'
import pathlib
import sys
import tarfile

required = {
    'bin/management-service',
    'web/index.html',
    'web-desktop/index.html',
    'configs/prompts/anchor-style/MANIFEST.json',
    'configs/prompts/anchor-style/compliance/L1/p0-hard-block.md',
    'configs/fact-categories.json',
}
files = set()
with tarfile.open(sys.argv[1]) as archive:
    for item in archive.getmembers():
        path = pathlib.PurePosixPath(item.name)
        if path.is_absolute() or '..' in path.parts or not (item.isfile() or item.isdir()):
            raise SystemExit('unsafe archive member')
        normalized = pathlib.PurePosixPath(*[part for part in path.parts if part != '.']).as_posix()
        if item.isfile():
            if not (
                normalized == 'bin/management-service'
                or normalized.startswith('web/')
                or normalized.startswith('web-desktop/')
                or normalized.startswith('configs/prompts/anchor-style/')
                or normalized == 'configs/fact-categories.json'
            ):
                raise SystemExit('archive contains out-of-scope file')
            files.add(normalized)
if not required.issubset(files):
    raise SystemExit('archive is missing required risk confirmation assets')
PY

AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 536870912 )) || { echo 'insufficient free space; prior releases retained' >&2; exit 4; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || { echo 'release already exists' >&2; exit 3; }
mkdir "$TARGET"
cp -a --reflink=auto "$EXPECTED/." "$TARGET/"
# Keep the base release's immutable hashed assets. An already-open browser
# still imports its old lazy chunks after index.html is replaced. Clearing the
# directories makes otherwise-authorized navigation fail with HTTP 404.
# The retired-workbench checks below still reject an incompatible base release.
tar -xzf "$ARCHIVE" -C "$TARGET"

chmod 0755 "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
cmp "$EXPECTED/web-customer/index.html" "$TARGET/web-customer/index.html"
cmp "$EXPECTED/web-sales/index.html" "$TARGET/web-sales/index.html"
# Preserve separately deployed presentation overlays (e.g. device controls).
# The current entry document uses the new application CSS and JavaScript;
# old hashed files remain available only for browser tabs using those versions.
python3 - "$EXPECTED" "$TARGET" <<'PY'
import pathlib,re,shutil,sys
base,target=map(pathlib.Path,sys.argv[1:])
for surface in ('web','web-desktop'):
    old=(base/surface/'index.html').read_text(encoding='utf-8')
    path=target/surface/'index.html'
    new=path.read_text(encoding='utf-8')
    for tag in re.findall(r'<link\b[^>]*>',old):
        match=re.search(r'href="(/assets/[^"?]+\.css)"',tag)
        if not match or re.match(r'/assets/index-[A-Za-z0-9_-]+\.css$',match[1]):
            continue
        rel=pathlib.PurePosixPath(match[1].lstrip('/'))
        if '..' in rel.parts:
            raise SystemExit('unsafe overlay stylesheet')
        source=(base/surface/str(rel)).resolve()
        if (base/surface).resolve() not in source.parents or not source.is_file():
            raise SystemExit('overlay stylesheet missing or unsafe')
        destination=target/surface/str(rel)
        destination.parent.mkdir(parents=True,exist_ok=True)
        shutil.copy2(source,destination)
        if match[1] not in new:
            new=new.replace('</head>',tag+'\n</head>')
    path.write_text(new,encoding='utf-8')
PY
cmp "$TARGET/web/index.html" "$TARGET/web-desktop/index.html"

python3 - "$TARGET/configs/prompts/anchor-style" <<'PY'
import hashlib
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1]).resolve()
manifest = json.loads((root / 'MANIFEST.json').read_text(encoding='utf-8-sig'))
expected = set()
for item in manifest:
    path = (root / item['path']).resolve()
    if root not in path.parents or path == root / 'MANIFEST.json' or not path.is_file():
        raise SystemExit('unsafe prompt manifest path')
    if hashlib.sha256(path.read_bytes()).hexdigest() != item['sha256']:
        raise SystemExit('prompt mother hash mismatch: ' + item['path'])
    expected.add(item['path'])
actual = {path.relative_to(root).as_posix() for path in root.rglob('*') if path.is_file() and path != root / 'MANIFEST.json'}
if actual != expected:
    raise SystemExit('prompt mother manifest is incomplete')
PY

for surface in web web-desktop; do
  if find "$TARGET/$surface/assets" -maxdepth 1 -type f -name 'AnchorStyleWorkbenchView-*' -print -quit | grep -q .; then
    echo "retired workbench chunk remains in $surface" >&2
    exit 4
  fi
  if grep -aRIl -e '主播口播调教台' -e '/operations/live/style-workbench' -e 'AnchorStyleWorkbenchView' "$TARGET/$surface" | grep -q .; then
    echo "retired workbench marker remains in $surface" >&2
    exit 4
  fi
done

grep -q '"policy_version": "2026-10-08-p0-v3"' "$TARGET/configs/prompts/anchor-style/compliance/L1/p0-hard-block.md"

python3 - "$TARGET/configs/fact-categories.json" <<'PY'
import json,sys
data=json.load(open(sys.argv[1],encoding='utf-8-sig'))
items=data['categories']
values={item['value'] for item in items}
if data['schema_version']!=1 or len(values)!=len(items) or not {'product','link','trade','fulfillment','identity_location','other'}.issubset(values):
    raise SystemExit('invalid fact category catalog')
PY

python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" <<'PY'
import datetime
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
metadata = {
    'release_id': sys.argv[2],
    'base_release': sys.argv[3],
    'scope': 'risk-confirmation-management-web-prompts',
    'service_restarted': ['xiaolan-management.service'],
    'deployed_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
}
(root / 'VERSION.json').write_text(json.dumps(metadata, ensure_ascii=False), encoding='utf-8')
(root / 'VERSION').write_text(sys.argv[2] + '\n', encoding='ascii')
PY
chown -R ecs-user:ecs-user "$TARGET"

[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'concurrent release detected; not activated' >&2; exit 3; }
assert_management_idle
rollback() {
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  systemctl restart xiaolan-management.service || true
  echo "rolled back to $EXPECTED" >&2
}
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"
if ! systemctl restart xiaolan-management.service; then rollback; exit 5; fi

HEALTHY=false
for _ in $(seq 1 30); do
  if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; then HEALTHY=true; break; fi
  sleep 1
done
if [[ "$HEALTHY" != true ]]; then rollback; exit 6; fi
if ! curl -fsS --max-time 10 https://www.xiaolandaizi.cn/ >/dev/null; then rollback; exit 6; fi

mkdir -p "$ROOT/deployments"
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=risk-confirmation-management-web-prompts\nsha256=%s\n' \
  "$RELEASE_ID" "$EXPECTED" "$TARGET" "$EXPECTED_SHA256" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "risk confirmation deploy ok: $TARGET (management restarted; Core/Xiaozhi unchanged)"
