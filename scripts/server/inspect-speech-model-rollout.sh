#!/usr/bin/env bash
set -euo pipefail
set -a
source /etc/xiaolan/management.env
set +a
export MYSQL_PWD="$DB_PASSWORD"
mysql -h "$DB_HOST" -P "${DB_PORT:-3306}" -u "$DB_USER" "$DB_NAME" --batch --default-character-set=utf8mb4 <<'SQL'
SELECT id,tenant_id,name,status,monitor_enabled FROM core_rooms WHERE monitor_enabled=1 OR LOWER(status) NOT IN ('pending','stopped','offline','idle','failed','error','disabled','');
SELECT id,tenant_id,room_id,status,started_at,ended_at FROM live_runtime_sessions WHERE ended_at IS NULL OR LOWER(status) IN ('running','paused','starting','start','resuming');
SELECT JSON_LENGTH(config_json,'$.profiles') AS model_count,JSON_UNQUOTE(JSON_EXTRACT(config_json,'$.default_id')) AS selected_default,JSON_EXTRACT(config_json,'$.revision') AS revision,JSON_LENGTH(keys_json) AS credential_count FROM mgmt_anchor_model_settings WHERE id=1;
SQL
python3 -B - <<'PY'
import json,os,subprocess,urllib.request
from pathlib import Path
pid=subprocess.check_output(['systemctl','show','-p','MainPID','--value','xiaolan-management'],text=True).strip()
env=dict(p.split(b'=',1) for p in Path('/proc/'+pid+'/environ').read_bytes().split(b'\0') if b'=' in p)
print('Running management encryption key ready:',len(env.get(b'MODEL_CONFIG_ENCRYPTION_KEY',b''))>=32)
def get(b,p):
    r=urllib.request.Request(b+p,headers={'X-Core-Token':os.environ['CORE_INTERNAL_TOKEN']})
    with urllib.request.urlopen(r,timeout=8) as v:return json.load(v)
for b in (os.environ.get('CORE_BASE_URLS') or os.environ.get('CORE_BASE_URL') or 'http://127.0.0.1:8081').split(','):
    b=b.strip().rstrip('/')
    for r in get(b,'/internal/v1/rooms')['items']:
        p=f"/internal/v1/rooms/{int(r['id'])}";q=f"?tenant_id={int(r['tenant_id'])}"
        s=get(b,p+'/speech-runtime'+q);program=s.get('program') or {}
        engine=get(b,p+'/audio-engine'+q)
        print(json.dumps({'room_id':r['id'],'name':r.get('name'),'status':r.get('status'),'monitor':r.get('monitor_enabled'),'program_running':program.get('running'),'program_suspended':program.get('suspended'),'mainline':(s.get('mainline') or {}).get('status'),'interrupt':(s.get('interrupt') or {}).get('status'),'audio_phase':engine.get('phase')},ensure_ascii=False))
PY
