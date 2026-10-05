import type { RechargeRecord, ShopOrder, WechatPaymentParams } from './types';

type WechatBridge = { invoke: (method: string, params: WechatPaymentParams, callback: (value: { err_msg?: string }) => void) => void };
type WechatBrowser = Window & { WeixinJSBridge?: WechatBridge };

// A bridge success message is only a UI result. Order payment must still be
// checked with the server, never inferred from this callback.
export function invokeWechatPayment(params: WechatPaymentParams, browser: WechatBrowser = window): Promise<'ok' | 'cancel' | 'fail'> {
  return new Promise((resolve, reject) => {
    let invoked = false;
    let complete = false;
    let timer: ReturnType<typeof setTimeout>;
    const finish = (result?: 'ok' | 'cancel' | 'fail', error?: Error) => {
      if (complete) return;
      complete = true;
      clearTimeout(timer);
      browser.document.removeEventListener('WeixinJSBridgeReady', ready);
      if (error) reject(error); else resolve(result || 'fail');
    };
    const ready = () => {
      if (invoked || complete || !browser.WeixinJSBridge) return;
      invoked = true;
      clearTimeout(timer);
      timer = setTimeout(() => finish(undefined, new Error('支付结果尚未确认，请查询订单，不要重复付款')), 90000);
      try {
        browser.WeixinJSBridge.invoke('getBrandWCPayRequest', params, (value) => {
          if (value.err_msg === 'get_brand_wcpay_request:ok') finish('ok');
          else if (value.err_msg === 'get_brand_wcpay_request:cancel') finish('cancel');
          else finish('fail');
        });
      } catch { finish(undefined, new Error('微信收银台调起失败，请查询订单后重试')); }
    };
    timer = setTimeout(() => finish(undefined, new Error('请在微信中打开此页面进行支付')), 5000);
    browser.document.addEventListener('WeixinJSBridgeReady', ready);
    ready();
  });
}

export function shopOrderIsPaid(order: ShopOrder): boolean {
  return ['paid', 'fulfilled', 'completed'].includes(order.status) && order.payment_status === 'paid';
}

export function rechargeIsPaid(item: RechargeRecord): boolean {
  return item.status === 'paid' && item.credited_amount_cents === item.requested_amount_cents && item.credited_amount_cents > 0;
}

// Parse decimal input without rounding hidden fractions or accepting exponents.
export function rechargeAmountCents(input: string): number | null {
  const value = String(input).trim();
  if (!/^\d+(?:\.\d{0,2})?$/.test(value)) return null;
  const [whole, fraction = ''] = value.split('.');
  const cents = Number(whole) * 100 + Number(fraction.padEnd(2, '0'));
  return Number.isSafeInteger(cents) && cents > 0 && cents <= 2147483647 ? cents : null;
}

export function paymentLoginReturn(raw: string): string | null {
  try {
    const parsed = new URL(raw, 'https://local.invalid');
    if (parsed.origin !== 'https://local.invalid' || parsed.hash || raw.includes('\\')) return null;
    const key = parsed.pathname === '/wallet' ? 'recharge' : parsed.pathname === '/shop/checkout' ? 'order' : '';
    if (!key || parsed.searchParams.getAll(key).length !== 1) return null;
    const id = parsed.searchParams.get(key) || '';
    if (!/^[1-9]\d*$/.test(id) || !Number.isSafeInteger(Number(id))) return null;
    return `${parsed.pathname}?${key}=${id}`;
  } catch { return null; }
}
