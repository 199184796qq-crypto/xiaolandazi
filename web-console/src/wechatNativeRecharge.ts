const storageKey = 'xiaolan-desktop-native-recharge-draft'
export type NativeRechargeDraft = { cents: number; key: string; id?: number }

export function nativeRechargeAmount(value: string): number {
  const text = value.trim()
  if (!/^\d+(?:\.\d{1,2})?$/.test(text)) throw new Error('请输入正确的充值金额，最多两位小数')
  const [whole, fraction = ''] = text.split('.')
  const cents = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  if (!Number.isSafeInteger(cents) || cents <= 0 || cents > 2147483647) throw new Error('充值金额必须在0.01至21474836.47元之间')
  return cents
}

export function savedNativeRechargeDraft(): NativeRechargeDraft | null {
  const raw = sessionStorage.getItem(storageKey)
  if (!raw) return null
  const value = JSON.parse(raw) as NativeRechargeDraft
  if (!value || typeof value !== 'object' || !Number.isSafeInteger(value.cents) || value.cents <= 0 || value.cents > 2147483647 ||
      typeof value.key !== 'string' || !/^[A-Za-z0-9_:.-]{16,96}$/.test(value.key) ||
      (value.id !== undefined && (!Number.isSafeInteger(value.id) || value.id <= 0))) {
    throw new Error('原充值记录标识无效，请先在充值记录中核对，勿重复付款')
  }
  return value
}

export function nativeRechargeDraft(cents: number): NativeRechargeDraft {
  if (!Number.isSafeInteger(cents) || cents <= 0 || cents > 2147483647) throw new Error('充值金额无效')
  const saved = savedNativeRechargeDraft()
  if (saved) {
    if (saved.cents !== cents) throw new Error(`上一笔${(saved.cents / 100).toFixed(2)}元充值尚未确认，请继续原金额，不要重复创建`)
    return saved
  }
  const draft = { cents, key: `native-recharge-${crypto.randomUUID()}` }
  sessionStorage.setItem(storageKey, JSON.stringify(draft))
  return draft
}

export function rememberNativeRechargeID(draft: NativeRechargeDraft, id: number) {
  if (!Number.isSafeInteger(id) || id <= 0) throw new Error('充值单编号无效')
  sessionStorage.setItem(storageKey, JSON.stringify({ ...draft, id }))
}

export function clearNativeRechargeDraft() { sessionStorage.removeItem(storageKey) }

export function nativeQRIsValid(image: string, expires: string, now = Date.now()): boolean {
  const expiry = Date.parse(expires)
  return /^data:image\/png;base64,[A-Za-z0-9+/]+=*$/.test(image) && image.length < 65536 &&
    Number.isFinite(expiry) && expiry > now && expiry <= now + 2 * 60 * 60 * 1000
}
