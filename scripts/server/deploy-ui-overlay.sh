#!/usr/bin/env bash
set -euo pipefail

CSS="${1:?css file required}"
JS="${2:?javascript file required}"
RELEASE_ID="${3:?release id required}"
EXPECTED="${4:?expected current release required}"
CSS_SHA="${5:?css sha256 required}"
JS_SHA="${6:?javascript sha256 required}"
ROOT=/opt/xiaolan

[[ "$RELEASE_ID" =~ ^[A-Za-z0-9._-]+$ ]] || exit 2
[[ "$CSS_SHA" =~ ^[a-f0-9]{64}$ && "$JS_SHA" =~ ^[a-f0-9]{64}$ ]] || exit 2
[[ -f "$CSS" && -f "$JS" ]] || exit 2
[[ "$EXPECTED" == "$ROOT/releases/"* && -d "$EXPECTED" ]] || exit 2
if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$0" "$CSS" "$JS" "$RELEASE_ID" "$EXPECTED" "$CSS_SHA" "$JS_SHA"
fi

[[ "$(sha256sum "$CSS" | cut -d' ' -f1)" == "$CSS_SHA" ]] || exit 3
[[ "$(sha256sum "$JS" | cut -d' ' -f1)" == "$JS_SHA" ]] || exit 3
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3

TARGET="$ROOT/releases/$RELEASE_ID"
[[ ! -e "$TARGET" ]] || exit 3
mkdir "$TARGET"
[[ "$(readlink -f "$TARGET")" == "$TARGET" ]] || exit 3
cp -al "$EXPECTED/." "$TARGET/"

for FILE in web/index.html web-desktop/index.html VERSION VERSION.json; do
  [[ "$(readlink -f "$TARGET/$FILE")" == "$TARGET/$FILE" && -f "$TARGET/$FILE" ]] || exit 3
  rm -- "$TARGET/$FILE"
done

for SURFACE in web web-desktop; do
  [[ ! -e "$TARGET/$SURFACE/assets/$RELEASE_ID.css" ]] || exit 3
  [[ ! -e "$TARGET/$SURFACE/assets/$RELEASE_ID.js" ]] || exit 3
  install -o ecs-user -g ecs-user -m 0644 "$CSS" "$TARGET/$SURFACE/assets/$RELEASE_ID.css"
  install -o ecs-user -g ecs-user -m 0644 "$JS" "$TARGET/$SURFACE/assets/$RELEASE_ID.js"
done

python3 - "$TARGET" "$RELEASE_ID" "$EXPECTED" <<'PY'
import datetime, json, pathlib, sys

root = pathlib.Path(sys.argv[1])
release = sys.argv[2]
previous = pathlib.Path(sys.argv[3])
addition = (
    f'<link rel="stylesheet" href="/assets/{release}.css">\n'
    f'<script defer src="/assets/{release}.js"></script>\n'
)
for surface in ('web', 'web-desktop'):
    html = (previous / surface / 'index.html').read_text(encoding='utf-8')
    if html.count('</head>') != 1:
        raise SystemExit('unexpected entry document')
    (root / surface / 'index.html').write_text(html.replace('</head>', addition + '</head>'), encoding='utf-8')
(root / 'VERSION.json').write_text(json.dumps({
    'release_id': release,
    'base_release': str(previous),
    'scope': 'scoped-ui-overlay',
    'service_restarted': False,
    'deployed_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
}, ensure_ascii=False), encoding='utf-8')
(root / 'VERSION').write_text(release + '\n', encoding='ascii')
PY

chown ecs-user:ecs-user "$TARGET" "$TARGET/web/index.html" "$TARGET/web-desktop/index.html" "$TARGET/VERSION" "$TARGET/VERSION.json"
[[ "$(readlink -f "$ROOT/current")" == "$EXPECTED" ]] || exit 3
ln -s "$TARGET" "$ROOT/.current-$RELEASE_ID"
mv -Tf "$ROOT/.current-$RELEASE_ID" "$ROOT/current"

PUBLIC_HTML="$(curl -fsS --max-time 10 https://www.xiaolandaizi.cn/)" || {
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  exit 4
}
if [[ "$PUBLIC_HTML" != *"$RELEASE_ID.css"* || "$PUBLIC_HTML" != *"$RELEASE_ID.js"* ]]; then
  ln -s "$EXPECTED" "$ROOT/.rollback-$RELEASE_ID"
  mv -Tf "$ROOT/.rollback-$RELEASE_ID" "$ROOT/current"
  exit 4
fi

echo "UI overlay deploy ok: $TARGET (no service restart)"
