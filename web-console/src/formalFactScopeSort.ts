import type { LiveAgentPlanFact, LiveAgentPlanProductLink } from './types'

export type FactScopeSortOrder = 'none' | 'ascending' | 'descending'
const linkCollator = new Intl.Collator('zh-CN', { numeric: true, sensitivity: 'base' })

export function sortFormalFactsByScope(
  facts: readonly LiveAgentPlanFact[],
  products: readonly LiveAgentPlanProductLink[],
  order: FactScopeSortOrder,
): LiveAgentPlanFact[] {
  if (order === 'none') return [...facts]
  const byId = new Map(products.map(product => [product.id, product]))
  const direction = order === 'ascending' ? 1 : -1
  return facts.map((fact, index) => {
    const scope = fact.scope
    if (scope?.kind === 'common') return { fact, index, group: 0, label: '', productId: 0 }
    const product = scope?.kind === 'product' && scope.product_link_id != null ? byId.get(scope.product_link_id) : undefined
    if (product?.status === 'active' && product.tenant_id === fact.tenant_id && product.plan_id === fact.plan_id) {
      const key = product.link_key.trim()
      return { fact, index, group: 1, label: /^\d+$/.test(key) ? key + '号链接' : key, productId: product.id }
    }
    return { fact, index, group: scope?.kind === 'product' ? 2 : 3, label: '', productId: 0 }
  }).sort((left, right) => {
    // Missing products and unassigned facts stay at the end in both directions.
    if (left.group >= 2 || right.group >= 2) return left.group - right.group || left.index - right.index
    const comparison = left.group - right.group || linkCollator.compare(left.label, right.label) || left.productId - right.productId
    return direction * comparison || left.index - right.index
  }).map(item => item.fact)
}
