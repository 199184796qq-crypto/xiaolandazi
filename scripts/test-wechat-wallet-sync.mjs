import assert from 'node:assert/strict';
import { test } from 'node:test';
import { readRefundAwareWallet, refundProgressMessage, refundNeedsSync } from '../shared/wechatRefundSync.ts';

const previous = { cash_balance_cents: 2600, total_balance_cents: 3000, ledger: [] };
const processing = { id: 2, status: 'processing', amount_cents: 1000, refunded_cents: 0, frozen_cents: 1000, released_cents: 0 };
const success = { ...processing, status: 'success', refunded_cents: 1000, frozen_cents: 0 };
const wallet = record => ({ available_cents: 1600, frozen_cents: record.frozen_cents, refundable_cents: 1600, records: [record] });
const noDelay = async () => {};

test('accepted refund survives transient dashboard 500 and later callback without resubmission', async () => {
  let attempts = 0;
  const pending = await readRefundAwareWallet(async () => {
    if (++attempts < 3) throw new Error('HTTP 500');
    return previous;
  }, async () => wallet(processing), previous, noDelay);
  assert.equal(attempts, 3);
  assert.equal(pending.dashboard.cash_balance_cents, 1600);
  assert.equal(pending.dashboard.total_balance_cents, 2000);
  assert.equal(pending.refunds.frozen_cents, 1000);
  assert.equal(pending.warning, '');
  assert.match(refundProgressMessage(processing), /已受理/);
  assert.equal(refundNeedsSync(processing), true);
  const completed = await readRefundAwareWallet(async () => previous, async () => wallet(success), pending.dashboard, noDelay);
  assert.equal(completed.dashboard.cash_balance_cents, 1600);
  assert.equal(completed.refunds.frozen_cents, 0);
  assert.match(refundProgressMessage(completed.refunds.records[0]), /已确认原路退回成功/);
  assert.equal(refundNeedsSync(success), false);
});

test('persistent dashboard failure still displays authoritative refund balance with neutral warning', async () => {
  let attempts = 0;
  const result = await readRefundAwareWallet(async () => { attempts++; throw new Error('HTTP 500'); }, async () => wallet(success), previous, noDelay);
  assert.equal(attempts, 3);
  assert.equal(result.dashboard.cash_balance_cents, 1600);
  assert.equal(result.refunds.records[0].status, 'success');
  assert.match(result.warning, /不代表退款失败/);
  assert.equal(previous.cash_balance_cents, 2600);
});

test('unreadable refund snapshot is unknown, never fabricated as zero', async () => {
  const result = await readRefundAwareWallet(async () => previous, async () => { throw new Error('offline'); }, previous, noDelay);
  assert.equal(result.refunds, null);
  assert.match(result.warning, /同步/);
  const noData = await readRefundAwareWallet(async () => { throw new Error('offline'); }, async () => wallet(success), null, noDelay);
  assert.equal(noData.dashboard, null);
  assert.equal(noData.refunds.records[0].status, 'success');
});

test('only fully settled verified amounts show success; closed and partial releases differ', () => {
  assert.doesNotMatch(refundProgressMessage({ ...success, refunded_cents: 900 }), /已确认原路退回成功/);
  assert.match(refundProgressMessage({ ...success, status: 'closed', refunded_cents: 0, released_cents: 1000 }), /恢复到钱包/);
  assert.match(refundProgressMessage({ ...success, status: 'partially_refunded', refunded_cents: 500, released_cents: 500 }), /其余金额已解冻/);
  assert.equal(refundNeedsSync({ ...processing, status: 'abnormal' }), true);
});
