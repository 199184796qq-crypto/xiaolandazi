#!/usr/bin/env bash
set -euo pipefail
ARCHIVE="${1:?archive required}"
RELEASE_ID="${2:?release id required}"
EXPECTED="${3:?expected current required}"
SHA="${4:?sha256 required}"
ROOT=/opt/xiaolan
[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ && "$SHA" =~ ^[a-f0-9]{64}$ && -f "$ARCHIVE" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || exit 2
if [[ "$(id -u)" -ne 0 ]]; then exec sudo -n bash "$0" "$ARCHIVE" "$RELEASE_ID" "$EXPECTED" "$SHA"; fi
[[ "$(sha256sum "$ARCHIVE" | cut -d' ' -f1)" == "$SHA" ]] || exit 3
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || { echo 'current changed; recheck before publishing'; exit 3; }
# Backend was deployed by the preceding release. Verify its contract and schema
# before publishing a frontend that depends on it; never rebuild concurrent work.
for marker in '主播样本已变化，本次仅保存单样本分析' '单样本分析已保存，总风格未改变' 'POST /api/v1/live-anchor-styles/{styleID}/analyze-multiple' '请通过综合风格版本编辑或切换风格，不可直接覆盖'; do
  grep -aqF "$marker" "$EXPECTED/bin/management-service" || { echo 'required backend contract missing' >&2; exit 4; }
done
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
SCHEMA_OK=$(mysql --connect-timeout=10 -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" -NBe "SELECT (SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_name IN ('live_anchor_style_analysis_versions','live_anchor_style_analysis_activations'))+(SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='live_anchor_styles' AND column_name='active_analysis_version_id');" 2>/dev/null) || { echo 'schema verification failed' >&2; exit 4; }
unset MYSQL_PWD
[[ "$SCHEMA_OK" == 3 ]] || { echo 'required analysis schema missing' >&2; exit 4; }
test -f "$EXPECTED/configs/prompts/anchor-style/style-multi-sample-fusion.md"
test -f "$EXPECTED/configs/prompts/anchor-style/scorecard-effective-style-integration.md"

AVAILABLE=$(df -B1 --output=avail "$ROOT" | tail -1 | tr -d ' ')
(( AVAILABLE > 134217728 )) || { echo 'insufficient free space; prior releases retained'; exit 4; }
TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || exit 3
python3 - "$ARCHIVE" <<'PY'
import pathlib,sys,tarfile
with tarfile.open(sys.argv[1]) as archive:
    files=set()
    for item in archive.getmembers():
        p=pathlib.PurePosixPath(item.name)
        if p.is_absolute() or '..' in p.parts or not (item.isfile() or item.isdir()) or p.parts[0] not in ('web','web-desktop'):
            raise SystemExit('unsafe or out-of-scope archive member')
        if item.isfile():
            if len(p.parts)<2 or not (p.parts[1]=='assets' or str(p) in ('web/index.html','web-desktop/index.html')):
                raise SystemExit('only compiled assets and entry documents allowed')
            files.add(str(p))
    if not {'web/index.html','web-desktop/index.html'}.issubset(files): raise SystemExit('missing entry document')
    for kind in ('male','female','middle_aged_male','middle_aged_female'):
        for surface in ('web','web-desktop'):
            if f'{surface}/assets/anchor-avatars/{kind}-v1.png' not in files: raise SystemExit('missing avatar')
PY
mkdir "$TARGET"
cp -al "$EXPECTED/." "$TARGET/"
# Remove only replacement hardlinks in the new release before extraction.
# No prior release or original asset is modified or deleted.
python3 - "$ARCHIVE" "$TARGET" <<'PY'
import pathlib,sys,tarfile
root=pathlib.Path(sys.argv[2]).resolve()
with tarfile.open(sys.argv[1]) as archive:
    paths=[root/item.name for item in archive.getmembers() if item.isfile()]
paths += [root/'VERSION',root/'VERSION.json']
for path in paths:
    if path.is_symlink() or not path.resolve().is_relative_to(root): raise SystemExit('replacement path escapes new release')
    if path.exists():
        if not path.is_file(): raise SystemExit('replacement target is not a file')
        path.unlink()
PY
tar -xzf "$ARCHIVE" -C "$TARGET"
cmp "$EXPECTED/bin/management-service" "$TARGET/bin/management-service"
cmp "$EXPECTED/bin/core-service" "$TARGET/bin/core-service"
cmp "$EXPECTED/bin/xiaozhi-gateway" "$TARGET/bin/xiaozhi-gateway"
diff -qr "$EXPECTED/configs" "$TARGET/configs"
cmp "$TARGET/web/index.html" "$TARGET/web-desktop/index.html"
python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" "$ARCHIVE" <<'PY'
import datetime,json,pathlib,sys,tarfile,os,pwd
root=pathlib.Path(sys.argv[1]);release=sys.argv[2]
(root/'VERSION.json').write_text(json.dumps({'release_id':release,'base_release':sys.argv[3],'scope':'anchor-style-analysis-versions-web-only','service_restarted':False,'deployed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()},ensure_ascii=False),encoding='utf-8')
(root/'VERSION').write_text(release+'\n',encoding='ascii')
with tarfile.open(sys.argv[4]) as archive:
    user=pwd.getpwnam('ecs-user')
    for item in archive.getmembers():
        if item.isfile(): os.chown(root/item.name,user.pw_uid,user.pw_gid)
PY
chown ecs-user:ecs-user "$TARGET" "$TARGET/VERSION" "$TARGET/VERSION.json"
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"
if ! curl -fsS --max-time 10 https://www.xiaolandaizi.cn/ >/dev/null; then
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  exit 5
fi
echo "anchor style versions deploy ok: $TARGET (no service restart; all backend/configs retained)"
