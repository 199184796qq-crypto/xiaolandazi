import type { BenefitScope } from './contentPools'
import type { LiveAgentPlanBenefit, LiveAgentPlanProductLink } from './types'

// Saved product IDs define applicability; a renamed/reordered link is only a label.
// For older records, the server and this display share the same link-key fallback.
export function benefitAppliesToProduct(
  benefit: LiveAgentPlanBenefit,
  product: LiveAgentPlanProductLink,
  scopes?: BenefitScope[],
) {
  if (benefit.tenant_id !== product.tenant_id || benefit.plan_id !== product.plan_id) return false
  const scope = scopes?.find(item => item.benefit_id === benefit.id)
  if (scope) return scope.all || !!scope.products?.includes(product.id)
  return !benefit.link_key || benefit.link_key === product.link_key
}
