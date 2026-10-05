import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('originfix', Path(__file__).with_name('fix-wechat-public-origin.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class PublicOriginTest(unittest.TestCase):
    def test_exact_setting_only(self):
        for ending in (b'\n', b'\r\n', b''):
            for value in (b'http://47.114.55.117', b'"http://47.114.55.117"', b"'http://47.114.55.117'"):
                before = b'# preserve\r\nDB_PASSWORD="test-only"\r\nWECHAT_PAY_ENABLED=true\r\nPUBLIC_WEB_BASE_URL=' + value + ending
                self.assertEqual(module.corrected_config(before), before.replace(b'PUBLIC_WEB_BASE_URL=' + value, b'PUBLIC_WEB_BASE_URL="https://www.xiaolandaizi.cn"'))

    def test_missing_duplicate_or_unexpected_refused(self):
        for contents in (b'', b'PUBLIC_WEB_BASE_URL="https://www.xiaolandaizi.cn"',
                         b'PUBLIC_WEB_BASE_URL=http://unknown.test',
                         b'PUBLIC_WEB_BASE_URL=http://47.114.55.117\n PUBLIC_WEB_BASE_URL=http://47.114.55.117'):
            with self.assertRaises(ValueError):
                module.corrected_config(contents)


if __name__ == '__main__':
    unittest.main()
