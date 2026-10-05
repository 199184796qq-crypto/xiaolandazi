import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('enable', Path(__file__).with_name('enable-wechatpay.py'))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class EnableSwitchTest(unittest.TestCase):
    def test_only_exact_switch_changes(self):
        for ending in (b'\n', b'\r\n', b''):
            before = b'# keep\r\nDB_PASSWORD="unchanged"\r\nWECHAT_PAY_ENABLED=false' + ending
            after = module.enabled_config(before)
            self.assertEqual(after, before.replace(b'WECHAT_PAY_ENABLED=false', b'WECHAT_PAY_ENABLED=true'))

    def test_ambiguous_enabled_or_missing_switch_refused(self):
        for value in (b'', b'WECHAT_PAY_ENABLED=true\n', b'WECHAT_PAY_ENABLED="false"\n',
                      b'WECHAT_PAY_ENABLED=false\nWECHAT_PAY_ENABLED=false\n',
                      b'WECHAT_PAY_ENABLED=false\n  WECHAT_PAY_ENABLED=true\n',
                      b'WECHAT_PAY_ENABLED=false-extra\n'):
            with self.assertRaises(ValueError):
                module.enabled_config(value)


if __name__ == '__main__':
    unittest.main()
