// Marketing pricing is always settled at whole-yuan precision.
// Any jiao/fen are discarded directly; never round up.
export function floorToWholeYuanCents(cents: number) {
  const safe = Number.isFinite(cents) ? Math.max(0, cents) : 0
  return Math.floor(safe / 100) * 100
}

export function marketingPayableCents(baseCents: number, discountBps: number) {
  const base = floorToWholeYuanCents(baseCents)
  const bps = Math.max(0, Math.min(10000, Number(discountBps || 0)))
  if (bps <= 0 || base <= 0) return 0
  return floorToWholeYuanCents(base * bps / 10000)
}

export function formatWholeYuanMoney(cents: number) {
  const yuan = floorToWholeYuanCents(cents) / 100
  return '¥' + yuan.toLocaleString('zh-CN', {
    maximumFractionDigits: 0,
  })
}

export function wholeYuanPerHour(cents: number, hours: number) {
  if (!Number.isFinite(hours) || hours <= 0) return 0
  return Math.floor(floorToWholeYuanCents(cents) / 100 / hours)
}
