import assert from 'node:assert/strict';
import { test } from 'node:test';
import { nativeRechargeAmount, nativeRechargeDraft, savedNativeRechargeDraft, rememberNativeRechargeID, clearNativeRechargeDraft, nativeQRIsValid } from '../web-console/src/wechatNativeRecharge.ts';

test('Native recharge: cents, lost-response retry and persisted order', () => {
  const values = new Map();
  globalThis.sessionStorage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) };
  assert.equal(nativeRechargeAmount('0.01'), 1);
  assert.equal(nativeRechargeAmount('12.30'), 1230);
  assert.equal(nativeRechargeAmount('21474836.47'), 2147483647);
  for (const value of ['0', '-1', '.1', '1.', '1.001', '1e2', '21474836.48']) assert.throws(() => nativeRechargeAmount(value));
  const draft = nativeRechargeDraft(123);
  assert.deepEqual(nativeRechargeDraft(123), draft);
  assert.throws(() => nativeRechargeDraft(124));
  rememberNativeRechargeID(draft, 42);
  assert.equal(savedNativeRechargeDraft().id, 42);
  clearNativeRechargeDraft();
  assert.notEqual(nativeRechargeDraft(123).key, draft.key);
});

test('Native QR: expired or unsafe images cannot be displayed', () => {
  const now = Date.now(), image = 'data:image/png;base64,dGVzdA==';
  assert.equal(nativeQRIsValid(image, new Date(now + 900000).toISOString(), now), true);
  assert.equal(nativeQRIsValid(image, new Date(now).toISOString(), now), false);
  assert.equal(nativeQRIsValid(image, 'invalid', now), false);
  assert.equal(nativeQRIsValid(image, new Date(now + 7200001).toISOString(), now), false);
  for (const bad of ['https://tracking.test/qr', 'data:image/svg+xml,<svg/>', 'javascript:alert(1)', image + '!']) {
    assert.equal(nativeQRIsValid(bad, new Date(now + 900000).toISOString(), now), false);
  }
});

test('Native recovery: invalid drafts and direct invalid cents fail closed', () => {
  const values = new Map();
  globalThis.sessionStorage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => values.set(key, value), removeItem: key => values.delete(key) };
  for (const cents of [0, -1, 1.1, NaN, Infinity, 2147483648]) assert.throws(() => nativeRechargeDraft(cents));
  const key = 'xiaolan-desktop-native-recharge-draft';
  for (const raw of ['null', '[]', 'invalid', '{"cents":123,"key":"short"}', '{"cents":123,"key":"native-valid-key-001","id":0}']) {
    values.set(key, raw);
    assert.throws(() => savedNativeRechargeDraft());
  }
});
