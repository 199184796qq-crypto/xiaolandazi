#!/usr/bin/env python3
"""Stage credentials from SSH stdin. Never enables payment or restarts services."""
import json
import os
from pathlib import Path
import re
import shlex
import stat
import sys
import uuid


ENV_FILE = Path('/etc/xiaolan/management.env')
EXPECTED_KEYS = {
    'WECHAT_PAY_APP_ID', 'WECHAT_PAY_MCH_ID',
    'WECHAT_PAY_MERCHANT_CERT_SERIAL_NO', 'WECHAT_PAY_API_V3_KEY',
    'WECHAT_OFFICIAL_ACCOUNT_APP_SECRET', 'WECHAT_PAY_PUBLIC_KEY_ID',
    'WECHAT_PAY_NOTIFY_URL', 'WECHAT_PAY_OAUTH_CALLBACK_URL',
}


def regular_path(path, directory=False):
    for parent in reversed(path.parents):
        if parent.is_symlink() or not parent.is_dir():
            raise ValueError('unsafe parent path')
    info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or not (
            stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)):
        raise ValueError('unsafe target path')
    return info


def env_value(raw):
    values = shlex.split(raw, comments=True, posix=True)
    if len(values) != 1:
        if not values and not raw.strip():
            return ''
        raise ValueError('unsupported existing payment setting')
    return values[0]


def merge_env(original, updates):
    lines = original.splitlines(keepends=True)
    found = set()
    for index, line in enumerate(lines):
        match = re.match(r'^\s*([A-Z][A-Z0-9_]*)\s*=(.*?)(?:\r?\n)?$', line)
        if not match or match[1] not in updates:
            continue
        key = match[1]
        if key in found:
            raise ValueError('duplicate payment configuration')
        found.add(key)
        previous = env_value(match[2])
        if previous and previous != updates[key]:
            raise ValueError('refusing to replace existing payment configuration')
        lines[index] = key + '=' + updates[key] + '\n'
    if lines and not lines[-1].endswith('\n'):
        lines[-1] += '\n'
    for key in sorted(updates.keys() - found):
        lines.append(key + '=' + updates[key] + '\n')
    return ''.join(lines)


def private_write(path, content, uid, gid, mode):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, mode)
    try:
        os.fchown(fd, uid, gid)
        os.fchmod(fd, mode)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(content)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)


def stage(payload):
    import fcntl
    import grp
    if os.geteuid() != 0:
        raise ValueError('root required')
    operation = payload['operation_id']
    if str(uuid.UUID(operation)) != operation:
        raise ValueError('invalid operation id')
    updates = payload['environment']
    if set(updates) != EXPECTED_KEYS:
        raise ValueError('unexpected environment keys')
    if any(not isinstance(value, str) or not value or re.search(r'[\s\x00"\'\\]', value)
           for value in updates.values()):
        raise ValueError('invalid environment value')
    if (not re.fullmatch(r'[A-Za-z0-9]{32}', updates['WECHAT_PAY_API_V3_KEY']) or
            not re.fullmatch(r'[A-Za-z0-9]{32}', updates['WECHAT_OFFICIAL_ACCOUNT_APP_SECRET'])):
        raise ValueError('invalid secret format')
    if updates['WECHAT_PAY_API_V3_KEY'] == updates['WECHAT_OFFICIAL_ACCOUNT_APP_SECRET']:
        raise ValueError('secrets must differ')
    if updates['WECHAT_PAY_MCH_ID'] != '1118326239' or updates['WECHAT_PAY_APP_ID'] != 'wx5a19fd75c13d5584':
        raise ValueError('unexpected merchant or application')
    for key, suffix in (
        ('WECHAT_PAY_NOTIFY_URL', '/api/v1/payments/wechat/notify'),
        ('WECHAT_PAY_OAUTH_CALLBACK_URL', '/api/v1/payments/wechat/oauth/callback'),
    ):
        if updates[key] != 'https://www.xiaolandaizi.cn' + suffix:
            raise ValueError('unexpected callback URL')
    materials = payload['materials']
    if set(materials) != {'apiclient_key.pem', 'apiclient_cert.pem', 'pub_key.pem'}:
        raise ValueError('unexpected credential files')
    for name, marker in (
        ('apiclient_key.pem', 'PRIVATE KEY'),
        ('apiclient_cert.pem', 'CERTIFICATE'),
        ('pub_key.pem', 'PUBLIC KEY'),
    ):
        if not isinstance(materials[name], str) or not re.search(
                r'^-----BEGIN (?:RSA )?' + marker + r'-----', materials[name]):
            raise ValueError('invalid credential envelope')

    os.umask(0o077)
    info = regular_path(ENV_FILE)
    if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
        raise ValueError('unexpected runtime env ownership or permissions')
    service_gid = grp.getgrnam('ecs-user').gr_gid
    credential_dir = ENV_FILE.parent / ('wechatpay-' + operation)
    backup_file = ENV_FILE.parent / ('management.env.before-wechatpay-' + operation)
    temporary = ENV_FILE.parent / ('.management.env.wechatpay-' + operation)
    updates = dict(updates)
    updates.update({
        'WECHAT_PAY_ENABLED': 'false',
        'WECHAT_PAY_MERCHANT_PRIVATE_KEY_PATH': str(credential_dir / 'apiclient_key.pem'),
        'WECHAT_PAY_PUBLIC_KEY_PATH': str(credential_dir / 'pub_key.pem'),
    })
    with ENV_FILE.open('rb') as source:
        fcntl.flock(source, fcntl.LOCK_EX)
        original_bytes = source.read()
        merged = merge_env(original_bytes.decode('utf-8'), updates).encode('utf-8')
        if merged == original_bytes:
            raise ValueError('configuration already staged; run read-only preflight')
        # Validate every target before creating credentials or a backup.
        if credential_dir.exists() or credential_dir.is_symlink() or backup_file.exists() or temporary.exists():
            raise ValueError('operation target already exists')
        credential_dir.mkdir(mode=0o750)
        os.chown(credential_dir, 0, service_gid)
        os.chmod(credential_dir, 0o750)
        for name, value in materials.items():
            private_write(credential_dir / name, value.encode('ascii'), 0, service_gid, 0o640)
        private_write(backup_file, original_bytes, info.st_uid, info.st_gid, 0o600)
        private_write(temporary, merged, info.st_uid, info.st_gid, 0o600)
        current_info = regular_path(ENV_FILE)
        if current_info.st_ino != info.st_ino or ENV_FILE.read_bytes() != original_bytes:
            raise ValueError('runtime env changed concurrently; staged files retained')
        os.replace(temporary, ENV_FILE)
        dirfd = os.open(ENV_FILE.parent, os.O_DIRECTORY)
        try:
            os.fsync(dirfd)
        finally:
            os.close(dirfd)
    return {
        'staged': True, 'productionPaymentEnabled': False,
        'serviceRestarted': False, 'credentialDirectory': str(credential_dir),
        'backupFile': str(backup_file), 'sensitiveValuesPrinted': False,
    }


def preflight(binary):
    import subprocess
    if os.geteuid() != 0 or not re.fullmatch(r'/tmp/xiaolan-wechat-config-[a-f0-9-]+/wechatpay-preflight', binary):
        raise ValueError('unsafe preflight invocation')
    regular_path(Path(binary))
    regular_path(ENV_FILE)
    child_environment = {'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8'}
    for line in ENV_FILE.read_text(encoding='utf-8').splitlines():
        match = re.match(r'^\s*(WECHAT_[A-Z0-9_]+)\s*=(.*)$', line)
        if match:
            if match[1] in child_environment:
                raise ValueError('duplicate payment configuration')
            child_environment[match[1]] = env_value(match[2])
    if child_environment.get('WECHAT_PAY_ENABLED', '').lower() != 'false':
        raise ValueError('preflight requires production payment disabled')
    # Drop privileges: verify that the actual service account can read key files.
    import pwd
    account = pwd.getpwnam('ecs-user')
    def drop_privileges():
        os.initgroups(account.pw_name, account.pw_gid)
        os.setgid(account.pw_gid)
        os.setuid(account.pw_uid)
    completed = subprocess.run([binary], env=child_environment, timeout=45,
                               preexec_fn=drop_privileges, capture_output=True, check=False)
    try:
        report = json.loads(completed.stdout)
    except (ValueError, UnicodeDecodeError):
        raise ValueError('preflight failed without safe JSON report') from None
    # Binary emits only a typed report, never API bodies, headers or errors.
    print(json.dumps(report, ensure_ascii=True))
    return completed.returncode


if __name__ == '__main__':
    try:
        if len(sys.argv) == 3 and sys.argv[1] == 'preflight':
            sys.exit(preflight(sys.argv[2]))
        if len(sys.argv) != 1:
            raise ValueError('invalid arguments')
        raw = sys.stdin.buffer.read(32769)
        if len(raw) > 32768:
            raise ValueError('oversized credential input')
        print(json.dumps(stage(json.loads(raw)), ensure_ascii=True))
    except Exception as error:
        # Do not print exceptions: they may embed submitted credential values.
        print(json.dumps({'ok': False, 'errorType': type(error).__name__,
                          'sensitiveValuesPrinted': False}))
        sys.exit(1)
