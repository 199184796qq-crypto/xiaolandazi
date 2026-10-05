// Display-only synchronization. Never creates, retries or settles a refund.
type CashDashboard = { cash_balance_cents: number; total_balance_cents: number }
export type RefundProgress = { id: number; status: string; amount_cents: number; refunded_cents: number; frozen_cents: number; released_cents: number }
type RefundWallet = { available_cents: number; frozen_cents: number; refundable_cents: number; records: RefundProgress[] }

export function refundNeedsSync(record: RefundProgress | null | undefined): boolean {
  return !!record && (record.frozen_cents > 0 || ['queued', 'processing', 'abnormal'].includes(record.status))
}

export function refundProgressMessage(record: RefundProgress): string {
  if (record.status === 'success' && record.frozen_cents === 0 && record.refunded_cents === record.amount_cents) {
    return `微信已确认原路退回成功，已退回 ¥${(record.refunded_cents / 100).toFixed(2)}。`
  }
  if (record.status === 'closed' && record.frozen_cents === 0) return '微信已确认退回关闭，冻结金额已恢复到钱包。'
  if (record.status === 'partially_refunded' && record.frozen_cents === 0) return `已退回 ¥${(record.refunded_cents / 100).toFixed(2)}，其余金额已解冻。`
  if (record.status === 'abnormal') return '微信退回结果异常，金额保持冻结，请联系客服核验，不要重复提交。'
  return `退回申请已受理，微信正在处理。冻结 ¥${(record.frozen_cents / 100).toFixed(2)}，页面会自动更新结果，请勿重复提交。`
}

export async function readRefundAwareWallet<T extends CashDashboard, R extends RefundWallet>(
  readFinance: () => Promise<T>, readRefunds: () => Promise<R>, previous: T | null,
  wait: (ms: number) => Promise<void> = ms => new Promise(resolve => setTimeout(resolve, ms)),
): Promise<{ dashboard: T | null; refunds: R | null; warning: string }> {
  const finance = async () => {
    for (let attempt = 0; ; attempt++) {
      try { return await readFinance() }
      catch (error) { if (attempt >= 2) throw error; await wait(250 * (attempt + 1)) }
    }
  }
  const [f, r] = await Promise.allSettled([finance(), readRefunds()])
  let dashboard = f.status === 'fulfilled' ? f.value : previous
  const refunds = r.status === 'fulfilled' ? r.value : null
  if (dashboard && refunds) {
    // One server snapshot is authoritative for available cash + frozen + refunds.
    dashboard = { ...dashboard, cash_balance_cents: refunds.available_cents,
      total_balance_cents: dashboard.total_balance_cents - dashboard.cash_balance_cents + refunds.available_cents }
  }
  return { dashboard, refunds,
    warning: f.status === 'rejected' || r.status === 'rejected' ? '钱包部分数据正在同步，页面会自动重试；不代表退款失败，请勿重复提交。' : '' }
}
