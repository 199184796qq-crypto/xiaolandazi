#!/usr/bin/env bash
set -euo pipefail

ARCHIVE="${1:?frontend archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current release required}"
ROOT=/opt/xiaolan
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && -f "$ARCHIVE" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || exit 2
if [[ "$(id -u)" -ne 0 ]]; then exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED"; fi
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'current changed; rebuild/recheck before publishing' >&2; exit 3; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || exit 3
mkdir "$TARGET"
cp -a --reflink=auto "$EXPECTED/." "$TARGET/"
# Keep previously hashed assets for old browser tabs. index.html is replaced
# only in the new checkout; no in-place modification or service restart.
tar -xzf "$ARCHIVE" -C "$TARGET/web"
tar -xzf "$ARCHIVE" -C "$TARGET/web-desktop"
python3 - "$EXPECTED" "$TARGET" <<'PY'
import pathlib,re,sys
base,target=map(pathlib.Path,sys.argv[1:])
for surface in ('web','web-desktop'):
    old=(base/surface/'index.html').read_text(encoding='utf-8')
    path=target/surface/'index.html'
    new=path.read_text(encoding='utf-8')
    for tag in re.findall(r'<link\b[^>]*>',old):
        match=re.search(r'href="(/assets/[^"?]+\.css)"',tag)
        if not match or re.match(r'/assets/index-[A-Za-z0-9_-]+\.css$',match[1]): continue
        rel=pathlib.PurePosixPath(match[1].lstrip('/'))
        if '..' in rel.parts or not (target/surface/str(rel)).is_file():
            raise SystemExit('missing or unsafe overlay stylesheet')
        if match[1] not in new: new=new.replace('</head>',tag+'\n</head>')
    path.write_text(new,encoding='utf-8')
PY
grep -q 'name="xiaolan-app" content="desktop"' "$TARGET/web/index.html"
cmp "$TARGET/web/index.html" "$TARGET/web-desktop/index.html"
python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" <<'PY'
import datetime,json,pathlib,sys
root=pathlib.Path(sys.argv[1])
(root/'VERSION.json').write_text(json.dumps({'release_id':sys.argv[2],'base_release':sys.argv[3],'scope':'desktop-web-only','deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},ensure_ascii=False),encoding='utf-8')
(root/'VERSION').write_text(sys.argv[2]+'\n',encoding='ascii')
PY
chown -R ecs-user:ecs-user "$TARGET"
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'concurrent release detected; not activated' >&2; exit 3; }
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"
if ! curl -fsS --max-time 10 https://www.xiaolandaizi.cn/ >/dev/null; then
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  echo 'public page check failed; previous release restored' >&2; exit 4
fi
mkdir -p "$ROOT/deployments"
printf 'release_id=%s\nprevious=%s\ncurrent=%s\nscope=desktop-web-only\n' "$RELEASE_ID" "$EXPECTED" "$TARGET" > "$ROOT/deployments/$RELEASE_ID.txt"
echo "web deploy ok: $TARGET (services not restarted)"
