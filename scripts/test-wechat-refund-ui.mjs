import assert from 'node:assert/strict';
import { test } from 'node:test';
import * as mobile from '../customer-mobile/src/lib/wechatRefund.ts';
import * as desktop from '../web-console/src/wechatRefund.ts';

for (const [name, helpers] of [['mobile', mobile], ['desktop', desktop]]) {
  test(`${name}: exact cents, persisted request and lost-response retry`, () => {
    const values = new Map();
    globalThis.sessionStorage = {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => values.set(key, value),
      removeItem: (key) => values.delete(key),
    };
    assert.equal(helpers.refundAmountCents('0.01'), 1);
    assert.equal(helpers.refundAmountCents('1.20'), 120);
    assert.equal(helpers.refundAmountCents('21474836.47'), 2147483647);
    for (const text of ['0', '-1', 'NaN', '1e3', '1.001', '.1', '1.', '21474836.48']) {
      assert.throws(() => helpers.refundAmountCents(text));
    }
    assert.throws(() => helpers.refundDraft(101, 100));
    assert.equal(helpers.savedRefundAmount(), '');
    const first = helpers.refundDraft(100, 500);
    assert.equal(helpers.savedRefundAmount(), '1.00');
    // The server may have held funds even though its response was lost.
    assert.deepEqual(helpers.refundDraft(100, 0), first);
    assert.throws(() => helpers.refundDraft(200, 500));
    helpers.clearRefundDraft();
    assert.notEqual(helpers.refundDraft(100, 500).key, first.key);
  });
}
