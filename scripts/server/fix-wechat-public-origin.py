#!/usr/bin/env python3
"""Correct the canonical site origin without touching payment credentials."""
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time
import uuid

ENV_FILE = Path('/etc/xiaolan/management.env')
EXPECTED_RELEASE = Path('/opt/xiaolan/releases/20261003-wechat-recharge-v1')
ORIGIN = 'https://www.xiaolandaizi.cn'


def corrected_config(original):
    matches = list(re.finditer(rb'(?m)^([ \t]*PUBLIC_WEB_BASE_URL[ \t]*=)[^\r\n]*', original))
    if len(matches) != 1:
        raise ValueError('expected exactly one public site setting')
    match = matches[0]
    value = match[0][len(match[1]):].strip().strip(b'\"\'')
    if value != b'http://47.114.55.117':
        raise ValueError('unexpected old public site setting')
    return original[:match.start()] + match[1] + b'"https://www.xiaolandaizi.cn"' + original[match.end():]


def service_pid(name):
    return subprocess.check_output(['systemctl', 'show', '-p', 'MainPID', '--value', name], timeout=10).decode().strip()


def private_write(path, contents, gid):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    try:
        os.fchown(fd, 0, gid)
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, 'wb', closefd=False) as stream:
            stream.write(contents)
            stream.flush()
            os.fsync(fd)
    finally:
        os.close(fd)


def probe(origin, site='same-origin'):
    import urllib.error
    import urllib.request
    request = urllib.request.Request(ORIGIN + '/api/v1/wallet/recharges', data=b'{}', method='POST',
        headers={'Content-Type': 'application/json', 'Origin': origin, 'Sec-Fetch-Site': site})
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    try:
        with opener.open(request, timeout=5) as response:
            return response.status
    except urllib.error.HTTPError as error:
        return error.code
    except Exception:
        return 0


def fix(operation):
    import fcntl
    if os.geteuid() != 0 or str(uuid.UUID(operation)) != operation:
        raise ValueError('invalid invocation')
    if Path('/etc/xiaolan').is_symlink() or ENV_FILE.is_symlink():
        raise ValueError('unsafe configuration path')
    os.umask(0o077)
    with open('/opt/xiaolan/deployments/wechatpay-deployment.lock', 'rb') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if Path('/opt/xiaolan/current').resolve(strict=True) != EXPECTED_RELEASE:
            raise ValueError('release changed')
        info = ENV_FILE.lstat()
        if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or stat.S_IMODE(info.st_mode) != 0o600:
            raise ValueError('unexpected configuration permissions')
        original = ENV_FILE.read_bytes()
        updated = corrected_config(original)
        core_pid = service_pid('xiaolan-core.service')
        device_pid = service_pid('xiaolan-xiaozhi.service')
        management_pid = service_pid('xiaolan-management.service')
        backup = ENV_FILE.parent / ('management.env.before-public-origin-' + operation)
        temporary = ENV_FILE.parent / ('.management.env.public-origin-' + operation)
        private_write(backup, original, info.st_gid)
        private_write(temporary, updated, info.st_gid)
        if ENV_FILE.lstat().st_ino != info.st_ino or ENV_FILE.read_bytes() != original:
            raise ValueError('configuration changed concurrently')
        os.replace(temporary, ENV_FILE)
        directory_fd = os.open(ENV_FILE.parent, os.O_DIRECTORY)
        try:
            os.fsync(directory_fd)
        finally:
            os.close(directory_fd)
        subprocess.run(['systemctl', 'restart', 'xiaolan-management.service'], check=True, timeout=45, capture_output=True)
        correct = 0
        for _ in range(60):
            correct = probe(ORIGIN)
            if correct == 401:
                break
            time.sleep(1)
        hostile = probe('https://evil.test')
        cross_site = probe(ORIGIN, 'cross-site')
        null = probe('null')
        same_domain_wrong_protocol = probe('http://www.xiaolandaizi.cn')
        config_preserved = ENV_FILE.read_bytes() == corrected_config(backup.read_bytes())
        services_preserved = service_pid('xiaolan-core.service') == core_pid and service_pid('xiaolan-xiaozhi.service') == device_pid
        result = {
            'publicOrigin': ORIGIN, 'sameOriginUnauthenticatedStatus': correct,
            'hostileOriginStatus': hostile, 'crossSiteStatus': cross_site,
            'nullOriginStatus': null, 'wrongProtocolStatus': same_domain_wrong_protocol,
            'onlyPublicOriginChanged': config_preserved,
            'managementRestarted': service_pid('xiaolan-management.service') != management_pid,
            'coreAndDevicePreserved': services_preserved,
            'backupFile': str(backup), 'paymentCreated': False, 'sensitiveValuesPrinted': False,
        }
        print(json.dumps(result), flush=True)
        if correct != 401 or any(code != 403 for code in (hostile, cross_site, null, same_domain_wrong_protocol)) or not config_preserved or not services_preserved:
            raise RuntimeError('origin verification failed; recharge-capable backend retained')


if __name__ == '__main__':
    try:
        fix(sys.argv[1])
    except Exception as error:
        print(json.dumps({'operationFailed': True, 'errorType': type(error).__name__, 'sensitiveValuesPrinted': False}), flush=True)
        sys.exit(1)
