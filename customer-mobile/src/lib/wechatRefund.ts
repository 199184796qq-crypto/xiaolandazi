// Persist the key BEFORE sending. A network error must not mint a new refund.
const storageKey = 'xiaolan-wechat-cash-refund-draft';
export function refundDraft(cents: number, available: number): { cents: number; key: string } {
  const saved = sessionStorage.getItem(storageKey);
  if (saved) {
    const draft = JSON.parse(saved) as { cents: number; key: string };
    if (draft.cents !== cents) throw new Error(`上一笔 ${ (draft.cents / 100).toFixed(2) } 元退回结果待核验，请使用原金额重试，勿另发新单`);
    return draft;
  }
  if (cents > available) throw new Error('退回金额超过当前可退本金');
  const draft = { cents, key: `cash-refund-${crypto.randomUUID()}` };
  sessionStorage.setItem(storageKey, JSON.stringify(draft));
  return draft;
}
export function clearRefundDraft() { sessionStorage.removeItem(storageKey); }
export function savedRefundAmount(): string {
  try { const saved = sessionStorage.getItem(storageKey); return saved ? (Number(JSON.parse(saved).cents) / 100).toFixed(2) : ''; }
  catch { return ''; }
}
export function refundAmountCents(value: string): number {
  const text = value.trim();
  if (!/^\d+(\.\d{1,2})?$/.test(text)) throw new Error('请输入正确金额，最多两位小数');
  const [whole, decimal = ''] = text.split('.');
  const cents = Number(whole) * 100 + Number(decimal.padEnd(2, '0'));
  if (!Number.isSafeInteger(cents) || cents <= 0 || cents > 2147483647) throw new Error('退回金额必须在 0.01 至 21474836.47 元之间');
  return cents;
}
