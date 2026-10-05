#!/usr/bin/env python3
"""Explicitly authorized production switch. No orders or money-moving API calls."""
import json
import os
from pathlib import Path
import re
import shlex
import stat
import sys
import uuid

ENV_FILE = Path('/etc/xiaolan/management.env')
EXPECTED_RELEASE = Path('/opt/xiaolan/releases/20261003-wechatpay-jsapi-v1')
EXPECTED_BINARY_SHA = 'f8a1c9ba569893da94b4fb6d0a449a603c81d5cd9f9c55f62c7bb98d93bc1732'


def enabled_config(original):
    matches = list(re.finditer(rb'(?m)^(WECHAT_PAY_ENABLED=)false(\r?)$', original))
    settings = re.findall(rb'(?m)^\s*WECHAT_PAY_ENABLED\s*=', original)
    if len(matches) != 1 or len(settings) != 1:
        raise ValueError('expected exactly one disabled payment switch')
    match = matches[0]
    return original[:match.start()] + match[1] + b'true' + match[2] + original[match.end():]


def safe_regular(path):
    for parent in path.parents:
        if parent.is_symlink() or not parent.is_dir():
            raise ValueError('unsafe parent')
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError('unsafe file')
    return info


def private_file(path, contents, uid, gid):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.fchown(fd, uid, gid)
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(contents)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)


def systemctl(*arguments):
    import subprocess
    result = subprocess.run(['systemctl', *arguments], capture_output=True, timeout=45, check=False)
    if result.returncode:
        raise RuntimeError('service command failed')
    return result.stdout.decode('utf-8').strip()


def status(url, method='GET', body=None):
    import urllib.error
    import urllib.request
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    request = urllib.request.Request(url, data=body, method=method)
    if body is not None:
        request.add_header('Content-Type', 'application/json')
    try:
        with opener.open(request, timeout=5) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except Exception:
        return 0


def enable(operation):
    import fcntl
    import hashlib
    import time
    if os.geteuid() != 0 or str(uuid.UUID(operation)) != operation:
        raise ValueError('invalid invocation')
    os.umask(0o077)
    lockfd = os.open('/opt/xiaolan/deployments/wechatpay-deployment.lock', os.O_WRONLY | os.O_NOFOLLOW)
    with os.fdopen(lockfd, 'wb') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if Path('/opt/xiaolan/current').resolve(strict=True) != EXPECTED_RELEASE:
            raise ValueError('release changed')
        binary = EXPECTED_RELEASE / 'bin/management-service'
        safe_regular(binary)
        if hashlib.sha256(binary.read_bytes()).hexdigest() != EXPECTED_BINARY_SHA:
            raise ValueError('binary changed')
        info = safe_regular(ENV_FILE)
        if info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError('unexpected env access permissions')
        core_pid = systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-core.service')
        device_pid = systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-xiaozhi.service')
        previous_pid = systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-management.service')
        with ENV_FILE.open('rb') as source:
            fcntl.flock(source, fcntl.LOCK_EX | fcntl.LOCK_NB)
            original = source.read()
            updated = enabled_config(original)
            # Parse only payment settings, never shell-source/echo the env file.
            settings = {}
            for line in original.decode('utf-8').splitlines():
                match = re.match(r'^\s*(WECHAT_[A-Z0-9_]+)\s*=(.*)$', line)
                if match:
                    value = shlex.split(match[2], comments=True)
                    if match[1] in settings or len(value) != 1:
                        raise ValueError('invalid payment setting')
                    settings[match[1]] = value[0]
            expected = {
                'WECHAT_PAY_APP_ID': 'wx5a19fd75c13d5584',
                'WECHAT_PAY_MCH_ID': '1118326239',
                'WECHAT_PAY_MERCHANT_CERT_SERIAL_NO': '1FC20AC9C1A7593834C0DCDC03405E6B2960773B',
                'WECHAT_PAY_PUBLIC_KEY_ID': 'PUB_KEY_ID_0111183262392026100300211816001000',
                'WECHAT_PAY_NOTIFY_URL': 'https://www.xiaolandaizi.cn/api/v1/payments/wechat/notify',
                'WECHAT_PAY_OAUTH_CALLBACK_URL': 'https://www.xiaolandaizi.cn/api/v1/payments/wechat/oauth/callback',
            }
            if any(settings.get(key) != value for key, value in expected.items()):
                raise ValueError('merchant configuration changed')
            for key in ('WECHAT_PAY_API_V3_KEY', 'WECHAT_OFFICIAL_ACCOUNT_APP_SECRET'):
                if not re.fullmatch(r'[A-Za-z0-9]{32}', settings.get(key, '')):
                    raise ValueError('credential format invalid')
            for key, name in (('WECHAT_PAY_MERCHANT_PRIVATE_KEY_PATH', 'apiclient_key.pem'),
                              ('WECHAT_PAY_PUBLIC_KEY_PATH', 'pub_key.pem')):
                path = Path(settings.get(key, ''))
                if path != Path('/etc/xiaolan/wechatpay-52d6af3a-73cb-4c15-a852-4ed434ea74db') / name:
                    raise ValueError('credential path changed')
                safe_regular(path)
            backup = ENV_FILE.parent / ('management.env.before-enable-wechatpay-' + operation)
            temporary = ENV_FILE.parent / ('.management.env.enable-wechatpay-' + operation)
            private_file(backup, original, info.st_uid, info.st_gid)
            private_file(temporary, updated, info.st_uid, info.st_gid)
            if safe_regular(ENV_FILE).st_ino != info.st_ino or ENV_FILE.read_bytes() != original:
                raise ValueError('env changed concurrently')
            if Path('/opt/xiaolan/current').resolve(strict=True) != EXPECTED_RELEASE:
                raise ValueError('release changed concurrently')
            os.replace(temporary, ENV_FILE)
            directory_fd = os.open(ENV_FILE.parent, os.O_DIRECTORY)
            try:
                os.fsync(directory_fd)
            finally:
                os.close(directory_fd)
        print(json.dumps({'configurationEnabled': True, 'backupFile': str(backup),
                          'onlyPaymentSwitchChanged': True, 'sensitiveValuesPrinted': False}), flush=True)
        systemctl('restart', 'xiaolan-management.service')
        healthy = False
        for attempt in range(90):
            if status('http://127.0.0.1:8080/healthz') == 200:
                healthy = True
                break
            if attempt and attempt % 15 == 0:
                print(json.dumps({'waitingForManagementStartup': True}), flush=True)
            time.sleep(2)
        new_pid = systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-management.service')
        runtime_enabled = False
        if new_pid.isdecimal() and int(new_pid) > 0:
            runtime_enabled = b'WECHAT_PAY_ENABLED=true' in Path('/proc/' + new_pid + '/environ').read_bytes().split(b'\x00')
        probes = {
            'anonymousStatus': status('https://www.xiaolandaizi.cn/api/v1/payments/wechat/status'),
            'anonymousOAuth': status('https://www.xiaolandaizi.cn/api/v1/payments/wechat/oauth/callback'),
            'invalidUnsignedNotification': status('https://www.xiaolandaizi.cn/api/v1/payments/wechat/notify', 'POST', b'{}'),
        }
        preserved = (
            core_pid == systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-core.service') and
            device_pid == systemctl('show', '-p', 'MainPID', '--value', 'xiaolan-xiaozhi.service'))
        ok = (healthy and runtime_enabled and new_pid != previous_pid and preserved and
              probes == {'anonymousStatus': 401, 'anonymousOAuth': 401, 'invalidUnsignedNotification': 400} and
              ENV_FILE.read_bytes() == updated and Path('/opt/xiaolan/current').resolve(strict=True) == EXPECTED_RELEASE)
        print(json.dumps({'ok': ok, 'runtimePaymentEnabled': runtime_enabled,
                          'managementHealthy': healthy, 'managementRestarted': new_pid != previous_pid,
                          'coreAndDevicePreserved': preserved, 'probes': probes,
                          'paymentOrderCreated': False, 'sensitiveValuesPrinted': False}), flush=True)
        # Never blindly restore the disabled env after the enabled service might
        # have accepted a customer's payment. On failure inspect payment state.
        return 0 if ok else 1


if __name__ == '__main__':
    try:
        if len(sys.argv) != 2:
            raise ValueError('operation id required')
        sys.exit(enable(sys.argv[1]))
    except Exception as error:
        # No exception text, HTTP body, token, key or environment contents.
        print(json.dumps({'ok': False, 'errorType': type(error).__name__,
                          'inspectStateBeforeRetry': True, 'sensitiveValuesPrinted': False}), flush=True)
        sys.exit(1)
