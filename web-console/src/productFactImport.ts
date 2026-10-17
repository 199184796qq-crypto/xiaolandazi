import type { AdoptLiveAgentPlanFactsOutput, LiveAgentPlanBenefit, LiveAgentPlanFact, LiveAgentPlanFactCandidate, LiveAgentPlanProductLink } from './types'
import type { BenefitScope } from './contentPools'
import { benefitAppliesToProduct } from './benefitScopeDisplay'

type Context = { plan: number; tenant: number }
type ImportRow = { product: number; source: string; key: string; value: string; quote: string }
type ImportMarker = { schema: string; product: number; source: string; fingerprint: string; text?: string }
export type ProductFactImportResult = { imported: number; existing: number; failures: string[]; skippedBenefits: number; stopped: boolean }
export type ProductFactImportIO = {
  adopt: (candidate: LiveAgentPlanFactCandidate) => Promise<AdoptLiveAgentPlanFactsOutput>
  scope: (fact: LiveAgentPlanFact, product: number) => Promise<LiveAgentPlanFact>
  current: () => boolean
  accept: (fact: LiveAgentPlanFact) => void
  progress: (done: number, total: number) => void
}
const schema = 'product-card-fact-import/v1'
const text = (value: unknown) => typeof value === 'string' ? value.trim() : ''
const fieldLabels = { spec: '规格', daily_price: '销售价', quantity: '数量', audience: '适用' } as const
function marker(fact: LiveAgentPlanFact): ImportMarker | null {
  try { const m = JSON.parse(fact.source_quote || ''); return m.schema === schema && typeof m.source === 'string' && typeof m.fingerprint === 'string' && Number.isSafeInteger(m.product) ? m : null } catch { return null }
}
function owned(fact: LiveAgentPlanFact, ctx: Context) { return fact.status === 'active' && fact.plan_id === ctx.plan && fact.tenant_id === ctx.tenant && fact.id > 0 }
export async function buildProductFactImportRows(ctx: Context, products: LiveAgentPlanProductLink[], benefits: LiveAgentPlanBenefit[], scopes: BenefitScope[], now = Date.now()) {
  const rows: ImportRow[] = []; let skippedBenefits = 0
  const active = products.filter(p => p.plan_id === ctx.plan && p.tenant_id === ctx.tenant && p.status === 'active' && p.id > 0)
    .sort((a, b) => a.link_key.localeCompare(b.link_key, 'zh-CN', { numeric: true }) || a.id - b.id)
  async function add(p: LiveAgentPlanProductLink, source: string, label: string, raw: string, conditions = '') {
    if (!text(raw)) return
    const link = /^\d+$/.test(p.link_key.trim()) ? p.link_key.trim() + '号链接' : p.link_key.trim()
    const key = [...`${link}·${label}`].slice(0, 255).join('')
    const value = [`链接：${link}`, p.product_name ? `商品：${p.product_name}` : '', `${label}：${text(raw)}`, conditions].filter(Boolean).join('\n')
    const fingerprint = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value))), n => n.toString(16).padStart(2, '0')).join('')
    const quote = JSON.stringify({ schema, product: p.id, source, fingerprint, text: [...value].slice(0, 500).join('') })
    rows.push({ product: p.id, source, key, value, quote })
  }
  for (const p of active) {
    await add(p, `product:${p.id}:name`, '商品名称', text(p.product_name))
    for (const field of Object.keys(fieldLabels) as (keyof typeof fieldLabels)[]) await add(p, `product:${p.id}:${field}`, text(p.field_labels?.[field]) || fieldLabels[field], text(p[field]))
    for (const a of p.attributes || []) {
      if (a.status === 'active' && a.tenant_id === ctx.tenant && a.plan_id === ctx.plan && a.product_link_id === p.id) {
        await add(p, `product:${p.id}:attribute:${a.id}`, text(a.label) || text(a.code), [text(a.value), text(a.unit)].filter(Boolean).join(' '))
      }
    }
  }
  for (const b of benefits) {
    if (b.tenant_id !== ctx.tenant || b.plan_id !== ctx.plan || b.status !== 'active') continue
    let validity = '有效方式：长期有效'
    if (b.validity_mode !== 'permanent') {
      const start = b.starts_at ? Date.parse(b.starts_at) : null, end = b.ends_at ? Date.parse(b.ends_at) : null
      if ((!b.starts_at && !b.ends_at) || (start !== null && (!Number.isFinite(start) || start > now)) || (end !== null && (!Number.isFinite(end) || end <= now)) || (start !== null && end !== null && start >= end)) { skippedBenefits++; continue }
      validity = ['有效方式：指定时间', b.starts_at ? `生效时间：${b.starts_at}` : '', b.ends_at ? `失效时间：${b.ends_at}` : ''].filter(Boolean).join('\n')
    }
    const targets = active.filter(p => benefitAppliesToProduct(b, p, scopes))
    if (!targets.length) { skippedBenefits++; continue }
    for (const p of targets) for (const [field, label] of [['activity_price', '活动价'], ['gift', '赠品福利'], ['activity', '活动内容']] as const) {
      await add(p, `benefit:${b.id}:product:${p.id}:${field}`, `福利${b.key ? '·' + b.key : ''}·${label}`, text(b[field]), validity)
    }
  }
  return { rows, skippedBenefits }
}

// Reuse the original normalization/adoption and CAS ownership endpoints. A
// newly adopted row finishes its ownership write even if the page switches;
// no later rows start, and the stale caller cannot write into the new UI.
export async function importProductFacts(ctx: Context, rows: ImportRow[], facts: LiveAgentPlanFact[], io: ProductFactImportIO): Promise<ProductFactImportResult> {
  const result: ProductFactImportResult = { imported: 0, existing: 0, failures: [], skippedBenefits: 0, stopped: false }
  const known = facts.filter(f => owned(f, ctx))
  for (const [index, row] of rows.entries()) {
    if (!io.current()) { result.stopped = true; break }
    io.progress(index, rows.length)
    try {
      if ([...row.quote].length > 2000) throw new Error('来源记录超过2000字，请先拆分商品卡资料')
      if ([...row.value].length > 4000) throw new Error('内容超过4000字，请先拆分商品卡资料')
      const source = JSON.parse(row.quote) as ImportMarker
      const matches = known.filter(f => { const m = marker(f); return m?.product === row.product && m.source === row.source })
      if (matches.length > 1) throw new Error('发现重复导入记录，请先核对现有事实')
      let saved = matches[0], created = false
      if (saved) {
        if (marker(saved)?.fingerprint !== source.fingerprint) throw new Error('商品卡资料已变化，已保留原事实，请修改或删除原事实后再导入')
      } else {
        const collision = known.find(f => f.category === 'other' && f.key === row.key)
        if (collision) throw new Error('已有同名事实，已保留原内容和归属，请先核对')
        const output = await io.adopt({ category: 'other', key: row.key, value: row.value, source_quote: row.quote, forbidden_wording: '', safe_rewrite: '', status: 'pending', review_bucket: 'adoptable' })
        const outcome = output.results?.[0]
        if (!outcome || !['adopted', 'unchanged'].includes(outcome.status)) throw new Error(outcome?.message || '事实未通过原保存流程')
        saved = outcome.saved || outcome.existing!
        const savedMarker = saved && marker(saved)
        if (!saved || !owned(saved, ctx) || savedMarker?.source !== row.source || savedMarker.product !== row.product || savedMarker.fingerprint !== source.fingerprint) throw new Error('保存回执不匹配，请刷新核对后重试')
        created = outcome.status === 'adopted'
        if (!known.some(f => f.id === saved.id)) known.push(saved)
      }
      // Never overwrite a later human ownership decision on repeated imports.
      if (created || !saved.scope) {
        try {
          const updated = await io.scope(saved, row.product)
          if (!owned(updated, ctx) || updated.id !== saved.id || updated.scope?.kind !== 'product' || updated.scope.product_link_id !== row.product || updated.version_no <= saved.version_no) throw new Error('归属保存回执不匹配')
          saved = updated
          known.splice(known.findIndex(f => f.id === saved.id), 1, saved)
        } catch (err) {
          if (io.current()) io.accept(saved)
          throw new Error(`事实已保存，归属未完成，再次导入可补齐：${err instanceof Error ? err.message : '保存失败'}`)
        }
      }
      if (io.current()) io.accept(saved)
      if (created) result.imported++; else result.existing++
    } catch (err) { result.failures.push(`${row.key}：${err instanceof Error ? err.message : '导入失败'}`) }
    if (io.current()) io.progress(index + 1, rows.length)
  }
  return result
}
