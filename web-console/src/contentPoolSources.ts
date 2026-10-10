import type { PoolSourceRow, PoolStatus, PoolSummary, PoolTask } from './contentPools'

export interface PoolSourceLocation {
  sourceId: string
  area: 'knowledge' | 'products' | 'benefits' | 'miner'
  entityId: number
  attributeId?: number
}
export interface PoolTaskSource {
  id: string
  label: string
  saved?: PoolSourceRow
  current?: PoolSourceRow
  state: 'same' | 'changed' | 'removed' | 'unavailable'
  location?: PoolSourceLocation
}
function fieldsIdentity(row: PoolSourceRow) {
  return JSON.stringify([row.area, row.label, [...(row.products || [])].sort((a, b) => a - b), Object.entries(row.fields || {}).sort(([a], [b]) => a.localeCompare(b))])
}
function sourceLocation(row: PoolSourceRow): PoolSourceLocation | undefined {
  const match = /^(knowledge|products|attributes|benefits):(\d+)$/.exec(row.id)
  if (match) {
    const id = Number(match[2])
    if (!Number.isSafeInteger(id) || id <= 0) return
    if (match[1] === 'attributes') {
      const product = row.products?.[0]
      if (row.products?.length !== 1 || !product) return
      return { sourceId: row.id, area: 'products', entityId: product, attributeId: id }
    }
    return { sourceId: row.id, area: match[1] as 'knowledge' | 'products' | 'benefits', entityId: id }
  }
  if (row.id === 'wheel:active') return { sourceId: row.id, area: 'miner', entityId: 0 }
}

// Follow recorded IDs through shared facts to their original records. Text is
// display-only: similar labels or values never select a different source.
export function poolTaskSources(status: PoolStatus | null, pool: PoolSummary, task: PoolTask): PoolTaskSource[] {
  const saved = new Map((status?.source_rows || []).map(row => [row.id, row]))
  const current = new Map((status?.current_source_rows || []).map(row => [row.id, row]))
  const facts = new Map((pool.facts || []).map(fact => [fact.id, fact]))
  const ids = [...new Set((task.source_ids || []).flatMap(id => facts.get(id)?.sources?.length ? facts.get(id)!.sources : [id]))]
  return ids.map(id => {
    const original = saved.get(id), latest = current.get(id)
    const state = !original || !status?.current_source_rows ? 'unavailable' : !latest ? 'removed' : fieldsIdentity(original) === fieldsIdentity(latest) ? 'same' : 'changed'
    return { id, label: original?.label || latest?.label || id, saved: original, current: latest, state, location: latest && sourceLocation(latest) }
  })
}

export function poolSourceText(row?: PoolSourceRow) {
  return Object.entries(row?.fields || {}).filter(([, value]) => !!value).map(([key, value]) => `${key}：${value}`).join('\n')
}
