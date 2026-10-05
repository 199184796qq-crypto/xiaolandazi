#!/usr/bin/env python3
"""Read-only production schema/aggregate check. Never display credentials or rows."""
import json
import os
from pathlib import Path
import shlex
import subprocess
import sys


def verify():
    if os.geteuid() != 0:
        raise ValueError('root required')
    values = {}
    for line in Path('/etc/xiaolan/management.env').read_text().splitlines():
        if not line.strip() or line.lstrip().startswith('#') or '=' not in line:
            continue
        name, raw = line.split('=', 1)
        name = name.strip()
        if name not in {'DB_HOST', 'DB_PORT', 'DB_NAME', 'DB_USER', 'DB_PASSWORD'}:
            continue
        parts = shlex.split(raw, comments=False, posix=True)
        if name in values or len(parts) != 1:
            raise ValueError('invalid DB setting')
        values[name] = parts[0]
    if len(values) != 5:
        raise ValueError('missing DB setting')
    require_native = sys.argv[1:] == ['--require-native']
    if sys.argv[1:] and not require_native:
        raise ValueError('invalid verification arguments')
    sql = """
START TRANSACTION READ ONLY;
SELECT COUNT(*) FROM information_schema.TABLES
 WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME IN ('fin_wechat_cash_refunds','fin_wechat_cash_refund_items');
SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()
 AND TABLE_NAME='fin_wechat_cash_refund_items'
 AND COLUMN_NAME IN ('submitted_at','lease_until','lease_token','next_check_at');
SELECT COUNT(*) FROM fin_wechat_cash_refunds;
SELECT COUNT(*) FROM fin_wechat_cash_refund_items WHERE status NOT IN ('success','closed');
COMMIT;
"""
    if require_native:
        sql = sql.replace('COMMIT;', """
SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE()
 AND TABLE_NAME='fin_payment_transactions' AND COLUMN_NAME='provider_code_url'
 AND DATA_TYPE='varchar' AND CHARACTER_MAXIMUM_LENGTH=512;
COMMIT;
""")
    process_env = dict(os.environ, MYSQL_PWD=values['DB_PASSWORD'])
    result = subprocess.run([
        'mysql', '--protocol=TCP', '--connect-timeout=10', '--batch', '--skip-column-names',
        '--host=' + values['DB_HOST'], '--port=' + values['DB_PORT'],
        '--user=' + values['DB_USER'], '--database=' + values['DB_NAME'],
    ], input=sql, text=True, capture_output=True, timeout=20, env=process_env)
    if result.returncode != 0:
        raise ValueError('read-only schema query failed')
    rows = result.stdout.strip().splitlines()
    if len(rows) != (5 if require_native else 4) or any(not row.isdigit() for row in rows):
        raise ValueError('unexpected schema result')
    tables, worker_columns, requests, pending = map(int, rows[:4])
    if tables != 2 or worker_columns != 4:
        raise ValueError('refund schema incomplete')
    if require_native and rows[4] != '1':
        raise ValueError('Native schema incomplete')
    print(json.dumps({
        'refund_tables': tables, 'worker_columns': worker_columns,
        'refund_request_count': requests, 'nonterminal_refund_item_count': pending,
        'read_only': True, 'sensitive_values_printed': False,
        'native_code_url_column': int(rows[4]) if require_native else None,
    }))


if __name__ == '__main__':
    try:
        verify()
    except Exception:
        # Do not print exceptions/subprocess stderr which could contain settings.
        print('Refund schema verification failed; no diagnostic credentials printed', file=sys.stderr)
        sys.exit(1)
