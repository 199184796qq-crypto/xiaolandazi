export interface LiveCostTier {
  max_input_tokens: number
  input_micros_per_million: number
  cached_input_micros_per_million: number
  output_micros_per_million: number
}

export interface LiveCostModelPrice {
  provider: string
  model: string
  tiers: LiveCostTier[]
}

export interface LiveCostSettings {
  version: 1
  revision: number
  tts_price_micros_per_10k: number
  default_model_rates: LiveCostTier[]
  model_prices: LiveCostModelPrice[]
  mainline_hourly_micros: number
  optional_hourly_micros: number
  total_hourly_micros: number
}

export interface LiveCostTierDraft {
  maxInput: string
  input: string
  cached: string
  output: string
}

export interface LiveCostModelDraft {
  provider: string
  model: string
  tiers: LiveCostTierDraft[]
}

export interface LiveCostDraft {
  revision: number
  tts: string
  mainline: string
  optional: string
  total: string
  fallback: LiveCostTierDraft[]
  models: LiveCostModelDraft[]
}

export const liveCostAmountMax = 1_000_000_000_000

// Decimal strings avoid binary floating-point rounding at the money boundary.
export function liveCostYuanToMicros(raw: string, label: string, allowZero = false): number {
  const value = String(raw).trim()
  if (!/^\d+(?:\.\d{1,6})?$/.test(value)) {
    throw new Error(`${label}请填写非负金额，最多保留六位小数`)
  }
  const [whole = '0', fraction = ''] = value.split('.')
  const micros = BigInt(whole) * 1_000_000n + BigInt(fraction.padEnd(6, '0'))
  if (micros > BigInt(liveCostAmountMax) || (!allowZero && micros === 0n)) {
    throw new Error(`${label}${allowZero ? '须为零至一百万元' : '须大于零且不超过一百万元'}`)
  }
  return Number(micros)
}

export function liveCostMicrosToYuan(value: number): string {
  if (!Number.isSafeInteger(value) || value < 0 || value > liveCostAmountMax) {
    throw new Error('成本配置中的金额无效，请联系管理员检查')
  }
  const whole = Math.floor(value / 1_000_000)
  const fraction = String(value % 1_000_000).padStart(6, '0').replace(/0+$/, '')
  return fraction ? `${whole}.${fraction}` : String(whole)
}

function tierDraft(tier: LiveCostTier): LiveCostTierDraft {
  return {
    maxInput: String(tier.max_input_tokens),
    input: liveCostMicrosToYuan(tier.input_micros_per_million),
    cached: liveCostMicrosToYuan(tier.cached_input_micros_per_million),
    output: liveCostMicrosToYuan(tier.output_micros_per_million),
  }
}

export function liveCostSettingsToDraft(config: LiveCostSettings): LiveCostDraft {
  if (config?.version !== 1 || !Number.isSafeInteger(config.revision) || config.revision < 0 ||
    !Array.isArray(config.default_model_rates) || !Array.isArray(config.model_prices)) {
    throw new Error('读取到的成本配置不完整，请重新读取')
  }
  const draft: LiveCostDraft = {
    revision: config.revision,
    tts: liveCostMicrosToYuan(config.tts_price_micros_per_10k),
    mainline: liveCostMicrosToYuan(config.mainline_hourly_micros),
    optional: liveCostMicrosToYuan(config.optional_hourly_micros),
    total: liveCostMicrosToYuan(config.total_hourly_micros),
    fallback: config.default_model_rates.map(tierDraft),
    models: config.model_prices.map(rule => ({ provider: rule.provider, model: rule.model, tiers: rule.tiers.map(tierDraft) })),
  }
  liveCostDraftToSettings(draft)
  return draft
}

function tiersFromDraft(tiers: LiveCostTierDraft[], label: string): LiveCostTier[] {
  if (tiers.length < 1 || tiers.length > 16) throw new Error(`${label}须保留一至十六个价格档位`)
  let previous = 0
  return tiers.map((tier, index) => {
    const raw = String(tier.maxInput).trim()
    const maximum = Number(raw)
    if (!/^\d+$/.test(raw) || !Number.isSafeInteger(maximum) || maximum < 0 || maximum > liveCostAmountMax ||
      (index === tiers.length - 1 ? maximum !== 0 : maximum <= previous)) {
      throw new Error(`${label}的输入上限须递增，最后一档请填零表示以上`)
    }
    previous = maximum
    const row = `${label}第${index + 1}档`
    return {
      max_input_tokens: maximum,
      input_micros_per_million: liveCostYuanToMicros(tier.input, `${row}输入单价`),
      cached_input_micros_per_million: liveCostYuanToMicros(tier.cached, `${row}缓存输入单价`, true),
      output_micros_per_million: liveCostYuanToMicros(tier.output, `${row}输出单价`),
    }
  })
}

export function liveCostDraftToSettings(draft: LiveCostDraft): LiveCostSettings {
  if (!Number.isSafeInteger(draft.revision) || draft.revision < 0 || draft.revision >= liveCostAmountMax) throw new Error('配置版本无效，请重新读取')
  if (draft.models.length > 32) throw new Error('模型价格最多支持三十二条规则')
  const mainline = liveCostYuanToMicros(draft.mainline, '主线更新小时目标', true)
  const optional = liveCostYuanToMicros(draft.optional, '全部可选工作小时软线', true)
  const total = liveCostYuanToMicros(draft.total, '直播间小时总目标')
  if (mainline > optional || optional > total) {
    throw new Error('小时预算须满足：主线更新目标不高于可选软线，可选软线不高于总目标')
  }
  const seen = new Set<string>()
  const prices = draft.models.map((rule, index) => {
    const provider = rule.provider.trim()
    const model = rule.model.trim()
    const encoder = new TextEncoder()
    if (!provider || !model || /[\p{Cc}\s]/u.test(provider + model) ||
      encoder.encode(provider).length > 96 || encoder.encode(model).length > 160 ||
      model.includes('*') || (provider !== '*' && provider.includes('*'))) {
      throw new Error(`第${index + 1}条模型规则请填写有效的供应商和模型名称`)
    }
    const key = `${provider.toLowerCase()}\u0000${model.toLowerCase()}`
    if (seen.has(key)) throw new Error('同一供应商与模型的价格规则不能重复')
    seen.add(key)
    return { provider, model, tiers: tiersFromDraft(rule.tiers, `第${index + 1}条模型规则`) }
  })
  const config: LiveCostSettings = {
    version: 1, revision: draft.revision,
    tts_price_micros_per_10k: liveCostYuanToMicros(draft.tts, '每万计费字符声音单价'),
    default_model_rates: tiersFromDraft(draft.fallback, '默认模型价格'),
    model_prices: prices,
    mainline_hourly_micros: mainline, optional_hourly_micros: optional, total_hourly_micros: total,
  }
  if (new TextEncoder().encode(JSON.stringify(config)).length > 32_000) throw new Error('成本配置内容过长，请减少模型或价格档位')
  return config
}

export function newLiveCostTier(): LiveCostTierDraft {
  return { maxInput: '0', input: '', cached: '', output: '' }
}

export function addLiveCostTier(tiers: LiveCostTierDraft[]) {
  if (tiers.length >= 16) return
  const next = newLiveCostTier()
  // Keep the unbounded final tier last; the new bounded row needs an explicit cap.
  next.maxInput = ''
  tiers.splice(Math.max(0, tiers.length - 1), 0, next)
}
