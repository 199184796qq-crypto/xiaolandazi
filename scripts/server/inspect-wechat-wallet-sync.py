#!/usr/bin/env python3
"""Read-only refund #2 reconciliation; excludes credentials and payer details."""
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys

try:
    if os.geteuid() != 0:
        raise ValueError('root required')
    values = {}
    for line in Path('/etc/xiaolan/management.env').read_text().splitlines():
        if line.lstrip().startswith('#') or '=' not in line:
            continue
        name, raw = line.split('=', 1)
        name = name.strip()
        if name not in {'DB_HOST', 'DB_PORT', 'DB_NAME', 'DB_USER', 'DB_PASSWORD'}:
            continue
        parts = shlex.split(raw, comments=False)
        if name in values or len(parts) != 1:
            raise ValueError('invalid setting')
        values[name] = parts[0]
    sql = """START TRANSACTION READ ONLY;
SELECT r.id,r.amount_cents,
 COALESCE(SUM(IF(i.status='success',i.amount_cents,0)),0),
 COALESCE(SUM(IF(i.status NOT IN ('success','closed'),i.amount_cents,0)),0),
 w.balance_cents,w.frozen_balance_cents,COUNT(i.id),
 COALESCE(GROUP_CONCAT(DISTINCT i.status ORDER BY i.status SEPARATOR ','),'')
FROM fin_wechat_cash_refunds r
JOIN fin_wallet_accounts w ON w.id=r.wallet_account_id
LEFT JOIN fin_wechat_cash_refund_items i ON i.request_id=r.id
WHERE r.id=2
GROUP BY r.id,r.amount_cents,w.balance_cents,w.frozen_balance_cents;
COMMIT;"""
    response = subprocess.run(['mysql', '--protocol=TCP', '--connect-timeout=10', '--batch', '--skip-column-names',
        '--host=' + values['DB_HOST'], '--port=' + values['DB_PORT'], '--user=' + values['DB_USER'], '--database=' + values['DB_NAME']],
        input=sql, text=True, capture_output=True, timeout=20, env=dict(os.environ, MYSQL_PWD=values['DB_PASSWORD']))
    fields = response.stdout.strip().split('\t')
    if response.returncode or len(fields) != 8:
        raise ValueError('query failed')
    names = ['refund_id', 'amount_cents', 'refunded_cents', 'pending_cents', 'available_cash_cents', 'frozen_cash_cents', 'item_count']
    report = dict(zip(names, map(int, fields[:7])))
    report.update(item_statuses=fields[7], read_only=True, payer_details_printed=False)
    print(json.dumps(report))
except Exception:
    print('Read-only wallet reconciliation failed; no sensitive diagnostics printed', file=sys.stderr)
    sys.exit(1)
