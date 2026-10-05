import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('stage', Path(__file__).with_name('stage-wechatpay-credentials.py'))
stage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(stage)


class MergeEnvironmentTest(unittest.TestCase):
    def test_unrelated_lines_are_preserved_and_payment_remains_disabled(self):
        original = '# comment\r\nDB_PASSWORD="keep existing"\r\nWECHAT_PAY_ENABLED="false"\r\nOTHER=unchanged'
        merged = stage.merge_env(original, {'WECHAT_PAY_ENABLED': 'false', 'WECHAT_PAY_APP_ID': 'test'})
        self.assertIn('# comment\r\nDB_PASSWORD="keep existing"\r\n', merged)
        self.assertIn('OTHER=unchanged\n', merged)
        self.assertEqual(merged.count('WECHAT_PAY_ENABLED='), 1)
        self.assertIn('WECHAT_PAY_ENABLED=false\n', merged)

    def test_enabled_or_conflicting_credentials_are_not_replaced(self):
        for original, changes in (
            ('WECHAT_PAY_ENABLED=true\n', {'WECHAT_PAY_ENABLED': 'false'}),
            ('WECHAT_PAY_API_V3_KEY=alreadyconfigured\n', {'WECHAT_PAY_API_V3_KEY': 'new'}),
            ('WECHAT_PAY_ENABLED=false\nWECHAT_PAY_ENABLED=false\n', {'WECHAT_PAY_ENABLED': 'false'}),
        ):
            with self.assertRaises(ValueError):
                stage.merge_env(original, changes)

    def test_empty_or_matching_settings_allowed(self):
        self.assertEqual(stage.merge_env('KEY=""\n', {'KEY': 'value'}), 'KEY=value\n')
        self.assertEqual(stage.merge_env('KEY="value"\n', {'KEY': 'value'}), 'KEY=value\n')


if __name__ == '__main__':
    unittest.main()
