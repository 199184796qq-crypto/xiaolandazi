import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import ts from 'typescript';

const source = await readFile(new URL('../src/lib/wechatPay.ts', import.meta.url), 'utf8');
const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
const { invokeWechatPayment, shopOrderIsPaid, rechargeIsPaid, rechargeAmountCents, paymentLoginReturn } = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
for (const [input, cents] of [['0.01', 1], ['1',100],['1.10',110],[' 12.03 ',1203],['21474836.47',2147483647]]) assert.equal(rechargeAmountCents(input),cents);
for (const input of ['0','-1','1e2','1.001','Infinity','NaN','21474836.48','99999999999999999999','abc']) assert.equal(rechargeAmountCents(input),null);
assert.equal(rechargeIsPaid({status:'paid',requested_amount_cents:100,credited_amount_cents:100}),true);
for (const item of [{status:'pending',requested_amount_cents:100,credited_amount_cents:100},{status:'paid',requested_amount_cents:100,credited_amount_cents:0},{status:'paid',requested_amount_cents:100,credited_amount_cents:99}]) assert.equal(rechargeIsPaid(item),false);
assert.equal(paymentLoginReturn('/wallet?recharge=42&wechat_auth=ready'),'/wallet?recharge=42');
assert.equal(paymentLoginReturn('/shop/checkout?order=4'),'/shop/checkout?order=4');
for (const path of ['https://evil.test/wallet?recharge=42','//evil.test/wallet?recharge=42','/wallet?recharge=0','/wallet?recharge=1&recharge=2','/wallet?recharge=1#bad','/login']) assert.equal(paymentLoginReturn(path),null);
const params = { appId: 'wx-test', timeStamp: '1', nonceStr: 'nonce', package: 'prepay_id=test', signType: 'RSA', paySign: 'test' };

for (const [err_msg, expected] of [
  ['get_brand_wcpay_request:ok', 'ok'],
  ['get_brand_wcpay_request:cancel', 'cancel'],
  ['get_brand_wcpay_request:fail', 'fail'],
  ['unexpected', 'fail'],
]) {
  const document = new EventTarget();
  let invokes = 0;
  const browser = { document, WeixinJSBridge: { invoke(method, supplied, callback) {
    assert.equal(method, 'getBrandWCPayRequest');
    assert.deepEqual(supplied, params);
    invokes++;
    callback({ err_msg });
    callback({ err_msg: 'get_brand_wcpay_request:ok' });
  } } };
  assert.equal(await invokeWechatPayment(params, browser), expected);
  document.dispatchEvent(new Event('WeixinJSBridgeReady'));
  assert.equal(invokes, 1);
}

const document = new EventTarget();
const delayedBrowser = { document };
const delayedPayment = invokeWechatPayment(params, delayedBrowser);
delayedBrowser.WeixinJSBridge = { invoke(_, __, callback) { callback({ err_msg: 'get_brand_wcpay_request:ok' }); } };
document.dispatchEvent(new Event('WeixinJSBridgeReady'));
assert.equal(await delayedPayment, 'ok');

assert.equal(shopOrderIsPaid({ status: 'pending', payment_status: 'pending' }), false);
assert.equal(shopOrderIsPaid({ status: 'pending', payment_status: 'paid' }), false);
assert.equal(shopOrderIsPaid({ status: 'cancelled', payment_status: 'paid' }), false);
assert.equal(shopOrderIsPaid({ status: 'paid', payment_status: 'paid' }), true);
const checkout = await readFile(new URL('../src/routes/shop/checkout/+page.svelte', import.meta.url), 'utf8');
assert.equal(checkout.includes('sandboxPay'), false);
assert.ok(checkout.includes('queryWechatShopOrder'));
assert.ok(checkout.includes('history.replaceState'));
console.log('WeChat bridge outcomes, delayed readiness, single invocation, server payment truth and checkout resume passed.');
